-- Recruitment, lifecycle checklists, performance, disciplinary and training.
--
-- These five lived in erp_hr_module_records: one table, a `module` string and
-- an untyped JSONB `data` column, with the same six CRUD handlers serving all
-- of them. No schema, no validation, no state, and no foreign key to
-- erp_employees — a disciplinary case and a training enrolment were the same
-- row shape, and neither was attached to a person the database knew about.
--
-- Each domain here has the thing the JSONB version could not have: a state,
-- and rules about which state may follow which. The transitions are enforced in
-- Go (see the *_transitions.go files) rather than by a trigger, so an invalid
-- move is a 409 with a reason rather than a constraint violation.
--
-- erp_hr_module_records is deliberately left in place and still served at
-- /hr/:module. The frontend is not broken by this migration; it is given
-- somewhere better to go.

-- ---------------------------------------------------------------------------
-- Recruitment
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS erp_job_requisitions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requisition_no    TEXT NOT NULL UNIQUE,
    title             TEXT NOT NULL,
    department_id     UUID REFERENCES erp_departments (id),
    employment_type   TEXT NOT NULL DEFAULT 'permanent'
        CHECK (employment_type IN ('permanent', 'contract', 'casual')),
    headcount         INT NOT NULL DEFAULT 1 CHECK (headcount > 0),
    -- A requisition is a request to hire, and it is approved or it is not.
    status            TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'pending_approval', 'open', 'on_hold', 'filled', 'cancelled')),
    hiring_manager_id UUID REFERENCES erp_employees (id),
    justification     TEXT NOT NULL DEFAULT '',
    target_start_date DATE,
    approved_by_employee_id UUID REFERENCES erp_employees (id),
    approved_at       TIMESTAMPTZ,
    opened_on         DATE,
    closed_on         DATE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_job_requisitions_status_idx
    ON erp_job_requisitions (status, created_at DESC);

-- A candidate is a person, not an application. The same person applying to a
-- second role must not become a second person, or the pipeline cannot tell you
-- who you have already interviewed and rejected.
CREATE TABLE IF NOT EXISTS erp_candidates (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name  TEXT NOT NULL,
    email      TEXT,
    phone      TEXT,
    source     TEXT NOT NULL DEFAULT '',
    cv_ref     TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Email is the practical identity of a candidate, where there is one.
CREATE UNIQUE INDEX IF NOT EXISTS erp_candidates_email_idx
    ON erp_candidates (lower(email)) WHERE email IS NOT NULL AND email <> '';

CREATE TABLE IF NOT EXISTS erp_applications (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requisition_id  UUID NOT NULL REFERENCES erp_job_requisitions (id) ON DELETE CASCADE,
    candidate_id    UUID NOT NULL REFERENCES erp_candidates (id) ON DELETE CASCADE,
    stage           TEXT NOT NULL DEFAULT 'applied'
        CHECK (stage IN ('applied', 'screening', 'interview', 'offer', 'hired', 'rejected', 'withdrawn')),
    applied_on      DATE NOT NULL DEFAULT CURRENT_DATE,
    stage_since     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rejection_reason TEXT NOT NULL DEFAULT '',
    -- Set when the application ends in a hire, linking the pipeline to the
    -- person it produced. This is the ATS→HRIS handoff.
    hired_employee_id UUID REFERENCES erp_employees (id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- One application per person per requisition.
    UNIQUE (requisition_id, candidate_id)
);

CREATE INDEX IF NOT EXISTS erp_applications_stage_idx
    ON erp_applications (requisition_id, stage);

-- Every stage change, kept. A pipeline without its history cannot answer why
-- somebody was rejected, or how long they sat at screening.
CREATE TABLE IF NOT EXISTS erp_application_events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES erp_applications (id) ON DELETE CASCADE,
    from_stage     TEXT NOT NULL DEFAULT '',
    to_stage       TEXT NOT NULL,
    note           TEXT NOT NULL DEFAULT '',
    actor_employee_no TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_application_events_app_idx
    ON erp_application_events (application_id, created_at);

CREATE TABLE IF NOT EXISTS erp_interviews (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES erp_applications (id) ON DELETE CASCADE,
    scheduled_at   TIMESTAMPTZ NOT NULL,
    interviewer_employee_id UUID REFERENCES erp_employees (id),
    mode           TEXT NOT NULL DEFAULT 'in_person'
        CHECK (mode IN ('in_person', 'phone', 'video', 'panel')),
    outcome        TEXT NOT NULL DEFAULT 'pending'
        CHECK (outcome IN ('pending', 'pass', 'fail', 'no_show', 'cancelled')),
    score          NUMERIC(5, 2),
    notes          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_interviews_application_idx
    ON erp_interviews (application_id, scheduled_at);

CREATE TABLE IF NOT EXISTS erp_offers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES erp_applications (id) ON DELETE CASCADE,
    monthly_gross  NUMERIC(20, 2) NOT NULL CHECK (monthly_gross > 0),
    currency       TEXT NOT NULL DEFAULT 'UGX',
    start_date     DATE,
    status         TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'sent', 'accepted', 'declined', 'withdrawn', 'expired')),
    expires_on     DATE,
    sent_at        TIMESTAMPTZ,
    decided_at     TIMESTAMPTZ,
    notes          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Only one live offer per application: two outstanding offers for one role is
-- an ambiguity about what was actually promised.
CREATE UNIQUE INDEX IF NOT EXISTS erp_offers_live_idx
    ON erp_offers (application_id) WHERE status IN ('draft', 'sent', 'accepted');

-- ---------------------------------------------------------------------------
-- Onboarding and offboarding: one checklist model, two kinds
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS erp_checklist_templates (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('onboarding', 'offboarding')),
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS erp_checklist_template_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES erp_checklist_templates (id) ON DELETE CASCADE,
    seq         INT NOT NULL,
    title       TEXT NOT NULL,
    owner_role  TEXT NOT NULL DEFAULT 'hr',
    -- Days from the checklist's start date. Negative is before it: an
    -- offboarding item due three days before the last day is the normal case.
    due_offset_days INT NOT NULL DEFAULT 0,
    required    BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (template_id, seq)
);

