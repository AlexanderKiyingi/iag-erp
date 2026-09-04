-- Record why a leave request was decided, not only who decided it and when.
--
-- erp_leave_requests carries approver_ref, decided_at and decided_by_employee_id
-- -- everything about a decision except its reason. Every approval desk that
-- talks to this service collects a comment, and a rejection without one is the
-- case where it matters most: the employee is told no and cannot be told why.
--
-- The HR app's decision route had to drop the comment on the floor for exactly
-- this reason. Writing it into approver_ref was the alternative, and that puts a
-- sentence where a person's reference belongs.
--
-- Additive and empty by default, so a decision made without a note reads as one.

ALTER TABLE erp_leave_requests
    ADD COLUMN IF NOT EXISTS decision_note TEXT NOT NULL DEFAULT '';
