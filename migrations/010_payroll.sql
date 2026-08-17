-- Payroll.
--
-- Nothing on the platform computed pay. ERP stored "payroll" as untyped JSONB
-- documents in erp_hr_module_records, and finance's payroll_runs accepted
-- gross/PAYE/NSSF/net as figures an operator typed in. So the one number that
-- had to be right — what each person is owed and what is withheld on their
-- behalf — was produced outside every system that records it.
--
-- POLICY IS CONFIGURATION, NOT CODE. Tax bands and statutory rates are rows,
-- effective-dated, seeded with Uganda's current figures. When a rate changes,
-- the change is a row and the payslips already computed under the old one still
-- explain themselves. Nothing below hardcodes a percentage.

-- ---------------------------------------------------------------------------
-- Statutory configuration
-- ---------------------------------------------------------------------------

-- A progressive tax scale. One row per band, per scheme, per effective date.
--
-- base_tax is the cumulative tax at lower_bound, so tax within a band is
-- base_tax + rate x (income - lower_bound). Carrying the cumulative figure in
-- the row rather than summing the bands below it means a single band can be
-- corrected without silently restating the ones above it.
CREATE TABLE IF NOT EXISTS erp_tax_bands (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scheme         TEXT NOT NULL,             -- 'UG_PAYE_MONTHLY'
    effective_from DATE NOT NULL,
    seq            INT  NOT NULL,
    lower_bound    NUMERIC(20, 2) NOT NULL,
    -- NULL is the top band: unbounded above.
    upper_bound    NUMERIC(20, 2),
    rate           NUMERIC(6, 4) NOT NULL,    -- 0.3000 = 30%
    base_tax       NUMERIC(20, 2) NOT NULL DEFAULT 0,
    UNIQUE (scheme, effective_from, seq),
    CHECK (rate >= 0 AND rate <= 1),
    CHECK (upper_bound IS NULL OR upper_bound > lower_bound)
);

CREATE INDEX IF NOT EXISTS erp_tax_bands_lookup_idx
    ON erp_tax_bands (scheme, effective_from DESC, seq);

-- Flat-rate contributions: social security and anything like it. Split into the
-- employee's share (withheld from pay) and the employer's (a cost on top of it),
-- because they hit different accounts and only one reduces net pay.
CREATE TABLE IF NOT EXISTS erp_statutory_rates (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code           TEXT NOT NULL,             -- 'NSSF'
    name           TEXT NOT NULL,
    effective_from DATE NOT NULL,
    employee_rate  NUMERIC(6, 4) NOT NULL DEFAULT 0,
    employer_rate  NUMERIC(6, 4) NOT NULL DEFAULT 0,
    -- Ceiling on the earnings the rate applies to. NULL is uncapped, which is
    -- Uganda's NSSF today; the column exists so a cap can be introduced without
    -- a migration to the payroll engine.
    monthly_cap    NUMERIC(20, 2),
    active         BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (code, effective_from),
    CHECK (employee_rate >= 0 AND employer_rate >= 0)
);

-- Uganda PAYE, monthly, resident individuals (URA). The top band is the 30%
-- rate plus the additional 10% charged on chargeable income above 10,000,000.
INSERT INTO erp_tax_bands (scheme, effective_from, seq, lower_bound, upper_bound, rate, base_tax)
SELECT 'UG_PAYE_MONTHLY', '2023-07-01'::date, v.seq, v.lo, v.hi, v.rate, v.base
FROM (VALUES
    (1,        0.00,     235000.00, 0.0000,       0.00),
    (2,   235000.00,     335000.00, 0.1000,       0.00),
    (3,   335000.00,     410000.00, 0.2000,   10000.00),
    (4,   410000.00,   10000000.00, 0.3000,   25000.00),
    (5, 10000000.00,           NULL, 0.4000, 2902000.00)
) AS v(seq, lo, hi, rate, base)
WHERE NOT EXISTS (
    SELECT 1 FROM erp_tax_bands b
    WHERE b.scheme = 'UG_PAYE_MONTHLY' AND b.effective_from = '2023-07-01'::date AND b.seq = v.seq
);

-- NSSF: 5% withheld from the employee, 10% paid by the employer, uncapped.
INSERT INTO erp_statutory_rates (code, name, effective_from, employee_rate, employer_rate)
SELECT 'NSSF', 'National Social Security Fund', '2023-07-01'::date, 0.0500, 0.1000
WHERE NOT EXISTS (
    SELECT 1 FROM erp_statutory_rates r
    WHERE r.code = 'NSSF' AND r.effective_from = '2023-07-01'::date
);

-- ---------------------------------------------------------------------------
-- Pay components
-- ---------------------------------------------------------------------------

-- What can appear on a payslip besides basic pay. `taxable` and `pensionable`
-- are the two questions that decide how a component is treated, and they are
-- properties of the component rather than of the code that reads it.
CREATE TABLE IF NOT EXISTS erp_pay_components (
    code        TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('earning', 'deduction')),
    taxable     BOOLEAN NOT NULL DEFAULT true,
    pensionable BOOLEAN NOT NULL DEFAULT true,
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO erp_pay_components (code, name, kind, taxable, pensionable) VALUES
    ('HOUSING',   'Housing allowance',      'earning',   true,  true),
    ('TRANSPORT', 'Transport allowance',    'earning',   true,  true),
    ('AIRTIME',   'Airtime allowance',      'earning',   false, false),
    ('OVERTIME',  'Overtime',               'earning',   true,  true),
    ('BONUS',     'Bonus',                  'earning',   true,  false),
    ('LOAN',      'Staff loan repayment',   'deduction', false, false),
    ('ADVANCE',   'Salary advance recovery','deduction', false, false),
    ('UNION',     'Union dues',             'deduction', false, false)
