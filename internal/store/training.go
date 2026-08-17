package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Training and certification.
//
// The JSONB version recorded that somebody attended something. What it could
// not record is whether the certificate is still valid — and for the courses
// that matter here (food safety, first aid, machine operation) an expired
// certificate that nobody is tracking looks exactly like a valid one.

type TrainingCourse struct {
	ID             uuid.UUID `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	Provider       string    `json:"provider"`
	Description    string    `json:"description"`
	DurationHours  float64   `json:"duration_hours"`
	Mandatory      bool      `json:"mandatory"`
	ValidityMonths *int      `json:"validity_months,omitempty"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
}

type TrainingEnrolment struct {
	ID             uuid.UUID  `json:"id"`
	CourseCode     string     `json:"course_code"`
	CourseName     string     `json:"course_name"`
	EmployeeNo     string     `json:"employee_no"`
	EmployeeName   string     `json:"employee_name"`
	Status         string     `json:"status"`
	EnrolledOn     time.Time  `json:"enrolled_on"`
	StartedOn      *time.Time `json:"started_on,omitempty"`
	CompletedOn    *time.Time `json:"completed_on,omitempty"`
	Score          *float64   `json:"score,omitempty"`
	ExpiresOn      *time.Time `json:"expires_on,omitempty"`
	CertificateRef string     `json:"certificate_ref"`
	Notes          string     `json:"notes"`
	// Expired is computed on read rather than stored: a certificate does not
	// expire because a job ran, it expires because the date passed.
	Expired bool `json:"expired"`
}

type UpsertCourseInput struct {
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Provider       string  `json:"provider"`
	Description    string  `json:"description"`
	DurationHours  float64 `json:"duration_hours"`
	Mandatory      bool    `json:"mandatory"`
	ValidityMonths *int    `json:"validity_months"`
}

func (s *Store) CreateCourse(ctx context.Context, in UpsertCourseInput) (*TrainingCourse, error) {
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if code == "" || strings.TrimSpace(in.Name) == "" {
		return nil, ErrBadInput
	}
	if in.ValidityMonths != nil && *in.ValidityMonths <= 0 {
		return nil, ErrBadInput
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_training_courses
			(code, name, provider, description, duration_hours, mandatory, validity_months)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		code, in.Name, in.Provider, in.Description, in.DurationHours,
		in.Mandatory, in.ValidityMonths).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "erp_training_courses_code_key") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return s.GetCourse(ctx, id)
}

func (s *Store) GetCourse(ctx context.Context, id uuid.UUID) (*TrainingCourse, error) {
	var c TrainingCourse
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, name, provider, description, duration_hours, mandatory,
		       validity_months, active, created_at
		FROM erp_training_courses WHERE id = $1`, id).
		Scan(&c.ID, &c.Code, &c.Name, &c.Provider, &c.Description, &c.DurationHours,
			&c.Mandatory, &c.ValidityMonths, &c.Active, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListCourses(ctx context.Context, mandatoryOnly bool) ([]TrainingCourse, error) {
	q := `SELECT id, code, name, provider, description, duration_hours, mandatory,
	             validity_months, active, created_at
	      FROM erp_training_courses WHERE active = true`
	if mandatoryOnly {
		q += ` AND mandatory = true`
	}
	q += ` ORDER BY code`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrainingCourse{}
	for rows.Next() {
		var c TrainingCourse
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.Provider, &c.Description,
			&c.DurationHours, &c.Mandatory, &c.ValidityMonths, &c.Active, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Enrol puts an employee on a course.
func (s *Store) Enrol(ctx context.Context, courseCode, employeeNo string) (*TrainingEnrolment, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_training_enrolments (course_id, employee_id)
		VALUES (
			(SELECT id FROM erp_training_courses WHERE code = UPPER($1) AND active = true),
			(SELECT id FROM erp_employees WHERE employee_no = $2))
		RETURNING id`, courseCode, employeeNo).Scan(&id)
	if err != nil {
		// The partial unique index covers only unfinished enrolments, so
		// re-taking an expired course is allowed and a duplicate live one is not.
		if strings.Contains(err.Error(), "erp_training_enrolments_live_idx") {
			return nil, ErrConflict
		}
		if strings.Contains(err.Error(), "course_id") || strings.Contains(err.Error(), "employee_id") {
			return nil, ErrBadInput
		}
		return nil, err
	}
	return s.GetEnrolment(ctx, id)
}

const enrolmentColumns = `en.id, c.code, c.name, e.employee_no, e.first_name || ' ' || e.last_name,
	en.status, en.enrolled_on, en.started_on, en.completed_on, en.score, en.expires_on,
	en.certificate_ref, en.notes`

const enrolmentFrom = `FROM erp_training_enrolments en
	JOIN erp_training_courses c ON c.id = en.course_id
	JOIN erp_employees e ON e.id = en.employee_id`