-- A checklist issued to one person. Items are copied from the template at issue
-- time, so editing a template never rewrites a checklist somebody is working
-- through, or one already completed and signed off.
CREATE TABLE IF NOT EXISTS erp_employee_checklists (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id  UUID NOT NULL REFERENCES erp_employees (id) ON DELETE CASCADE,
    template_id  UUID REFERENCES erp_checklist_templates (id),
    kind         TEXT NOT NULL CHECK (kind IN ('onboarding', 'offboarding')),
    status       TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'completed', 'cancelled')),
    reference_date DATE NOT NULL,
    completed_on TIMESTAMPTZ,
    notes        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One open checklist of each kind per person. A second open offboarding is a
-- duplicate, not a second departure.
CREATE UNIQUE INDEX IF NOT EXISTS erp_employee_checklists_open_idx
    ON erp_employee_checklists (employee_id, kind) WHERE status = 'open';

CREATE TABLE IF NOT EXISTS erp_employee_checklist_items (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    checklist_id UUID NOT NULL REFERENCES erp_employee_checklists (id) ON DELETE CASCADE,
    seq          INT NOT NULL,
    title        TEXT NOT NULL,
    owner_role   TEXT NOT NULL DEFAULT 'hr',
    assignee_employee_id UUID REFERENCES erp_employees (id),
    required     BOOLEAN NOT NULL DEFAULT true,
    status       TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'done', 'not_applicable', 'blocked')),
    due_on       DATE,
    completed_at TIMESTAMPTZ,
    completed_by_employee_id UUID REFERENCES erp_employees (id),
    notes        TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS erp_employee_checklist_items_list_idx
    ON erp_employee_checklist_items (checklist_id, seq);

-- Two starting templates. Both are ordinary rows an HR officer edits.
INSERT INTO erp_checklist_templates (code, name, kind)
SELECT v.code, v.name, v.kind FROM (VALUES
    ('ONB-STD', 'Standard onboarding', 'onboarding'),
    ('OFF-STD', 'Standard offboarding', 'offboarding')
) AS v(code, name, kind)
WHERE NOT EXISTS (SELECT 1 FROM erp_checklist_templates t WHERE t.code = v.code);

