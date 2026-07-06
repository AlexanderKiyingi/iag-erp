-- HRMIAG extended modules, attendance fields, employee status expansion.

ALTER TABLE erp_attendance_records
    ADD COLUMN IF NOT EXISTS location TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS method TEXT NOT NULL DEFAULT '';

ALTER TABLE erp_attendance_records DROP CONSTRAINT IF EXISTS erp_attendance_records_status_check;
ALTER TABLE erp_attendance_records ADD CONSTRAINT erp_attendance_records_status_check
    CHECK (status IN ('present', 'absent', 'half_day', 'leave', 'late', 'remote'));

ALTER TABLE erp_employees DROP CONSTRAINT IF EXISTS erp_employees_status_check;
ALTER TABLE erp_employees ADD CONSTRAINT erp_employees_status_check
    CHECK (status IN ('active', 'on_leave', 'terminated', 'probation', 'suspended'));

CREATE TABLE IF NOT EXISTS erp_hr_module_records (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    module      TEXT NOT NULL CHECK (module IN (
        'shifts', 'recruitment', 'onboarding', 'performance', 'training',
        'helpdesk', 'documents', 'disciplinary', 'assets', 'offboarding',
        'payroll', 'settings'
    )),
    department  TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT '',
    data        JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_hr_module_records_module_idx
    ON erp_hr_module_records (module, updated_at DESC);

CREATE INDEX IF NOT EXISTS erp_hr_module_records_status_idx
    ON erp_hr_module_records (module, status);

CREATE INDEX IF NOT EXISTS erp_hr_module_records_department_idx
    ON erp_hr_module_records (module, department);