func (s *Store) GetEnrolment(ctx context.Context, id uuid.UUID) (*TrainingEnrolment, error) {
	var en TrainingEnrolment
	err := s.pool.QueryRow(ctx, `SELECT `+enrolmentColumns+` `+enrolmentFrom+` WHERE en.id = $1`, id).
		Scan(&en.ID, &en.CourseCode, &en.CourseName, &en.EmployeeNo, &en.EmployeeName,
			&en.Status, &en.EnrolledOn, &en.StartedOn, &en.CompletedOn, &en.Score,
			&en.ExpiresOn, &en.CertificateRef, &en.Notes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	en.Expired = enrolmentExpired(en.Status, en.ExpiresOn, time.Now().UTC())
	return &en, nil
}

// enrolmentExpired reports whether a completed certificate has lapsed.
func enrolmentExpired(status string, expiresOn *time.Time, asOf time.Time) bool {
	if status != "completed" || expiresOn == nil {
		return false
	}
	return expiresOn.Before(asOf.Truncate(24 * time.Hour))
}

type ListEnrolmentsFilter struct {
	EmployeeNo string
	CourseCode string
	Status     string
	// ExpiringWithinDays surfaces certificates about to lapse, including ones
	// that already have. This is the compliance officer's query.
	ExpiringWithinDays int
	RestrictTo         []string
	Limit              int
	Offset             int
}

func (s *Store) ListEnrolments(ctx context.Context, f ListEnrolmentsFilter) ([]TrainingEnrolment, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `SELECT ` + enrolmentColumns + ` ` + enrolmentFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if len(f.RestrictTo) > 0 {
		q += ` AND e.employee_no = ANY($` + itoa(n) + `)`
		args = append(args, f.RestrictTo)
		n++
	}
	if f.EmployeeNo != "" {
		q += ` AND e.employee_no = $` + itoa(n)
		args = append(args, f.EmployeeNo)
		n++
	}
	if f.CourseCode != "" {
		q += ` AND c.code = $` + itoa(n)
		args = append(args, strings.ToUpper(f.CourseCode))
		n++
	}
	if f.Status != "" {
		q += ` AND en.status = $` + itoa(n)
		args = append(args, f.Status)
		n++
	}
	if f.ExpiringWithinDays > 0 {
		q += ` AND en.status = 'completed' AND en.expires_on IS NOT NULL
		       AND en.expires_on <= CURRENT_DATE + ($` + itoa(n) + ` || ' days')::interval`
		args = append(args, f.ExpiringWithinDays)
		n++
	}
	_ = n
	q += ` ORDER BY en.expires_on NULLS LAST, en.enrolled_on DESC LIMIT ` +
		itoa(f.Limit) + ` OFFSET ` + itoa(f.Offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	out := []TrainingEnrolment{}
	for rows.Next() {
		var en TrainingEnrolment
		if err := rows.Scan(&en.ID, &en.CourseCode, &en.CourseName, &en.EmployeeNo,
			&en.EmployeeName, &en.Status, &en.EnrolledOn, &en.StartedOn, &en.CompletedOn,
			&en.Score, &en.ExpiresOn, &en.CertificateRef, &en.Notes); err != nil {
			return nil, err
		}
		en.Expired = enrolmentExpired(en.Status, en.ExpiresOn, now)
		out = append(out, en)
	}
	return out, rows.Err()
}

type UpdateEnrolmentInput struct {
	To             string   `json:"to"`
	Score          *float64 `json:"score"`
	CertificateRef string   `json:"certificate_ref"`
	Notes          string   `json:"notes"`
	CompletedOn    string   `json:"completed_on"`
}

// UpdateEnrolment moves an enrolment and, on completion, computes the expiry
// from the course's validity period.
//
// The expiry is derived here rather than supplied, so a certificate cannot be
// recorded as valid for longer than the course says it is.
func (s *Store) UpdateEnrolment(ctx context.Context, id uuid.UUID, in UpdateEnrolmentInput) (*TrainingEnrolment, error) {
	existing, err := s.GetEnrolment(ctx, id)
	if err != nil {
		return nil, err
	}
	to := strings.TrimSpace(in.To)
	if to == "" {
		to = existing.Status
	}
	if err := EnrolmentStates.Transition(existing.Status, to); err != nil {
		return nil, err
	}

	completedOn := time.Now().UTC().Truncate(24 * time.Hour)
	if in.CompletedOn != "" {
		t, err := time.Parse("2006-01-02", in.CompletedOn)
		if err != nil {
			return nil, ErrBadInput
		}
		completedOn = t
	}

	var expiresOn *time.Time
	if to == "completed" {
		var validityMonths *int
		if err := s.pool.QueryRow(ctx, `
			SELECT c.validity_months FROM erp_training_courses c
			JOIN erp_training_enrolments en ON en.course_id = c.id
			WHERE en.id = $1`, id).Scan(&validityMonths); err != nil {
			return nil, err
		}
		if validityMonths != nil {
			exp := completedOn.AddDate(0, *validityMonths, 0)
			expiresOn = &exp
		}
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_training_enrolments SET
		  status = $2,
		  started_on = CASE WHEN $2 = 'in_progress' AND started_on IS NULL
		      THEN CURRENT_DATE ELSE started_on END,
		  completed_on = CASE WHEN $2 IN ('completed','failed') THEN $3::date ELSE completed_on END,
		  expires_on = CASE WHEN $2 = 'completed' THEN $4::date ELSE expires_on END,
		  score = COALESCE($5, score),
		  certificate_ref = CASE WHEN $6 = '' THEN certificate_ref ELSE $6 END,
		  notes = CASE WHEN $7 = '' THEN notes ELSE $7 END,
		  updated_at = NOW()
		WHERE id = $1`, id, to, completedOn, expiresOn, in.Score,
		in.CertificateRef, in.Notes); err != nil {
		return nil, err
	}
	return s.GetEnrolment(ctx, id)
}