INSERT INTO erp_checklist_template_items (template_id, seq, title, owner_role, due_offset_days, required)
SELECT t.id, v.seq, v.title, v.owner_role, v.due_offset, v.required
FROM erp_checklist_templates t
JOIN (VALUES
    ('ONB-STD', 1, 'Signed contract on file',            'hr',      -3, true),
    ('ONB-STD', 2, 'Platform user account created',      'it',       0, true),
    ('ONB-STD', 3, 'Payroll and NSSF details captured',  'payroll',  1, true),
    ('ONB-STD', 4, 'Workstation and assets issued',      'it',       0, true),
    ('ONB-STD', 5, 'Induction and safety briefing',      'hr',       5, true),
    ('ONB-STD', 6, 'Probation objectives agreed',        'manager', 10, true),
    ('OFF-STD', 1, 'Resignation or termination letter on file', 'hr',      -14, true),
    ('OFF-STD', 2, 'Handover document accepted',         'manager',  -3, true),
    ('OFF-STD', 3, 'Company assets returned',            'it',        0, true),
    ('OFF-STD', 4, 'Platform access revoked',            'it',        0, true),
    ('OFF-STD', 5, 'Final pay and leave settlement',     'payroll',   5, true),
    ('OFF-STD', 6, 'Exit interview completed',           'hr',        0, false)
) AS v(template_code, seq, title, owner_role, due_offset, required)
  ON v.template_code = t.code
WHERE NOT EXISTS (
    SELECT 1 FROM erp_checklist_template_items i WHERE i.template_id = t.id AND i.seq = v.seq
);

-- ---------------------------------------------------------------------------
-- Performance
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS erp_review_cycles (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code         TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    period_start DATE NOT NULL,
    period_end   DATE NOT NULL,
    status       TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'open', 'closed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (period_end >= period_start)
);

-- The review moves through self-assessment, the manager's assessment, and being
-- shared with and acknowledged by the employee. The acknowledgement is the
-- point of the record: a review the employee never saw is not a review.
CREATE TABLE IF NOT EXISTS erp_performance_reviews (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cycle_id    UUID NOT NULL REFERENCES erp_review_cycles (id) ON DELETE CASCADE,
    employee_id UUID NOT NULL REFERENCES erp_employees (id) ON DELETE CASCADE,
    reviewer_employee_id UUID REFERENCES erp_employees (id),
    status      TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'self_review', 'manager_review', 'calibration',
                          'shared', 'acknowledged', 'cancelled')),
    overall_rating NUMERIC(4, 2) CHECK (overall_rating IS NULL OR (overall_rating >= 1 AND overall_rating <= 5)),
    self_comments    TEXT NOT NULL DEFAULT '',
    manager_comments TEXT NOT NULL DEFAULT '',
    submitted_at    TIMESTAMPTZ,
    shared_at       TIMESTAMPTZ,
    acknowledged_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cycle_id, employee_id)
);

CREATE INDEX IF NOT EXISTS erp_performance_reviews_employee_idx
    ON erp_performance_reviews (employee_id, created_at DESC);

CREATE TABLE IF NOT EXISTS erp_review_ratings (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id  UUID NOT NULL REFERENCES erp_performance_reviews (id) ON DELETE CASCADE,
    competency TEXT NOT NULL,
    rating     NUMERIC(4, 2) NOT NULL CHECK (rating >= 1 AND rating <= 5),
    weight     NUMERIC(5, 2) NOT NULL DEFAULT 1 CHECK (weight > 0),
    comment    TEXT NOT NULL DEFAULT '',
    UNIQUE (review_id, competency)
);

CREATE TABLE IF NOT EXISTS erp_performance_goals (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id UUID NOT NULL REFERENCES erp_employees (id) ON DELETE CASCADE,
    cycle_id    UUID REFERENCES erp_review_cycles (id) ON DELETE SET NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metric      TEXT NOT NULL DEFAULT '',
    target      TEXT NOT NULL DEFAULT '',
    weight      NUMERIC(5, 2) NOT NULL DEFAULT 1 CHECK (weight > 0),
    progress_pct NUMERIC(5, 2) NOT NULL DEFAULT 0
        CHECK (progress_pct >= 0 AND progress_pct <= 100),
    status      TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'achieved', 'missed', 'cancelled')),
    due_on      DATE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_performance_goals_employee_idx
    ON erp_performance_goals (employee_id, status);

