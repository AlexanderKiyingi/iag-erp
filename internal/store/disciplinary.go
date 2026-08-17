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

// Disciplinary cases.
//
// The most legally consequential record this service holds, and previously a
// JSONB blob whose "status" was whatever an operator typed. Due process is a
// sequence — reported, investigated, heard, decided, and only then closed — and
// a record that cannot enforce the sequence cannot evidence that it happened.
//
// Every state change is written to erp_disciplinary_events with who made it and
// when, because "was due process followed" is answered by the log, not by the
// current row.

type DisciplinaryCase struct {
	ID             uuid.UUID   `json:"id"`
	CaseNo         string      `json:"case_no"`
	EmployeeNo     string      `json:"employee_no"`
	EmployeeName   string      `json:"employee_name"`
	Category       string      `json:"category"`
	Severity       string      `json:"severity"`
	Status         string      `json:"status"`
	Description    string      `json:"description"`
	ReportedBy     *string     `json:"reported_by_employee_no,omitempty"`
	ReportedOn     time.Time   `json:"reported_on"`
	InvestigatorNo *string     `json:"investigator_employee_no,omitempty"`
	HearingOn      *time.Time  `json:"hearing_on,omitempty"`
	Outcome        string      `json:"outcome"`
	SanctionNotes  string      `json:"sanction_notes"`
	DecidedOn      *time.Time  `json:"decided_on,omitempty"`
	DecidedBy      *string     `json:"decided_by_employee_no,omitempty"`
	AppealDeadline *time.Time  `json:"appeal_deadline,omitempty"`
	ClosedOn       *time.Time  `json:"closed_on,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Events         []CaseEvent `json:"events,omitempty"`
}

type CaseEvent struct {
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Note       string    `json:"note"`
	Actor      string    `json:"actor_employee_no"`
	OccurredAt time.Time `json:"occurred_at"`
}

const caseColumns = `dc.id, dc.case_no, e.employee_no, e.first_name || ' ' || e.last_name,
	dc.category, dc.severity, dc.status, dc.description, rb.employee_no, dc.reported_on,
	inv.employee_no, dc.hearing_on, dc.outcome, dc.sanction_notes, dc.decided_on,
	db.employee_no, dc.appeal_deadline, dc.closed_on, dc.created_at, dc.updated_at`

const caseFrom = `FROM erp_disciplinary_cases dc
	JOIN erp_employees e ON e.id = dc.employee_id
	LEFT JOIN erp_employees rb  ON rb.id  = dc.reported_by_employee_id
	LEFT JOIN erp_employees inv ON inv.id = dc.investigator_employee_id
	LEFT JOIN erp_employees db  ON db.id  = dc.decided_by_employee_id`

func scanCase(row pgx.Row) (*DisciplinaryCase, error) {
	var c DisciplinaryCase
	err := row.Scan(&c.ID, &c.CaseNo, &c.EmployeeNo, &c.EmployeeName, &c.Category, &c.Severity,
		&c.Status, &c.Description, &c.ReportedBy, &c.ReportedOn, &c.InvestigatorNo,
		&c.HearingOn, &c.Outcome, &c.SanctionNotes, &c.DecidedOn, &c.DecidedBy,
		&c.AppealDeadline, &c.ClosedOn, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

type OpenCaseInput struct {
	EmployeeNo  string `json:"employee_no"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

// OpenCase records an allegation. It starts at 'reported' and nowhere else:
// a case cannot be created already decided.
func (s *Store) OpenCase(ctx context.Context, in OpenCaseInput, reportedBy string) (*DisciplinaryCase, error) {
	if strings.TrimSpace(in.EmployeeNo) == "" || strings.TrimSpace(in.Description) == "" {
		return nil, ErrBadInput
	}
	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = "conduct"
	}
	severity := strings.TrimSpace(in.Severity)
	if severity == "" {
		severity = "minor"
	}

	caseNo := fmt.Sprintf("DC-%s-%s", time.Now().UTC().Format("2006"), strings.ToUpper(uuid.NewString()[:6]))
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_disciplinary_cases
			(case_no, employee_id, category, severity, description, reported_by_employee_id)
		VALUES ($1,
			(SELECT id FROM erp_employees WHERE employee_no = $2),
			$3, $4, $5,
			(SELECT id FROM erp_employees WHERE employee_no = NULLIF($6,'')))
		RETURNING id`, caseNo, in.EmployeeNo, category, severity, in.Description, reportedBy).Scan(&id)
	if err != nil {
		// A null employee_id means the employee number did not resolve.
		if strings.Contains(err.Error(), "employee_id") {
			return nil, ErrBadInput
		}
		return nil, err
	}
	if err := s.recordCaseEvent(ctx, id, "", "reported", in.Description, reportedBy); err != nil {
		return nil, err
	}
	return s.GetCase(ctx, id)
}

func (s *Store) GetCase(ctx context.Context, id uuid.UUID) (*DisciplinaryCase, error) {
	c, err := scanCase(s.pool.QueryRow(ctx, `SELECT `+caseColumns+` `+caseFrom+` WHERE dc.id = $1`, id))
	if err != nil {
		return nil, err
	}
	events, err := s.ListCaseEvents(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Events = events
	return c, nil
}

func (s *Store) ListCases(ctx context.Context, employeeNo, status string, restrictTo []string, limit, offset int) ([]DisciplinaryCase, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + caseColumns + ` ` + caseFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if len(restrictTo) > 0 {
		q += ` AND e.employee_no = ANY($` + itoa(n) + `)`
		args = append(args, restrictTo)
		n++
	}
	if employeeNo != "" {
		q += ` AND e.employee_no = $` + itoa(n)
		args = append(args, employeeNo)
		n++
	}
	if status != "" {
		q += ` AND dc.status = $` + itoa(n)
		args = append(args, status)
		n++
	}
	_ = n
	q += ` ORDER BY dc.reported_on DESC, dc.created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Reuses scanCase rather than repeating its twenty destinations: see the
	// note in recruitment.go on why one column list must have one reader.
	//
	// Events are not loaded here. A list of cases does not need each one's full
	// history, and loading it would be a query per row.
	out := []DisciplinaryCase{}
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// AdvanceCaseInput carries the extra facts a particular move needs.
type AdvanceCaseInput struct {
	To             string `json:"to"`
	Note           string `json:"note"`
	InvestigatorNo string `json:"investigator_employee_no"`
	HearingOn      string `json:"hearing_on"`
	Outcome        string `json:"outcome"`
	SanctionNotes  string `json:"sanction_notes"`
	AppealDeadline string `json:"appeal_deadline"`
}

// AdvanceCase moves a case, enforcing both the sequence and what each state
// requires to be true before it can be entered.
func (s *Store) AdvanceCase(ctx context.Context, id uuid.UUID, in AdvanceCaseInput, actorEmployeeNo string) (*DisciplinaryCase, error) {
	existing, err := s.GetCase(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := DisciplinaryStates.Transition(existing.Status, in.To); err != nil {
		return nil, err
	}
	if existing.Status == in.To {
		return existing, nil
	}

	var hearingOn, appealDeadline *time.Time
	if in.HearingOn != "" {
		t, err := time.Parse(time.RFC3339, in.HearingOn)
		if err != nil {
			if d, err2 := time.Parse("2006-01-02", in.HearingOn); err2 == nil {
				t = d
			} else {
				return nil, ErrBadInput
			}
		}
		hearingOn = &t
	}
	if in.AppealDeadline != "" {
		t, err := time.Parse("2006-01-02", in.AppealDeadline)
		if err != nil {
			return nil, ErrBadInput
		}
		appealDeadline = &t
	}

	// Per-state requirements. These are the rules that make the sequence mean
	// something: a hearing needs a date, a decision needs an outcome.
	switch in.To {
	case "hearing_scheduled":
		if hearingOn == nil && existing.HearingOn == nil {
			return nil, ErrBadInput
		}
	case "decided":
		outcome := strings.TrimSpace(in.Outcome)
		if outcome == "" {
			outcome = existing.Outcome
		}
		if !IsValidDisciplinaryOutcome(outcome) {
			return nil, ErrBadInput
		}
		in.Outcome = outcome
		// A dismissal reached without a hearing is the procedural failure that
		// costs an unfair-dismissal case. Gross misconduct still gets a hearing.
		if outcome == "dismissal" && existing.HearingOn == nil && hearingOn == nil {
			return nil, ErrHearingRequired
		}
	case "dismissed", "closed":
		if strings.TrimSpace(in.Note) == "" {
			return nil, ErrBadInput
		}
	}

	_, err = s.pool.Exec(ctx, `
		UPDATE erp_disciplinary_cases SET
		  status = $2,
		  investigator_employee_id = COALESCE(
		      (SELECT id FROM erp_employees WHERE employee_no = NULLIF($3,'')),
		      investigator_employee_id),
		  hearing_on      = COALESCE($4, hearing_on),
		  outcome         = CASE WHEN $5 = '' THEN outcome ELSE $5 END,
		  sanction_notes  = CASE WHEN $6 = '' THEN sanction_notes ELSE $6 END,
		  appeal_deadline = COALESCE($7, appeal_deadline),
		  decided_on = CASE WHEN $2 = 'decided' THEN CURRENT_DATE ELSE decided_on END,
		  decided_by_employee_id = CASE WHEN $2 = 'decided'
		      THEN (SELECT id FROM erp_employees WHERE employee_no = NULLIF($8,''))
		      ELSE decided_by_employee_id END,
		  closed_on = CASE WHEN $2 IN ('closed','dismissed') THEN CURRENT_DATE ELSE closed_on END,
		  updated_at = NOW()
		WHERE id = $1`,
		id, in.To, in.InvestigatorNo, hearingOn, in.Outcome, in.SanctionNotes,
		appealDeadline, actorEmployeeNo)
	if err != nil {
		return nil, err
	}
	if err := s.recordCaseEvent(ctx, id, existing.Status, in.To, in.Note, actorEmployeeNo); err != nil {
		return nil, err
	}
	return s.GetCase(ctx, id)
}

func (s *Store) recordCaseEvent(ctx context.Context, caseID uuid.UUID, from, to, note, actor string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO erp_disciplinary_events (case_id, from_status, to_status, note, actor_employee_no)
		VALUES ($1,$2,$3,$4,$5)`, caseID, from, to, note, actor)
	return err
}

func (s *Store) ListCaseEvents(ctx context.Context, caseID uuid.UUID) ([]CaseEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT from_status, to_status, note, actor_employee_no, occurred_at
		FROM erp_disciplinary_events WHERE case_id = $1 ORDER BY occurred_at`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CaseEvent{}
	for rows.Next() {
		var e CaseEvent
		if err := rows.Scan(&e.FromStatus, &e.ToStatus, &e.Note, &e.Actor, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ErrHearingRequired blocks a dismissal on a case that never held a hearing.
var ErrHearingRequired = errors.New("a dismissal requires a hearing to have been held")
