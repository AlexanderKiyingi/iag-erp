-- Give a payroll run somewhere to keep how it was computed.
--
-- The IAG HR app's payroll panel records things about a run that this service
-- has no column for: the per-employee overrides an operator applied before
-- computing it, the default working days the run assumed, and the ids of the
-- payslips it produced. All of them were dropped on write.
--
-- These go in the free attrs map rather than typed columns, which is the
-- opposite call to the salary band in 021 -- and for the reason 016 states.
-- The service computes its own runs from live pay data and never reads an
-- override somebody applied in a browser; a column it does not read is a schema
-- promise it does not keep. What matters is that the figures can be explained
-- afterwards, and for that the map is enough.
--
-- The run's totals are NOT here and must not be: gross, PAYE, NSSF, net and the
-- employee count are computed upstream and are already columns. A second copy
-- arriving from a client would be a set of numbers that could disagree with the
-- payslips they are supposed to summarise.
--
-- Additive and empty by default; nothing selects it until the adapter does.

ALTER TABLE erp_payroll_runs
    ADD COLUMN IF NOT EXISTS attrs JSONB NOT NULL DEFAULT '{}';
