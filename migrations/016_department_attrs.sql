-- Give a department somewhere to keep the things HR records about it.
--
-- erp_departments has carried five columns since 002: code, name, plant_code,
-- active and created_at. The HR app's department form has always collected a
-- head of department, a cost centre, a planned headcount, attachments and
-- notes, and every one of them was dropped on write -- the adapter had nowhere
-- to send them, so an operator filled the form in and five of its nine fields
-- went nowhere. The form looked complete, which is why it lasted.
--
-- erp_employees solved the same problem in 002 with a free `attrs` JSONB, and
-- roughly thirty of that form's fields round-trip through it. Departments get
-- the same treatment rather than five new typed columns: none of these is
-- something the service reasons about -- it does not roll up headcount, post to
-- a cost centre, or read a manager -- they are HR's own record of the
-- department. A column the service never reads is a schema promise it does not
-- keep, and promoting one later is easy; demoting it is not.
--
-- Additive and safe on the old binary: existing rows default to '{}' and every
-- current query keeps working, because none of them selects this column.

ALTER TABLE erp_departments
    ADD COLUMN IF NOT EXISTS attrs JSONB NOT NULL DEFAULT '{}';
