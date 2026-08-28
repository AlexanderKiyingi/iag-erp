package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Recruitment.
//
// Previously twelve JSONB documents under module = 'recruitment'. A requisition,
// a candidate, an application and an offer were the same shape, none referenced
// each other, and the pipeline stage was a string nobody checked.

type JobRequisition struct {
	ID              uuid.UUID  `json:"id"`
	RequisitionNo   string     `json:"requisition_no"`
	Title           string     `json:"title"`
	DepartmentCode  *string    `json:"department_code,omitempty"`
	EmploymentType  string     `json:"employment_type"`
	Headcount       int        `json:"headcount"`
	Status          string     `json:"status"`
	HiringManagerNo *string    `json:"hiring_manager_employee_no,omitempty"`
	Justification   string     `json:"justification"`
	TargetStartDate *time.Time `json:"target_start_date,omitempty"`
	ApprovedBy      *string    `json:"approved_by_employee_no,omitempty"`
	ApprovedAt      *time.Time `json:"approved_at,omitempty"`
	OpenedOn        *time.Time `json:"opened_on,omitempty"`
	ClosedOn        *time.Time `json:"closed_on,omitempty"`
	// FilledCount is how many of the headcount have been hired, so an open
	// requisition can say whether it is still recruiting.
	FilledCount int       `json:"filled_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Candidate struct {
	ID        uuid.UUID `json:"id"`
	FullName  string    `json:"full_name"`
	Email     *string   `json:"email,omitempty"`
	Phone     *string   `json:"phone,omitempty"`
	Source    string    `json:"source"`
	CVRef     string    `json:"cv_ref"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
}

type Application struct {
	ID               uuid.UUID `json:"id"`
	RequisitionID    uuid.UUID `json:"requisition_id"`
	RequisitionNo    string    `json:"requisition_no"`
	RequisitionTitle string    `json:"requisition_title"`
	CandidateID      uuid.UUID `json:"candidate_id"`
	CandidateName    string    `json:"candidate_name"`
	Stage            string    `json:"stage"`
	AppliedOn        time.Time `json:"applied_on"`
	StageSince       time.Time `json:"stage_since"`
	RejectionReason  string    `json:"rejection_reason"`
	HiredEmployeeNo  *string   `json:"hired_employee_no,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ApplicationEvent struct {
	FromStage string    `json:"from_stage"`
	ToStage   string    `json:"to_stage"`
	Note      string    `json:"note"`
	Actor     string    `json:"actor_employee_no"`
	CreatedAt time.Time `json:"created_at"`
}

const requisitionColumns = `r.id, r.requisition_no, r.title, d.code, r.employment_type,
	r.headcount, r.status, m.employee_no, r.justification, r.target_start_date,
	a.employee_no, r.approved_at, r.opened_on, r.closed_on,
	(SELECT COUNT(*)::int FROM erp_applications ap WHERE ap.requisition_id = r.id AND ap.stage = 'hired'),
	r.created_at, r.updated_at`

const requisitionFrom = `FROM erp_job_requisitions r
	LEFT JOIN erp_departments d ON d.id = r.department_id
	LEFT JOIN erp_employees m ON m.id = r.hiring_manager_id
	LEFT JOIN erp_employees a ON a.id = r.approved_by_employee_id`

func scanRequisition(row pgx.Row) (*JobRequisition, error) {
	var r JobRequisition
	err := row.Scan(&r.ID, &r.RequisitionNo, &r.Title, &r.DepartmentCode, &r.EmploymentType,
		&r.Headcount, &r.Status, &r.HiringManagerNo, &r.Justification, &r.TargetStartDate,
		&r.ApprovedBy, &r.ApprovedAt, &r.OpenedOn, &r.ClosedOn, &r.FilledCount,
		&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

type CreateRequisitionInput struct {
	Title           string `json:"title"`
	DepartmentCode  string `json:"department_code"`
	EmploymentType  string `json:"employment_type"`
	Headcount       int    `json:"headcount"`
	HiringManagerNo string `json:"hiring_manager_employee_no"`
	Justification   string `json:"justification"`
	TargetStartDate string `json:"target_start_date"`
}

func (s *Store) CreateRequisition(ctx context.Context, in CreateRequisitionInput) (*JobRequisition, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, ErrBadInput
	}
	if in.Headcount <= 0 {
		in.Headcount = 1
	}
	// Lower-cased for the same reason CreateEmployee does it (hr.go): the
	// column's CHECK is lower-case, and a caller sending "Permanent" from a
	// form's Title Case option list would otherwise 500 on the constraint
	// rather than be understood. The two paths describe the same vocabulary,
	// so they have to normalise it the same way.
	employmentType, ok := NormaliseEmploymentType(in.EmploymentType)
	if !ok {
		return nil, ErrBadInput
	}
	if employmentType == "" {
		employmentType = "permanent"
	}

	var target *time.Time
	if in.TargetStartDate != "" {
		t, err := time.Parse("2006-01-02", in.TargetStartDate)
		if err != nil {
			return nil, ErrBadInput
		}
		target = &t
	}

	reqNo := fmt.Sprintf("REQ-%s-%s", time.Now().UTC().Format("2006"), strings.ToUpper(uuid.NewString()[:6]))
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_job_requisitions
			(requisition_no, title, department_id, employment_type, headcount,
			 hiring_manager_id, justification, target_start_date)
		VALUES ($1, $2,
			(SELECT id FROM erp_departments WHERE code = UPPER(NULLIF($3,''))),
			$4, $5,
			(SELECT id FROM erp_employees WHERE employee_no = NULLIF($6,'')),
			$7, $8)
		RETURNING id`,
		reqNo, in.Title, in.DepartmentCode, employmentType, in.Headcount,
		in.HiringManagerNo, in.Justification, target).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetRequisition(ctx, id)
}

