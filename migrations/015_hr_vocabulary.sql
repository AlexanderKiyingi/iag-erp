-- Make the HR vocabulary match the one an HR officer is actually offered, and
-- make assigning an allowance idempotent.
--
-- Every change here closes a case where a frontend presented a choice the
-- database then refused. The failure mode is the worst kind: the form validates,
-- the operator saves, and a CHECK constraint 500s from deep inside the store —
-- so the value was never wrong, only unrepresentable.
--
-- Additive throughout: nothing that validated before stops validating.

-- ---------------------------------------------------------------------------
-- 1. Interns are an employment type
-- ---------------------------------------------------------------------------
-- 'permanent', 'contract' and 'casual' left nowhere to put an intern, who is
-- none of the three: not permanent, not on a commercial contract, and not
-- casual labour paid by the day. Both the employee record and the recruitment
-- requisition that hires into it need the same vocabulary, or a requisition
-- cannot be filled by the person it was raised for.

ALTER TABLE erp_employees DROP CONSTRAINT IF EXISTS erp_employees_employment_type_check;
ALTER TABLE erp_employees ADD CONSTRAINT erp_employees_employment_type_check
    CHECK (employment_type IN ('permanent', 'contract', 'casual', 'intern'));

ALTER TABLE erp_job_requisitions DROP CONSTRAINT IF EXISTS erp_job_requisitions_employment_type_check;
ALTER TABLE erp_job_requisitions ADD CONSTRAINT erp_job_requisitions_employment_type_check
    CHECK (employment_type IN ('permanent', 'contract', 'casual', 'intern'));

-- ---------------------------------------------------------------------------
-- 2. A public holiday is an attendance outcome
-- ---------------------------------------------------------------------------
-- The service already stores holidays (erp_hr_module_records module 'holidays')
-- and the working-day leave calculation already reads them, but a day marked as
-- a holiday could not be recorded against a person. 'absent' and 'leave' both
-- misstate it: nobody was absent and nobody spent leave.

ALTER TABLE erp_attendance_records DROP CONSTRAINT IF EXISTS erp_attendance_records_status_check;
ALTER TABLE erp_attendance_records ADD CONSTRAINT erp_attendance_records_status_check
    CHECK (status IN ('present', 'absent', 'half_day', 'leave', 'late', 'remote', 'holiday'));

-- ---------------------------------------------------------------------------
-- 3. The leave types Ugandan employment actually grants
-- ---------------------------------------------------------------------------
-- 003_seed.sql seeded four. Paternity leave is statutory under the Employment
-- Act; compassionate and study leave are standard terms. A request for any of
-- them was rejected as an unknown leave type, with no way for HR to add one
-- (there is no create verb for leave types).
--
-- days_per_year is entitlement, not a limit on a single request. Paternity is
-- the statutory four working days; compassionate and study leave are agreed per
-- case rather than accrued, so they carry no annual entitlement and are left at
-- zero deliberately — a balance of zero is the honest statement that this type
-- does not accrue, and LEAVE_BALANCE_ENFORCED is what decides whether that
-- blocks a request.

INSERT INTO erp_leave_types (code, name, paid, days_per_year)
VALUES
    ('PATERNITY',    'Paternity leave',    true, 4),
    ('COMPASSIONATE','Compassionate leave',true, 0),
    ('STUDY',        'Study leave',        true, 0)
ON CONFLICT (code) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 4. Assigning the same allowance twice must not pay it twice
-- ---------------------------------------------------------------------------
-- AssignPayComponent INSERTs unconditionally, and the payroll calculation sums
-- every component effective in the period (payroll_calc.go: out.Allowances +=
-- comp.Amount). So saving an employee's housing allowance twice on one day
-- produced two rows and paid it twice — a silent overpayment, not an error.
--
-- erp_employee_compensation has carried UNIQUE (employee_id, effective_from)
-- since 005 for exactly this reason; this gives components the same shape so a
-- re-assignment corrects the amount instead of adding to it.
--
-- Existing duplicates are collapsed to the most recent row first. Keeping the
-- newest is what a repeated assignment meant: the later figure is the one the
-- operator intended, and the earlier one has been over-paying alongside it.

DELETE FROM erp_employee_pay_components a
    USING erp_employee_pay_components b
WHERE a.employee_id = b.employee_id
  AND a.component_code = b.component_code
  AND a.effective_from = b.effective_from
  AND (a.created_at, a.id) < (b.created_at, b.id);

CREATE UNIQUE INDEX IF NOT EXISTS erp_employee_pay_components_unique_idx
    ON erp_employee_pay_components (employee_id, component_code, effective_from);

-- ---------------------------------------------------------------------------
-- 5. Somewhere to put "other allowances"
-- ---------------------------------------------------------------------------
-- The HR app has collected a free "other allowances" figure on the employee
-- form since it was written, and the catalogue seeded in 010 has no entry it
-- maps to: HOUSING and TRANSPORT are specific, AIRTIME/OVERTIME/BONUS all mean
-- something the figure is not. With nowhere to send it the amount stayed in the
-- employee's attrs blob and never reached a payslip.
--
-- Taxable and pensionable to match HOUSING and TRANSPORT: a cash allowance is
-- employment income under the Income Tax Act unless a specific exemption
-- applies, and treating it as exempt by default would understate PAYE.

INSERT INTO erp_pay_components (code, name, kind, taxable, pensionable) VALUES
    ('OTHER', 'Other allowance', 'earning', true, true)
ON CONFLICT (code) DO NOTHING;
