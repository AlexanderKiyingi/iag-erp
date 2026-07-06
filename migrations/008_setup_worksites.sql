-- Company setup items and worksite catalogue for HRMIAG.

CREATE TABLE IF NOT EXISTS erp_setup_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_type   TEXT NOT NULL CHECK (item_type IN (
        'department', 'branch', 'job_title', 'grade', 'holiday', 'policy',
        'approval_flow', 'work_location'
    )),
    name        TEXT NOT NULL,
    code        TEXT NOT NULL DEFAULT '',
    owner       TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'active',
    effective_date DATE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_setup_items_type_idx ON erp_setup_items (item_type, status);

CREATE TABLE IF NOT EXISTS erp_worksites (
    code        TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    plant_code  TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO erp_worksites (code, name, plant_code) VALUES
    ('ACP_NTUNGAMO', 'Africa Coffee Park · Ntungamo', 'kampala'),
    ('KAMPALA_OFFICE', 'Kampala Regional Office', 'kampala'),
    ('MBARARA', 'Mbarara Processing Unit', 'mbale'),
    ('ENTEBBE', 'Entebbe Warehouse', 'kampala')
ON CONFLICT (code) DO NOTHING;
