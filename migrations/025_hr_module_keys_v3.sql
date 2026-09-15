-- Widen the HR module record store by one key: biometric-enrolments.
--
-- The HR app's biometric enrolment register (frontend 414dab6) was mapped onto
-- a module key called 'benefits', which this service has never accepted -- so
-- every call against it, reads included, was a 400 from IsHRModule. Found by
-- driving the deployed app in a browser on 2026-09-15, the same day the four
-- keys 019 added went live.
--
-- The key is 'biometric-enrolments', not 'benefits': a register of who has
-- enrolled a fingerprint template is not a benefits register, and a module
-- name that says otherwise would mislead every query written against it.
-- Nothing is stored under 'benefits' -- a 400 stores nothing -- so there is no
-- row to move.
--
-- The template itself is never here. The frontend hashes the device token
-- before it leaves the browser (scrubBiometricRecord); what lands in this
-- store is a hash and an employee number.
--
-- Same rule as 019: ship this before the matching HRModuleKeys change.
-- Constraint first leaves today's 400 unchanged until the code lands; binary
-- first turns writes into schema_behind 503s and reads into an empty list.

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
        'contracts',
        -- Added for the IAG HR app, third round.
        'biometric-enrolments'
    ));