func (s *Store) GetRequisition(ctx context.Context, id uuid.UUID) (*JobRequisition, error) {
	return scanRequisition(s.pool.QueryRow(ctx,
		`SELECT `+requisitionColumns+` `+requisitionFrom+` WHERE r.id = $1`, id))
}

func (s *Store) ListRequisitions(ctx context.Context, status, departmentCode string, limit, offset int) ([]JobRequisition, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + requisitionColumns + ` ` + requisitionFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if status != "" {
		q += ` AND r.status = $` + itoa(n)
		args = append(args, status)
		n++
	}
	if departmentCode != "" {
		q += ` AND d.code = $` + itoa(n)
		args = append(args, strings.ToUpper(departmentCode))
		n++
	}
	_ = n
	q += ` ORDER BY r.created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// The same scan helper the single-row read uses. Two hand-written scan
	// lists for one column list is how a SELECT gains a column and one of the
	// two readers starts filling the wrong fields — a defect no compiler sees,
	// because Scan is variadic.
	out := []JobRequisition{}
	for rows.Next() {
		r, err := scanRequisition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// SetRequisitionStatus moves a requisition, enforcing the transition table.
//
// Approving is recorded against the approver: a requisition is a commitment to
// spend on a salary, and the four-eyes question ("who signed off on this
// headcount") has to have an answer.
func (s *Store) SetRequisitionStatus(ctx context.Context, id uuid.UUID, to, actorEmployeeNo string) (*JobRequisition, error) {
	existing, err := s.GetRequisition(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := RequisitionStates.Transition(existing.Status, to); err != nil {
		return nil, err
	}
	if existing.Status == to {
		return existing, nil
	}

	_, err = s.pool.Exec(ctx, `
		UPDATE erp_job_requisitions SET
		  status = $2,
		  approved_by_employee_id = CASE WHEN $2 = 'open' AND approved_by_employee_id IS NULL
		      THEN (SELECT id FROM erp_employees WHERE employee_no = NULLIF($3,''))
		      ELSE approved_by_employee_id END,
		  approved_at = CASE WHEN $2 = 'open' AND approved_at IS NULL THEN NOW() ELSE approved_at END,
		  opened_on   = CASE WHEN $2 = 'open' AND opened_on IS NULL THEN CURRENT_DATE ELSE opened_on END,
		  closed_on   = CASE WHEN $2 IN ('filled','cancelled') THEN CURRENT_DATE ELSE closed_on END,
		  updated_at  = NOW()
		WHERE id = $1`, id, to, actorEmployeeNo)
	if err != nil {
		return nil, err
	}
	return s.GetRequisition(ctx, id)
}

// UpsertCandidate finds a candidate by email or creates one.
//
// Matching on email is what stops the same person becoming a new record on
// every application, which is the difference between a pipeline and a list.
func (s *Store) UpsertCandidate(ctx context.Context, fullName, email, phone, source, cvRef, notes string) (*Candidate, error) {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return nil, ErrBadInput
	}
	email = strings.TrimSpace(strings.ToLower(email))

	if email != "" {
		var c Candidate
		err := s.pool.QueryRow(ctx, `
			SELECT id, full_name, email, phone, source, cv_ref, notes, created_at
			FROM erp_candidates WHERE lower(email) = $1`, email).
			Scan(&c.ID, &c.FullName, &c.Email, &c.Phone, &c.Source, &c.CVRef, &c.Notes, &c.CreatedAt)
		if err == nil {
			return &c, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}

	var c Candidate
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_candidates (full_name, email, phone, source, cv_ref, notes)
		VALUES ($1, NULLIF($2,''), NULLIF($3,''), $4, $5, $6)
		RETURNING id, full_name, email, phone, source, cv_ref, notes, created_at`,
		fullName, email, phone, source, cvRef, notes).
		Scan(&c.ID, &c.FullName, &c.Email, &c.Phone, &c.Source, &c.CVRef, &c.Notes, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

const applicationColumns = `ap.id, ap.requisition_id, r.requisition_no, r.title,
	ap.candidate_id, c.full_name, ap.stage, ap.applied_on, ap.stage_since,
	ap.rejection_reason, e.employee_no, ap.created_at, ap.updated_at`

const applicationFrom = `FROM erp_applications ap
	JOIN erp_job_requisitions r ON r.id = ap.requisition_id
	JOIN erp_candidates c ON c.id = ap.candidate_id
	LEFT JOIN erp_employees e ON e.id = ap.hired_employee_id`

func scanApplication(row pgx.Row) (*Application, error) {
	var a Application
	err := row.Scan(&a.ID, &a.RequisitionID, &a.RequisitionNo, &a.RequisitionTitle,
		&a.CandidateID, &a.CandidateName, &a.Stage, &a.AppliedOn, &a.StageSince,
		&a.RejectionReason, &a.HiredEmployeeNo, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// CreateApplication puts a candidate into a requisition's pipeline.
//
// Only an open requisition accepts applications: taking applications for a role
// that was never approved, or was already filled, is how a pipeline fills with
// people nobody can hire.
func (s *Store) CreateApplication(ctx context.Context, requisitionID, candidateID uuid.UUID) (*Application, error) {
	var status string
	if err := s.pool.QueryRow(ctx,
		`SELECT status FROM erp_job_requisitions WHERE id = $1`, requisitionID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != "open" {
		return nil, ErrInvalidTransition{
			Machine: "application", From: "requisition:" + status, To: "applied",
			Allowed: []string{"a requisition must be open to receive applications"},
		}
	}

	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_applications (requisition_id, candidate_id)
		VALUES ($1, $2) RETURNING id`, requisitionID, candidateID).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "erp_applications_requisition_id_candidate_id_key") {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.recordApplicationEvent(ctx, id, "", "applied", "", ""); err != nil {
		return nil, err
	}
	return s.GetApplication(ctx, id)
}

func (s *Store) GetApplication(ctx context.Context, id uuid.UUID) (*Application, error) {
	return scanApplication(s.pool.QueryRow(ctx,
		`SELECT `+applicationColumns+` `+applicationFrom+` WHERE ap.id = $1`, id))
}

func (s *Store) ListApplications(ctx context.Context, requisitionID *uuid.UUID, stage string, limit, offset int) ([]Application, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + applicationColumns + ` ` + applicationFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if requisitionID != nil {
		q += ` AND ap.requisition_id = $` + itoa(n)
		args = append(args, *requisitionID)
		n++
	}
	if stage != "" {
		q += ` AND ap.stage = $` + itoa(n)
		args = append(args, stage)
		n++
	}
	_ = n
	q += ` ORDER BY ap.created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Application{}
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// AdvanceApplication moves a candidate through the pipeline.
//
// Reaching 'hired' is not done here: it needs an employee record to point at,
// which is HireApplicant's job.
func (s *Store) AdvanceApplication(ctx context.Context, id uuid.UUID, to, reason, actorEmployeeNo string) (*Application, error) {
	existing, err := s.GetApplication(ctx, id)
	if err != nil {
		return nil, err
	}
	if to == "hired" {
		return nil, ErrBadInput
	}
	if err := ApplicationStages.Transition(existing.Stage, to); err != nil {
		return nil, err
	}
	if existing.Stage == to {
		return existing, nil
	}
	// A rejection without a reason is a record that cannot answer the only
	// question anyone later asks of it.
	if to == "rejected" && strings.TrimSpace(reason) == "" {
		return nil, ErrBadInput
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_applications
		SET stage = $2, stage_since = NOW(), rejection_reason = $3, updated_at = NOW()
		WHERE id = $1`, id, to, reason); err != nil {
		return nil, err
	}
	if err := s.recordApplicationEvent(ctx, id, existing.Stage, to, reason, actorEmployeeNo); err != nil {
		return nil, err
	}
	return s.GetApplication(ctx, id)
}

// HireApplicant closes the loop between recruitment and the roster: it creates
// the employee and links the application to them, in one transaction.
//
// This is the handoff the JSONB version could not make. A "hired" document in a
// blob had no relationship to anybody on the payroll, so every hire was re-keyed
// into the employee list by hand.
func (s *Store) HireApplicant(ctx context.Context, applicationID uuid.UUID, in CreateEmployeeInput, actorEmployeeNo string) (*Application, *Employee, error) {
	existing, err := s.GetApplication(ctx, applicationID)
	if err != nil {
		return nil, nil, err
	}
	if err := ApplicationStages.Transition(existing.Stage, "hired"); err != nil {
		return nil, nil, err
	}

	// The employee is created through the ordinary path so it gets the same
	// validation, defaults and creation event as any other hire.
	emp, err := s.CreateEmployee(ctx, in)
	if err != nil {
		return nil, nil, err
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_applications
		SET stage = 'hired', stage_since = NOW(), hired_employee_id = $2, updated_at = NOW()
		WHERE id = $1`, applicationID, emp.ID); err != nil {
		return nil, nil, err
	}
	if err := s.recordApplicationEvent(ctx, applicationID, existing.Stage, "hired",
		"hired as "+emp.EmployeeNo, actorEmployeeNo); err != nil {
		return nil, nil, err
	}

	// A requisition whose headcount is met is filled. Left open, it keeps
	// accepting applications for a job that no longer exists.
	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_job_requisitions r SET status = 'filled', closed_on = CURRENT_DATE, updated_at = NOW()
		WHERE r.id = $1 AND r.status = 'open'
		  AND (SELECT COUNT(*) FROM erp_applications ap
		       WHERE ap.requisition_id = r.id AND ap.stage = 'hired') >= r.headcount`,
		existing.RequisitionID); err != nil {
		return nil, nil, err
	}

	app, err := s.GetApplication(ctx, applicationID)
	if err != nil {
		return nil, nil, err
	}
	return app, emp, nil
}

func (s *Store) recordApplicationEvent(ctx context.Context, applicationID uuid.UUID, from, to, note, actor string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO erp_application_events (application_id, from_stage, to_stage, note, actor_employee_no)
		VALUES ($1,$2,$3,$4,$5)`, applicationID, from, to, note, actor)
	return err
}

func (s *Store) ListApplicationEvents(ctx context.Context, applicationID uuid.UUID) ([]ApplicationEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT from_stage, to_stage, note, actor_employee_no, created_at
		FROM erp_application_events WHERE application_id = $1 ORDER BY created_at`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApplicationEvent{}
	for rows.Next() {
		var e ApplicationEvent
		if err := rows.Scan(&e.FromStage, &e.ToStage, &e.Note, &e.Actor, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
