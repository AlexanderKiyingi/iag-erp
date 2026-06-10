-- ERP HR core: departments, employees, leave, attendance.

CREATE TABLE IF NOT EXISTS erp_departments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    plant_code TEXT,
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS erp_employees (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_no     TEXT NOT NULL UNIQUE,
    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,
    email           TEXT,
    phone           TEXT,
    department_id   UUID REFERENCES erp_departments(id),
    job_title       TEXT NOT NULL DEFAULT '',
    employment_type TEXT NOT NULL DEFAULT 'permanent'
        CHECK (employment_type IN ('permanent', 'contract', 'casual')),
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'on_leave', 'terminated')),
    hire_date       DATE,
    plant_code      TEXT,
    attrs           JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_employees_department_idx ON erp_employees (department_id);
CREATE INDEX IF NOT EXISTS erp_employees_status_idx ON erp_employees (status);

CREATE TABLE IF NOT EXISTS erp_leave_types (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    paid          BOOLEAN NOT NULL DEFAULT true,
    days_per_year NUMERIC(6, 2) NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS erp_leave_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id  UUID NOT NULL REFERENCES erp_employees(id),
    leave_type_id UUID NOT NULL REFERENCES erp_leave_types(id),
    starts_on    DATE NOT NULL,
    ends_on      DATE NOT NULL,
    days         NUMERIC(6, 2) NOT NULL DEFAULT 0,
    reason       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    approver_ref TEXT,
    decided_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_leave_requests_employee_idx ON erp_leave_requests (employee_id, created_at DESC);
CREATE INDEX IF NOT EXISTS erp_leave_requests_status_idx ON erp_leave_requests (status);

CREATE TABLE IF NOT EXISTS erp_attendance_records (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id UUID NOT NULL REFERENCES erp_employees(id),
    work_date   DATE NOT NULL,
    clock_in    TIMESTAMPTZ,
    clock_out   TIMESTAMPTZ,
    status      TEXT NOT NULL DEFAULT 'present'
        CHECK (status IN ('present', 'absent', 'half_day', 'leave')),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (employee_id, work_date)
);

CREATE INDEX IF NOT EXISTS erp_attendance_work_date_idx ON erp_attendance_records (work_date DESC);

CREATE TABLE IF NOT EXISTS erp_production_orders (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    po_num     TEXT NOT NULL UNIQUE,
    customer   TEXT NOT NULL DEFAULT '',
    product    TEXT NOT NULL DEFAULT '',
    qty_kg     NUMERIC(18, 3) NOT NULL DEFAULT 0,
    origin_lot TEXT,
    asset_tag  TEXT,
    status     TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'scheduled', 'running', 'completed', 'cancelled')),
    due_at     TIMESTAMPTZ,
    erp_ref    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_production_orders_status_idx ON erp_production_orders (status);
