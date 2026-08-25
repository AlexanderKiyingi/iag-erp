-- Widen the HR module record store to the keys an HR frontend actually renders.
--
-- erp_hr_module_records is the generic JSONB store for HR modules that have not
-- been promoted to a schema, and both the CHECK here and HRModuleKeys in
-- internal/store/hr_modules.go bound which keys are accepted. The original
-- twelve came from the HRMIAG frontend's module list.
--
-- The IAG HR app renders six more HR record types with no schema of their own:
-- worksites and blocks carry the check-in geofence (erp_worksites has no
-- latitude, longitude or radius), holidays drive the working-day leave
-- calculation, and payslip items, recurring payslips and statutory remittances
-- are payroll reference data rather than computed payroll output.
--
-- They are separate keys rather than rows inside 'payroll' or 'settings' with a
-- discriminator, because a shared partition makes every list endpoint return
-- three unrelated record types mixed together and pushes the filtering into
-- each frontend. The existing twelve keys already draw the line at this
-- granularity.
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
        'payslip-items', 'recurring-payslips', 'statutory-remittances'
    ));
