-- Let a payroll run say who computed it.
--
-- iag-erp computes payroll itself: CreatePayrollRun reads compensation and pay
-- components and derives every payslip. The HR app has its own engine
-- (batch-payroll.ts) that has been the operator-facing one all along, and its
-- payslips had nowhere to go -- they were written to a read-only adapter, which
-- silently stored nothing.
--
-- Rather than a second table duplicating /payroll/runs, /payslips, the approve/
-- post verbs, the ledger event and the whole frontend mapping, a run records
-- where its figures came from. One column and one WHERE clause is the smaller
-- honest change.
--
-- Two consequences the code depends on:
--
--   * CreatePayrollRun replaces same-period drafts with
--     `DELETE ... WHERE period = $1 AND status IN ('draft','cancelled')`.
--     That must not reach an externally-supplied run, so the delete is narrowed
--     to source = 'computed'. Without this column, recomputing a period would
--     silently destroy the payroll somebody had already submitted.
--   * The partial unique index on (period) WHERE status = 'posted' stays global
--     and unchanged. Two runs for one period must still never both post,
--     whoever computed them -- that control is the point, and making it
--     per-source would quietly allow paying a month twice.
--
-- Approval and posting are deliberately NOT relaxed: an external run lands as
-- 'draft' and still needs a different person to approve it and an
-- erp.post_payroll holder to post it. Separation of duties must not be
-- bypassable by choosing a different endpoint.
--
-- Additive: existing rows default to 'computed', which is what they are.

ALTER TABLE erp_payroll_runs
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'computed';

ALTER TABLE erp_payroll_runs
    DROP CONSTRAINT IF EXISTS erp_payroll_runs_source_check;

ALTER TABLE erp_payroll_runs
    ADD CONSTRAINT erp_payroll_runs_source_check
    CHECK (source IN ('computed', 'external'));

-- Which engine, and which version of it. A payslip that has to be explained
-- two years later needs more than "not us".
ALTER TABLE erp_payroll_runs
    ADD COLUMN IF NOT EXISTS source_engine TEXT NOT NULL DEFAULT '';
