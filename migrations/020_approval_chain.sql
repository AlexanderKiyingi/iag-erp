-- Give the approval desks a home in the service that holds the record.
--
-- The IAG HR app runs a five-desk payroll chain (Requestor -> Accounts
-- Assistant -> GM -> CEO -> Finance -> Paid) and a three-desk leave chain
-- (Requestor -> HOD -> HR). This service models payroll as
-- draft -> approved -> posted and leave as pending -> approved. The
-- intermediate desks existed only in the browser, so the whole audit trail --
-- who advanced it, who returned it for amendment, who rejected it and why --
-- had nowhere to go, and the app posted its approvals to a different backend
-- entirely rather than to the service that holds the run.
--
-- ---------------------------------------------------------------------------
-- Why chain_stage rather than widening the status CHECK
-- ---------------------------------------------------------------------------
-- status is what this service's own invariants key off. The unique index on
-- (period) WHERE status = 'posted' is what stops a month's payroll being
-- double-booked into the ledger, and the three payroll permissions attach to
-- the three status transitions. Widening that CHECK to carry five desks would
-- put the double-booking guard and the separation of duties at risk in order to
-- describe an org chart.
--
-- So status keeps its meaning and chain_stage carries the desk. Everything
-- before CEO sign-off is status = 'draft' at a named stage; CEO sign-off is the
-- existing approve verb; the Finance release is the existing post verb. A
-- reader who knows only status still reads this table correctly, which is the
-- property that matters for anything else that queries it.
--
-- chain_stage is empty for every existing row, and empty means "this run never
-- went through the desks" -- which is true of every run created before now, and
-- stays true of any created by another client. Nothing infers a desk it did not
-- visit.
--
-- ---------------------------------------------------------------------------
-- Why an event table rather than columns on the subject
-- ---------------------------------------------------------------------------
-- The app collects a comment on every hop and a reason on every rejection and
-- amendment. Columns would hold only the latest, and the question an approval
-- trail exists to answer -- who sent this back, and what did they say -- is
-- about the hop, not the current state. erp_disciplinary_events and
-- erp_application_events are the same shape for the same reason.
--
-- Append-only by intent: there is no update path, and a correction is another
-- event. Nothing here is deleted while its subject exists.
--
-- Additive and safe on the running binary: both columns default to empty, the
-- new table is unreferenced until the code that writes it ships, and no
-- existing query selects either.

ALTER TABLE erp_payroll_runs
    ADD COLUMN IF NOT EXISTS chain_stage TEXT NOT NULL DEFAULT '';

ALTER TABLE erp_leave_requests
    ADD COLUMN IF NOT EXISTS chain_stage TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS erp_approval_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Which kind of thing was approved. Not a foreign key: the two subjects
    -- live in different tables, and a chain that could only ever point at one
    -- of them would need copying for the next.
    subject_type TEXT NOT NULL
        CHECK (subject_type IN ('payroll_run', 'leave_request')),
    subject_id   UUID NOT NULL,
    action       TEXT NOT NULL
        CHECK (action IN ('advance', 'reject', 'amend')),
    from_stage   TEXT NOT NULL DEFAULT '',
    to_stage     TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    actor_employee_no TEXT NOT NULL DEFAULT '',
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The trail is always read for one subject, oldest first -- it is a history.
CREATE INDEX IF NOT EXISTS erp_approval_events_subject_idx
    ON erp_approval_events (subject_type, subject_id, occurred_at);
