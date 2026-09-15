-- Give a job requisition its salary band, and a checklist its buddy.
--
-- Both forms in the IAG HR app collect fields that reached this service and
-- were dropped on the floor, because the adapter had nowhere to send them. The
-- form looked complete, which is why it lasted -- the same shape of gap as the
-- five department fields 016 closed.
--
-- ---------------------------------------------------------------------------
-- 1. Requisitions: a salary band, a posting date, and a home for the rest
-- ---------------------------------------------------------------------------
-- erp_job_requisitions has carried no money since 011. The app's job-positions
-- form collects a minimum, a maximum and the date the role was advertised, and
-- all three went nowhere -- so a requisition could be approved without anybody
-- being able to say what it would cost, and an offer made against it had no
-- band to be checked against.
--
-- These get typed columns rather than the free attrs map, which is the opposite
-- call to the one 016 made for departments, and for the reason 016 gives: attrs
-- is right for what the service will never reason about. A salary band is not
-- that. An offer is made against it, headcount planning sums it, and a column
-- the service reads is worth the schema promise. The form's own free-text code
-- has no such future and goes in attrs.
--
-- No CHECK that max >= min. A band is sometimes entered one end at a time, and
-- a constraint that rejects a half-filled draft would push the form back to
-- collecting nothing.

ALTER TABLE erp_job_requisitions
    ADD COLUMN IF NOT EXISTS salary_min NUMERIC(20, 2),
    ADD COLUMN IF NOT EXISTS salary_max NUMERIC(20, 2),
    ADD COLUMN IF NOT EXISTS salary_currency TEXT NOT NULL DEFAULT 'UGX',
    ADD COLUMN IF NOT EXISTS posted_on DATE,
    ADD COLUMN IF NOT EXISTS attrs JSONB NOT NULL DEFAULT '{}';

-- Nullable on purpose. A requisition raised before this migration has no band,
-- and zero would state one -- "this role pays nothing" rather than "nobody said".

-- ---------------------------------------------------------------------------
-- 2. Checklists: the buddy, and nothing else
-- ---------------------------------------------------------------------------
-- The onboarding form collects a position, a department and a buddy. Only the
-- last is new information.
--
-- Position and department are already on the employee row, so they are returned
-- by joining rather than stored here. A copy taken when the checklist was
-- issued is wrong the moment somebody transfers, and an onboarding record that
-- disagrees with the employee directory about which department a person is in
-- is worse than one that says nothing.
--
-- The buddy is a person, so it is an employee number rather than a name --
-- names are not unique and do not survive a marriage. Not a foreign key: a
-- buddy may be somebody who has since left, and a constraint would either block
-- that or delete the record of who showed them around.

ALTER TABLE erp_employee_checklists
    ADD COLUMN IF NOT EXISTS buddy_employee_no TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS attrs JSONB NOT NULL DEFAULT '{}';
