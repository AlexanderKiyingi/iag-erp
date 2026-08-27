-- Purge the demo employees and production orders seeded by 003_seed.sql.
--
-- Deliberately preserved:
--   * erp_departments and erp_leave_types (also 003_seed.sql) — organisational and
--     leave-policy configuration, not demo records.
--   * The PAYE bands and NSSF rates seeded by 010_payroll.sql — statutory reference
--     data the payroll engine computes against.
--
-- erp_employees is referenced by sixteen HR tables (leave, payroll, performance,
-- onboarding, recruitment, worksites, …). Rather than enumerate every foreign key,
-- each row is deleted in its own subtransaction and skipped if it is still
-- referenced, so an employee record an operator has since built HR history against
-- survives and the migration cannot fail on a foreign key.

-- The migration runner already wraps every file in a single transaction holding an
-- advisory lock, so this file must not open one of its own: a COMMIT here would end
-- that outer transaction early and release the lock mid-run.

DELETE FROM erp_production_orders
WHERE po_num IN ('PO-2401', 'PO-2402', 'PO-2403')
  AND erp_ref IN ('ERP-PO-2401', 'ERP-PO-2402', 'ERP-PO-2403');

DO $$
DECLARE
    demo_no TEXT;
    kept    INT := 0;
BEGIN
    FOREACH demo_no IN ARRAY ARRAY[
        'EMP-001', 'EMP-002', 'EMP-003', 'EMP-004', 'EMP-005', 'EMP-006'
    ]
    LOOP
        BEGIN
            DELETE FROM erp_employees WHERE employee_no = demo_no;
        EXCEPTION WHEN foreign_key_violation THEN
            kept := kept + 1;
            RAISE NOTICE 'erp_employees % still has HR records — kept', demo_no;
        END;
    END LOOP;
    IF kept > 0 THEN
        RAISE NOTICE 'purge: % demo employee(s) retained because live HR rows reference them', kept;
    END IF;
END $$;
