INSERT INTO erp_departments (code, name, plant_code)
VALUES
    ('HR', 'Human Resources', 'kampala'),
    ('PROD', 'Production', 'kampala'),
    ('MAINT', 'Maintenance', 'kampala'),
    ('QC', 'Quality Control', 'kampala'),
    ('LOG', 'Logistics', 'mbale')
ON CONFLICT (code) DO NOTHING;

INSERT INTO erp_leave_types (code, name, paid, days_per_year)
VALUES
    ('ANNUAL', 'Annual leave', true, 21),
    ('SICK', 'Sick leave', true, 14),
    ('UNPAID', 'Unpaid leave', false, 0),
    ('MATERNITY', 'Maternity leave', true, 60)
ON CONFLICT (code) DO NOTHING;

INSERT INTO erp_employees (employee_no, first_name, last_name, email, phone, department_id, job_title, employment_type, status, hire_date, plant_code)
SELECT v.employee_no, v.first_name, v.last_name, v.email, v.phone, d.id, v.job_title, v.employment_type, 'active', v.hire_date::date, v.plant_code
FROM (VALUES
    ('EMP-001', 'James', 'Mukasa', 'j.mukasa@iag.ug', '+256700100001', 'MAINT', 'Maintenance lead', 'permanent', '2022-03-01', 'kampala'),
    ('EMP-002', 'Sarah', 'Akello', 's.akello@iag.ug', '+256700100002', 'PROD', 'Roaster operator', 'permanent', '2021-06-15', 'kampala'),
    ('EMP-003', 'Faith', 'Nansubuga', 'f.nansubuga@iag.ug', '+256700100003', 'MAINT', 'Senior technician', 'permanent', '2020-01-10', 'kampala'),
    ('EMP-004', 'David', 'Wamala', 'd.wamala@iag.ug', '+256700100004', 'PROD', 'Packaging operator', 'contract', '2023-09-01', 'kampala'),
    ('EMP-005', 'Grace', 'Namukasa', 'g.namukasa@iag.ug', '+256700100005', 'HR', 'HR officer', 'permanent', '2019-11-20', 'kampala'),
    ('EMP-006', 'Samuel', 'Okello', 's.okello@iag.ug', '+256700100006', 'PROD', 'Hulling specialist', 'permanent', '2018-04-05', 'mbale')
) AS v(employee_no, first_name, last_name, email, phone, dept_code, job_title, employment_type, hire_date, plant_code)
JOIN erp_departments d ON d.code = v.dept_code
ON CONFLICT (employee_no) DO NOTHING;

INSERT INTO erp_production_orders (po_num, customer, product, qty_kg, origin_lot, asset_tag, status, due_at, erp_ref)
VALUES
    ('PO-2401', 'Volcafe', 'Arabica FAQ', 12000, 'LOT-KLA-2401', 'WM-01', 'queued', NOW() + INTERVAL '5 days', 'ERP-PO-2401'),
    ('PO-2402', 'Sucafina', 'Robusta Screen 15', 8000, 'LOT-MBL-2402', 'DRY-A1', 'scheduled', NOW() + INTERVAL '3 days', 'ERP-PO-2402'),
    ('PO-2403', 'Olam', 'Specialty Natural', 5000, 'LOT-KLA-2403', 'R1', 'running', NOW() + INTERVAL '1 day', 'ERP-PO-2403')
ON CONFLICT (po_num) DO NOTHING;
