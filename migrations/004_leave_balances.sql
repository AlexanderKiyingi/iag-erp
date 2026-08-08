-- Leave balances: how much leave each employee has earned and not yet taken.
--
-- HR knew entitlement (erp_leave_types.days_per_year) and consumption
-- (erp_leave_requests), and computed neither into a balance. Finance therefore
-- received leave being *taken* and never the obligation being *earned*, which
-- is the side an accrued-leave liability comes from.
--
-- POLICY IS CONFIGURATION, NOT CODE. The columns below carry the rules that
-- vary by organisation and jurisdiction. Their defaults are deliberately the
-- simplest reading, not a guess at yours — see the warning on carry_over_max_days.

-- Leave that is not paid creates no obligation, so `paid` already decides which
-- types accrue. These add the rules that decide how much.
ALTER TABLE erp_leave_types
    ADD COLUMN IF NOT EXISTS carry_over_max_days NUMERIC(6, 2) NOT NULL DEFAULT 0;

-- WARNING: the default of 0 means untaken leave expires at year end, which is
-- the LOWER liability. Where leave genuinely carries over, leaving this at 0
-- understates what is owed. Set it per leave type before the figures are
-- trusted. A negative value is rejected rather than treated as unlimited,
-- because "unlimited" should be stated explicitly and large.
ALTER TABLE erp_leave_types
    DROP CONSTRAINT IF EXISTS erp_leave_types_carry_over_non_negative;

ALTER TABLE erp_leave_types
    ADD CONSTRAINT erp_leave_types_carry_over_non_negative
    CHECK (carry_over_max_days >= 0);

-- Probation: months of service before entitlement begins accruing. 0 accrues
-- from the hire date.
ALTER TABLE erp_leave_types
    ADD COLUMN IF NOT EXISTS accrues_after_months INTEGER NOT NULL DEFAULT 0;

-- One row per employee, leave type and accrual year. Balances are materialised
-- rather than computed on read so a change can be published as an event: an
-- obligation that only exists as a query cannot be told to anyone.
CREATE TABLE IF NOT EXISTS erp_leave_balances (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id    UUID NOT NULL REFERENCES erp_employees (id) ON DELETE CASCADE,
    leave_type_id  UUID NOT NULL REFERENCES erp_leave_types (id) ON DELETE CASCADE,
    accrual_year   INTEGER NOT NULL,
    -- Carried in from the prior year, already capped at carry_over_max_days.
    opening_days   NUMERIC(8, 2) NOT NULL DEFAULT 0,
    -- Entitlement earned so far this year, pro-rated for service.
    earned_days    NUMERIC(8, 2) NOT NULL DEFAULT 0,
    -- Approved leave falling in this year. Rejected and cancelled do not count.
    taken_days     NUMERIC(8, 2) NOT NULL DEFAULT 0,
    computed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (employee_id, leave_type_id, accrual_year)
);

-- balance_days is derived, never stored: a stored copy is one more thing that
-- can disagree with its own inputs.
CREATE INDEX IF NOT EXISTS erp_leave_balances_employee_idx
    ON erp_leave_balances (employee_id, accrual_year);
