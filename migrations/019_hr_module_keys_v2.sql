-- Widen the HR module record store by the four keys the IAG HR app already sends.
--
-- 012 added six keys for that app and the frontend later added four more --
-- employee-profiles, performance-kpis, training-and-development and contracts --
-- with no matching change here or in HRModuleKeys. The frontend has been mapping
-- them onto /api/v1/hr/:module since then.
--
-- IsHRModule gates list, get, create, update and delete alike, so all four were
-- 400 ErrBadInput on every call, reads included. That is worse than the
-- half-applied-deploy case 012 documents: there the constraint and the binary
-- disagree and reads keep working, here neither side had ever heard of the key.
-- The app's own coverage report counted them as wired because it only checks
-- that an adapter exists, which is why four dead screens read as green.
--
-- Why these four are their own keys, on the same reasoning 012 gives: an
-- employee profile is HR's own extended record of a person, a KPI is not a
-- performance review, a training-and-development plan is not an enrolment, and a
-- contract is not a payslip. Folding any of them into an existing partition
-- makes every list endpoint return unrelated record types mixed together.
--
-- Ship this BEFORE the matching HRModuleKeys change. Constraint first leaves the
-- current 400 exactly as it is and changes nothing until the binary lands;
-- binary first turns that clean 400 into the schema_behind 503 path for as long
-- as the two disagree.
--
-- Additive only: no existing row changes module, and nothing that validated
-- before stops validating.

ALTER TABLE erp_hr_module_records
    DROP CONSTRAINT IF EXISTS erp_hr_module_records_module_check;

ALTER TABLE erp_hr_module_records
    ADD CONSTRAINT erp_hr_module_records_module_check CHECK (module IN (
        'shifts', 'recruitment', 'onboarding', 'performance', 'training',
        'helpdesk', 'documents', 'disciplinary', 'assets', 'offboarding',
        'payroll', 'settings',
        -- Added for the IAG HR app.
        'sites', 'blocks', 'holidays',
        'payslip-items', 'recurring-payslips', 'statutory-remittances',
        -- Added for the IAG HR app, second round.
        'employee-profiles', 'performance-kpis', 'training-and-development',
        'contracts'
    ));
