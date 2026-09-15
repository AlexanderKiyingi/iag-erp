-- Give HR documents an owner.
--
-- The IAG HR app has always uploaded to /api/attachments and linked
-- /api/attachments/:id, and nothing has ever answered either: there was no
-- filesystem route and no rewrite, in any configuration. The uploader's
-- fallback then wrote each file into the record itself as a base64 data URL, so
-- nothing surfaced as an error -- files just stopped being files. For employees
-- and departments that meant megabytes of base64 inside the attrs JSONB; for
-- leave requests and payroll runs the adapter dropped the field and the file
-- was simply gone.
--
-- No platform service owned this. iag-dms is Distribution Management, not
-- Document Management -- its /attachments routes belong to distribution orders
-- and are guarded by dms.* permissions. HR is iag-erp's domain, so the store
-- goes here, next to the records the files are evidence for.
--
-- ---------------------------------------------------------------------------
-- Why BYTEA
-- ---------------------------------------------------------------------------
-- The app caps a file at 1.5 MB and a record's attachments at 8 MB, and these
-- are payslip scans, contracts and ID photographs rather than media. At that
-- size and volume, bytes in the same Postgres keep the deployment to one
-- backing store and put a file in the same transaction and the same backup as
-- the row it belongs to.
--
-- That stops being true if the caps rise. Postgres will TOAST a large value out
-- of line and keep working long past the point where this is a good idea, so
-- the ceiling is worth stating rather than discovering: past roughly a few MB
-- per file, or a few GB in total, move the bytes to object storage and keep
-- this table as the metadata and the key. The columns below are already the
-- ones that migration needs.
--
-- ---------------------------------------------------------------------------
-- Owner keys, not a foreign key
-- ---------------------------------------------------------------------------
-- An attachment hangs off whatever the app was looking at -- an employee, a
-- leave request, a payroll run, a row in the generic HR module store. Those are
-- different tables with different key types (employee_no is text, a leave
-- request is a uuid), so the owner is recorded as the app names it and no
-- constraint pretends otherwise. The index is what makes "the files for this
-- record" the cheap query it needs to be.

CREATE TABLE IF NOT EXISTS erp_attachments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_module TEXT NOT NULL DEFAULT '',
    owner_entity TEXT NOT NULL DEFAULT '',
    owner_record_id TEXT NOT NULL DEFAULT '',
    filename     TEXT NOT NULL,
    mime         TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes   BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    -- SHA-256 of the content, so a re-upload of the same file can be
    -- recognised and a stored file can be shown to be the one that was sent.
    checksum_sha256 TEXT NOT NULL DEFAULT '',
    content      BYTEA NOT NULL,
    uploaded_by_employee_no TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_attachments_owner_idx
    ON erp_attachments (owner_module, owner_entity, owner_record_id, created_at DESC);

-- Listing and downloading must never pull the bytes unless the bytes are what
-- was asked for; every read path below selects the metadata columns explicitly.
