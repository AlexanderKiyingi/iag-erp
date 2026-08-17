-- Work calendar: weekends and public holidays are not leave.
--
-- Leave days were computed as (end - start) + 1 calendar days, so a Friday to
-- Monday request consumed four days of entitlement instead of two. That figure
-- is not only wrong on the balance: it is multiplied by a daily rate and posted
-- to finance as an accrued-leave liability, so the error carries into money.
--
-- The 'holiday' setup-item type already existed and nothing read it. This
-- migration makes it the source of record for non-working days and gives it an
-- index the leave path can hit on every request.

-- Holidays are looked up by date range on every leave create/update. Without
-- this the lookup is a sequential scan over every setup item there will ever be.
CREATE INDEX IF NOT EXISTS erp_setup_items_holiday_date_idx
    ON erp_setup_items (effective_date)
    WHERE item_type = 'holiday';

-- Calendar span is kept alongside the working-day figure. `days` is what the
-- employee is charged; `calendar_days` is what they were away, and the two
-- differing is the evidence the calendar was applied at all.
ALTER TABLE erp_leave_requests
    ADD COLUMN IF NOT EXISTS calendar_days NUMERIC(6, 2) NOT NULL DEFAULT 0;

-- Existing rows were charged calendar days, so that is what their span was.
-- Their `days` is left alone: recomputing history would silently restate
-- balances that have already been reported and posted.
UPDATE erp_leave_requests SET calendar_days = days WHERE calendar_days = 0;

-- Who decided, as an employee rather than a free-text ref. approver_ref stays
-- for the display name the frontend already sends; this is the identity the
-- approval-authority check is verified against and audited on.
ALTER TABLE erp_leave_requests
    ADD COLUMN IF NOT EXISTS decided_by_employee_id UUID REFERENCES erp_employees (id);

CREATE INDEX IF NOT EXISTS erp_leave_requests_decided_by_idx
    ON erp_leave_requests (decided_by_employee_id);

-- Uganda public holidays. Dates that move with the lunar calendar (Eid al-Fitr,
-- Eid al-Adha) are approximate and must be confirmed against the official
-- gazette each year — they are seeded so the list is not silently short, and
-- they are ordinary rows an HR officer can correct.
-- Seeded only where the code is not already present: erp_setup_items has no
-- unique constraint on code, so ON CONFLICT would not stop a re-run duplicating
-- every holiday and double-counting it out of everyone's entitlement.
INSERT INTO erp_setup_items (item_type, name, code, status, effective_date, description)
SELECT v.item_type, v.name, v.code, 'active', v.effective_date::date, v.description
FROM (VALUES
    ('holiday', 'New Year''s Day',            'UG-NY-2026',    '2026-01-01', 'Uganda public holiday'),
    ('holiday', 'NRM Liberation Day',         'UG-NRM-2026',   '2026-01-26', 'Uganda public holiday'),
    ('holiday', 'Archbishop Janani Luwum Day','UG-LUWUM-2026', '2026-02-16', 'Uganda public holiday'),
    ('holiday', 'International Women''s Day', 'UG-IWD-2026',   '2026-03-08', 'Uganda public holiday'),
    ('holiday', 'Good Friday',                'UG-GF-2026',    '2026-04-03', 'Uganda public holiday'),
    ('holiday', 'Easter Monday',              'UG-EM-2026',    '2026-04-06', 'Uganda public holiday'),
    ('holiday', 'Labour Day',                 'UG-LAB-2026',   '2026-05-01', 'Uganda public holiday'),
    ('holiday', 'Martyrs'' Day',              'UG-MART-2026',  '2026-06-03', 'Uganda public holiday'),
    ('holiday', 'National Heroes Day',        'UG-HERO-2026',  '2026-06-09', 'Uganda public holiday'),
    ('holiday', 'Independence Day',           'UG-IND-2026',   '2026-10-09', 'Uganda public holiday'),
    ('holiday', 'Christmas Day',              'UG-XMAS-2026',  '2026-12-25', 'Uganda public holiday'),
    ('holiday', 'Boxing Day',                 'UG-BOX-2026',   '2026-12-26', 'Uganda public holiday'),
    ('holiday', 'Eid al-Fitr (confirm)',      'UG-EIDF-2026',  '2026-03-20', 'Lunar — confirm against the gazette'),
    ('holiday', 'Eid al-Adha (confirm)',      'UG-EIDA-2026',  '2026-05-27', 'Lunar — confirm against the gazette')
) AS v(item_type, name, code, effective_date, description)
WHERE NOT EXISTS (
    SELECT 1 FROM erp_setup_items s WHERE s.item_type = 'holiday' AND s.code = v.code
);
