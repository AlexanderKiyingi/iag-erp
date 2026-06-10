ALTER TABLE erp_employees
    ADD COLUMN IF NOT EXISTS operator_ref TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS erp_employees_operator_ref_idx
    ON erp_employees (operator_ref) WHERE operator_ref IS NOT NULL;

UPDATE erp_employees SET operator_ref = v.op_ref
FROM (VALUES
    ('EMP-001', 'OP-001'),
    ('EMP-002', 'OP-002'),
    ('EMP-003', 'OP-003'),
    ('EMP-004', 'OP-004')
) AS v(employee_no, op_ref)
WHERE erp_employees.employee_no = v.employee_no AND erp_employees.operator_ref IS NULL;
