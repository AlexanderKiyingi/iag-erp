-- Employee compensation.
--
-- No service on the platform held a pay rate: not erp_employees, not finance's
-- payroll_employee_refs. Payroll was calculated entirely outside both, so a day
-- of leave could not be valued anywhere and the accrued-leave liability had to
-- be stated by an operator rather than derived.
--
-- Compensation lives here because HR owns the person and pay is an attribute of
-- employment. Finance receives a daily rate to value obligations with; it does
-- not receive, and does not need, the rest of the compensation record.
--
-- This is the most sensitive table in the service. Access is expected to be
-- narrower than the rest of HR, and only the derived daily rate is published —
-- never gross, benefits, or history.

CREATE TABLE IF NOT EXISTS erp_employee_compensation (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id   UUID NOT NULL REFERENCES erp_employees (id) ON DELETE CASCADE,
    monthly_gross NUMERIC(20, 2) NOT NULL,
    currency      TEXT NOT NULL DEFAULT 'UGX',
    -- Working days per month, the divisor that turns a monthly figure into the
    -- rate a day of leave is worth. 22 is the common five-day-week convention;
    -- set it to match the contract where it differs.
    working_days_per_month NUMERIC(5, 2) NOT NULL DEFAULT 22,
    -- Effective dating: a pay rise re-measures the liability from the date it
    -- applies, and history is kept rather than overwritten.
    effective_from DATE NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (employee_id, effective_from)
);

ALTER TABLE erp_employee_compensation
    DROP CONSTRAINT IF EXISTS erp_employee_compensation_positive;

ALTER TABLE erp_employee_compensation
    ADD CONSTRAINT erp_employee_compensation_positive
    CHECK (monthly_gross > 0 AND working_days_per_month > 0);

-- The current record for an employee is the latest effective_from not in the
-- future, which is the lookup every read makes.
CREATE INDEX IF NOT EXISTS erp_employee_compensation_current_idx
    ON erp_employee_compensation (employee_id, effective_from DESC);
