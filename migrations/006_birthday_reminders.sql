-- Employee birthdays and reminder deduplication log.

ALTER TABLE erp_employees
    ADD COLUMN IF NOT EXISTS birth_date DATE;

UPDATE erp_employees SET birth_date = v.birth_date::date
FROM (VALUES
    ('EMP-001', '1988-06-09'),
    ('EMP-002', '1992-06-09'),
    ('EMP-003', '1990-03-15'),
    ('EMP-004', '1995-11-22'),
    ('EMP-005', '1987-06-09'),
    ('EMP-006', '1983-08-30')
) AS v(employee_no, birth_date)
WHERE erp_employees.employee_no = v.employee_no AND erp_employees.birth_date IS NULL;

UPDATE erp_employees SET manager_id = (SELECT id FROM erp_employees m WHERE m.employee_no = 'EMP-005')
WHERE employee_no IN ('EMP-001', 'EMP-002', 'EMP-004') AND manager_id IS NULL;

CREATE TABLE IF NOT EXISTS erp_birthday_reminder_log (
    id              BIGSERIAL PRIMARY KEY,
    employee_no     TEXT NOT NULL,
    reminder_on     DATE NOT NULL,
    recipient_role  TEXT NOT NULL CHECK (recipient_role IN ('employee', 'manager', 'hr')),
    recipient_email TEXT NOT NULL,
    event_id        TEXT NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (employee_no, reminder_on, recipient_role, recipient_email)
);

CREATE INDEX IF NOT EXISTS erp_birthday_reminder_log_on_idx
    ON erp_birthday_reminder_log (reminder_on DESC);
