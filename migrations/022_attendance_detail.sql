-- Promote the check-in evidence out of the free-text notes column.
--
-- The IAG HR app runs a geofenced check-in: it captures latitude, longitude,
-- GPS accuracy and the Wi-Fi BSSID the device saw, and checks them against the
-- site and block radii HR configured. None of it had a column here, so the
-- adapter packed the lot into `notes` as a delimited string --
--
--     geo 0.31628,32.58219 · +/-12m · wifi a4:2b:8c:00:11:22 · <the note>
--
-- -- and unpacked it with a regex on the way back. It round-trips, and there is
-- a test for it, but an operator who types the separator in their own note
-- corrupts the parse, and `verification_note` cannot be recovered as its own
-- field once merged in.
--
-- This is the evidence produced when somebody disputes a day's attendance. It
-- should not be stored in a format a typo can break.
--
-- ---------------------------------------------------------------------------
-- Why department and block, and why not hours
-- ---------------------------------------------------------------------------
-- `location` (007) holds the site. The app also records which block within it,
-- and which department the person was working for that day -- a casual moved
-- between departments is exactly the row payroll has to allocate, so it belongs
-- on the attendance record rather than being inferred from the employee's
-- current department.
--
-- Hours deliberately gets no column. It is clock_out minus clock_in, and a
-- stored copy disagrees with them the first time either is corrected. The app's
-- Hours column reads blank today because the adapter hardcodes an empty string,
-- not because there was nowhere to put it -- computing it on read fixes that
-- without adding a number that can drift from the two it comes from.
--
-- Nullable coordinates, again on purpose: a punch recorded by an HR officer
-- from a paper register has no GPS fix, and 0,0 is a real place in the Gulf of
-- Guinea rather than a way of saying "unknown".

ALTER TABLE erp_attendance_records
    ADD COLUMN IF NOT EXISTS department_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS block TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS latitude NUMERIC(9, 6),
    ADD COLUMN IF NOT EXISTS longitude NUMERIC(9, 6),
    ADD COLUMN IF NOT EXISTS accuracy_m NUMERIC(8, 2),
    ADD COLUMN IF NOT EXISTS wifi_bssid TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS verification_note TEXT NOT NULL DEFAULT '';

-- Attendance is read by day and by person; a site rollup is the other question
-- asked of it, and it currently scans.
CREATE INDEX IF NOT EXISTS erp_attendance_department_idx
    ON erp_attendance_records (department_code, work_date DESC)
    WHERE department_code <> '';