ON CONFLICT (code) DO NOTHING;

-- A component attached to one employee for a period of time. Effective dating
-- again: an allowance that ended in March must not appear on April's payslip,
-- and must still appear on March's.
CREATE TABLE IF NOT EXISTS erp_employee_pay_components (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id    UUID NOT NULL REFERENCES erp_employees (id) ON DELETE CASCADE,
    component_code TEXT NOT NULL REFERENCES erp_pay_components (code),
    amount         NUMERIC(20, 2) NOT NULL,
    effective_from DATE NOT NULL,
    effective_to   DATE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (amount >= 0),
    CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE INDEX IF NOT EXISTS erp_employee_pay_components_lookup_idx
    ON erp_employee_pay_components (employee_id, effective_from DESC);

-- ---------------------------------------------------------------------------
-- Runs and payslips
-- ---------------------------------------------------------------------------

-- One payroll run per period. Draft is recomputable; approved is not; posted
-- has reached the ledger.
--
-- created_by and approved_by are separate columns and enforced apart in code:
-- payroll is the largest single payment most organisations make, and one person
-- who can both compute and release it is the standing definition of the control
-- weakness every audit looks for first.
CREATE TABLE IF NOT EXISTS erp_payroll_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_ref      TEXT NOT NULL UNIQUE,
    period       TEXT NOT NULL CHECK (period ~ '^[0-9]{4}-[0-9]{2}$'),
    status       TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'approved', 'posted', 'cancelled')),
    currency     TEXT NOT NULL DEFAULT 'UGX',
    employee_count   INT NOT NULL DEFAULT 0,
    gross            NUMERIC(20, 2) NOT NULL DEFAULT 0,
    taxable_gross    NUMERIC(20, 2) NOT NULL DEFAULT 0,
    paye             NUMERIC(20, 2) NOT NULL DEFAULT 0,
    nssf_employee    NUMERIC(20, 2) NOT NULL DEFAULT 0,
    nssf_employer    NUMERIC(20, 2) NOT NULL DEFAULT 0,
    other_deductions NUMERIC(20, 2) NOT NULL DEFAULT 0,
    net              NUMERIC(20, 2) NOT NULL DEFAULT 0,
    created_by_employee_no  TEXT,
    approved_by_employee_no TEXT,
    approved_at  TIMESTAMPTZ,
    posted_at    TIMESTAMPTZ,
    notes        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Only one run per period may reach the ledger. A second posted run for the
-- same month is a double-booking of the largest expense in it.
CREATE UNIQUE INDEX IF NOT EXISTS erp_payroll_runs_posted_period_idx
    ON erp_payroll_runs (period) WHERE status = 'posted';

CREATE INDEX IF NOT EXISTS erp_payroll_runs_period_idx
    ON erp_payroll_runs (period DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS erp_payslips (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        UUID NOT NULL REFERENCES erp_payroll_runs (id) ON DELETE CASCADE,
    employee_id   UUID NOT NULL REFERENCES erp_employees (id),
    employee_no   TEXT NOT NULL,
    employee_name TEXT NOT NULL,
    department_code TEXT NOT NULL DEFAULT '',
    currency      TEXT NOT NULL DEFAULT 'UGX',
    -- The contracted monthly figure, before anything is added or taken away.
    basic         NUMERIC(20, 2) NOT NULL DEFAULT 0,
    -- Basic reduced for unpaid absence, which is the figure everything else is
    -- computed from.
    earned_basic  NUMERIC(20, 2) NOT NULL DEFAULT 0,
    allowances    NUMERIC(20, 2) NOT NULL DEFAULT 0,
    gross         NUMERIC(20, 2) NOT NULL DEFAULT 0,
    taxable_gross NUMERIC(20, 2) NOT NULL DEFAULT 0,
    paye          NUMERIC(20, 2) NOT NULL DEFAULT 0,
    nssf_employee NUMERIC(20, 2) NOT NULL DEFAULT 0,
    nssf_employer NUMERIC(20, 2) NOT NULL DEFAULT 0,
    other_deductions NUMERIC(20, 2) NOT NULL DEFAULT 0,
    total_deductions NUMERIC(20, 2) NOT NULL DEFAULT 0,
    net           NUMERIC(20, 2) NOT NULL DEFAULT 0,
    working_days      NUMERIC(6, 2) NOT NULL DEFAULT 0,
    unpaid_leave_days NUMERIC(6, 2) NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, employee_id)
);

CREATE INDEX IF NOT EXISTS erp_payslips_employee_idx
    ON erp_payslips (employee_no, created_at DESC);

-- The lines behind the totals. A payslip that states a net figure without the
-- components that produced it cannot be queried by the person it belongs to,
-- which is the only reason a payslip exists.
CREATE TABLE IF NOT EXISTS erp_payslip_lines (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payslip_id     UUID NOT NULL REFERENCES erp_payslips (id) ON DELETE CASCADE,
    component_code TEXT NOT NULL,
    name           TEXT NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('earning', 'deduction', 'statutory')),
    amount         NUMERIC(20, 2) NOT NULL,
    seq            INT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS erp_payslip_lines_payslip_idx
    ON erp_payslip_lines (payslip_id, seq);