-- ---------------------------------------------------------------------------
-- Disciplinary
-- ---------------------------------------------------------------------------

-- The most legally consequential record in this service. Every state change is
-- logged with who made it, because "was due process followed" is a question
-- about the sequence of events and the dates they happened on.
CREATE TABLE IF NOT EXISTS erp_disciplinary_cases (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_no     TEXT NOT NULL UNIQUE,
    employee_id UUID NOT NULL REFERENCES erp_employees (id) ON DELETE CASCADE,
    category    TEXT NOT NULL DEFAULT 'conduct'
        CHECK (category IN ('conduct', 'performance', 'attendance', 'safety', 'policy', 'other')),
    severity    TEXT NOT NULL DEFAULT 'minor'
        CHECK (severity IN ('minor', 'serious', 'gross')),
    status      TEXT NOT NULL DEFAULT 'reported'
        CHECK (status IN ('reported', 'under_investigation', 'hearing_scheduled',
                          'decided', 'appealed', 'closed', 'dismissed')),
    description TEXT NOT NULL DEFAULT '',
    reported_by_employee_id UUID REFERENCES erp_employees (id),
    reported_on DATE NOT NULL DEFAULT CURRENT_DATE,
    investigator_employee_id UUID REFERENCES erp_employees (id),
    hearing_on  TIMESTAMPTZ,
    outcome     TEXT NOT NULL DEFAULT ''
        CHECK (outcome IN ('', 'no_case', 'verbal_warning', 'written_warning',
                           'final_warning', 'suspension', 'demotion', 'dismissal')),
    sanction_notes TEXT NOT NULL DEFAULT '',
    decided_on  DATE,
    decided_by_employee_id UUID REFERENCES erp_employees (id),
    -- An appeal window that has not passed is why a case stays open after the
    -- decision rather than closing on it.
    appeal_deadline DATE,
    closed_on   DATE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_disciplinary_cases_employee_idx
    ON erp_disciplinary_cases (employee_id, reported_on DESC);
CREATE INDEX IF NOT EXISTS erp_disciplinary_cases_status_idx
    ON erp_disciplinary_cases (status);

CREATE TABLE IF NOT EXISTS erp_disciplinary_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id     UUID NOT NULL REFERENCES erp_disciplinary_cases (id) ON DELETE CASCADE,
    from_status TEXT NOT NULL DEFAULT '',
    to_status   TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    actor_employee_no TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_disciplinary_events_case_idx
    ON erp_disciplinary_events (case_id, occurred_at);

-- ---------------------------------------------------------------------------
-- Training
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS erp_training_courses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    provider    TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    duration_hours NUMERIC(6, 2) NOT NULL DEFAULT 0,
    mandatory   BOOLEAN NOT NULL DEFAULT false,
    -- How long a pass stays valid. NULL never expires; a food-safety or
    -- first-aid certificate does, and an expiry nobody records is a compliance
    -- gap that looks like compliance.
    validity_months INT,
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS erp_training_enrolments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id   UUID NOT NULL REFERENCES erp_training_courses (id) ON DELETE CASCADE,
    employee_id UUID NOT NULL REFERENCES erp_employees (id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'enrolled'
        CHECK (status IN ('enrolled', 'in_progress', 'completed', 'failed', 'cancelled')),
    enrolled_on DATE NOT NULL DEFAULT CURRENT_DATE,
    started_on  DATE,
    completed_on DATE,
    score       NUMERIC(5, 2),
    expires_on  DATE,
    certificate_ref TEXT NOT NULL DEFAULT '',
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One live enrolment per person per course. Re-taking after a pass expires is a
-- new enrolment, which is why only the unfinished ones are constrained.
CREATE UNIQUE INDEX IF NOT EXISTS erp_training_enrolments_live_idx
    ON erp_training_enrolments (course_id, employee_id)
    WHERE status IN ('enrolled', 'in_progress');

CREATE INDEX IF NOT EXISTS erp_training_enrolments_employee_idx
    ON erp_training_enrolments (employee_id, status);

-- Certificates coming up for expiry — the query a compliance officer runs.
CREATE INDEX IF NOT EXISTS erp_training_enrolments_expiry_idx
    ON erp_training_enrolments (expires_on) WHERE status = 'completed' AND expires_on IS NOT NULL;
