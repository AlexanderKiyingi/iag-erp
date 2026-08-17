package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Onboarding and offboarding.
//
// One checklist model serves both: the difference between joining and leaving
// is which template is issued, not a different mechanism. Items are copied from
// the template when the checklist is issued, so editing a template never
// rewrites a checklist somebody is part-way through — or one already completed
// and relied on as the record that assets came back.

type ChecklistTemplate struct {
	ID     uuid.UUID               `json:"id"`
	Code   string                  `json:"code"`
	Name   string                  `json:"name"`
	Kind   string                  `json:"kind"`
	Active bool                    `json:"active"`
	Items  []ChecklistTemplateItem `json:"items,omitempty"`
}

type ChecklistTemplateItem struct {
	Seq           int    `json:"seq"`
	Title         string `json:"title"`
	OwnerRole     string `json:"owner_role"`
	DueOffsetDays int    `json:"due_offset_days"`
	Required      bool   `json:"required"`
}

type EmployeeChecklist struct {
	ID            uuid.UUID               `json:"id"`
	EmployeeNo    string                  `json:"employee_no"`
	EmployeeName  string                  `json:"employee_name"`
	TemplateCode  *string                 `json:"template_code,omitempty"`
	Kind          string                  `json:"kind"`
	Status        string                  `json:"status"`
	ReferenceDate time.Time               `json:"reference_date"`
	CompletedOn   *time.Time              `json:"completed_on,omitempty"`
	Notes         string                  `json:"notes"`
	CreatedAt     time.Time               `json:"created_at"`
	Items         []EmployeeChecklistItem `json:"items,omitempty"`
	// Outstanding is the count of required items still to do — the one number
	// a leaver's final-pay decision actually turns on.
	Outstanding int `json:"outstanding_required"`
}

type EmployeeChecklistItem struct {
	ID          uuid.UUID  `json:"id"`
	Seq         int        `json:"seq"`
	Title       string     `json:"title"`
	OwnerRole   string     `json:"owner_role"`
	AssigneeNo  *string    `json:"assignee_employee_no,omitempty"`
	Required    bool       `json:"required"`
	Status      string     `json:"status"`
	DueOn       *time.Time `json:"due_on,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CompletedBy *string    `json:"completed_by_employee_no,omitempty"`
	Notes       string     `json:"notes"`
}

func (s *Store) ListChecklistTemplates(ctx context.Context, kind string) ([]ChecklistTemplate, error) {
	q := `SELECT id, code, name, kind, active FROM erp_checklist_templates WHERE active = true`
	args := []any{}
	if kind != "" {
		q += ` AND kind = $1`
		args = append(args, kind)
	}
	q += ` ORDER BY kind, code`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChecklistTemplate{}
	for rows.Next() {
		var t ChecklistTemplate
		if err := rows.Scan(&t.ID, &t.Code, &t.Name, &t.Kind, &t.Active); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, err := s.templateItems(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Items = items
	}
	return out, nil
}

func (s *Store) templateItems(ctx context.Context, templateID uuid.UUID) ([]ChecklistTemplateItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq, title, owner_role, due_offset_days, required
		FROM erp_checklist_template_items WHERE template_id = $1 ORDER BY seq`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChecklistTemplateItem{}
	for rows.Next() {
		var i ChecklistTemplateItem
		if err := rows.Scan(&i.Seq, &i.Title, &i.OwnerRole, &i.DueOffsetDays, &i.Required); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// IssueChecklist gives an employee a checklist built from a template.
//
// referenceDate anchors the due dates: the start date for onboarding, the last
// working day for offboarding. Item offsets are relative to it, which is why an
// offboarding item can be due before the checklist is even issued.
func (s *Store) IssueChecklist(ctx context.Context, employeeNo, templateCode, referenceDate, notes string) (*EmployeeChecklist, error) {
	refDate, err := time.Parse("2006-01-02", referenceDate)
	if err != nil {
		return nil, ErrBadInput
	}

	var employeeID, templateID uuid.UUID
	var kind string
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM erp_employees WHERE employee_no = $1`, employeeNo).Scan(&employeeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT id, kind FROM erp_checklist_templates WHERE code = $1 AND active = true`,
		strings.ToUpper(templateCode)).Scan(&templateID, &kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var checklistID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO erp_employee_checklists (employee_id, template_id, kind, reference_date, notes)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		employeeID, templateID, kind, refDate, notes).Scan(&checklistID)
	if err != nil {
		// The partial unique index is what turns a second issue into a clear
		// conflict instead of two competing checklists.
		if strings.Contains(err.Error(), "erp_employee_checklists_open_idx") {
			return nil, ErrConflict
		}
		return nil, err
	}

	// Items are copied, not referenced.
	if _, err := tx.Exec(ctx, `
		INSERT INTO erp_employee_checklist_items
			(checklist_id, seq, title, owner_role, required, due_on)
		SELECT $1, i.seq, i.title, i.owner_role, i.required,
		       ($2::date + (i.due_offset_days || ' days')::interval)::date
		FROM erp_checklist_template_items i
		WHERE i.template_id = $3
		ORDER BY i.seq`, checklistID, refDate, templateID); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetChecklist(ctx, checklistID)
}

const checklistColumns = `cl.id, e.employee_no, e.first_name || ' ' || e.last_name, t.code,
	cl.kind, cl.status, cl.reference_date, cl.completed_on, cl.notes, cl.created_at,
	(SELECT COUNT(*)::int FROM erp_employee_checklist_items i
	 WHERE i.checklist_id = cl.id AND i.required AND i.status NOT IN ('done','not_applicable'))`

const checklistFrom = `FROM erp_employee_checklists cl
	JOIN erp_employees e ON e.id = cl.employee_id
	LEFT JOIN erp_checklist_templates t ON t.id = cl.template_id`

func (s *Store) GetChecklist(ctx context.Context, id uuid.UUID) (*EmployeeChecklist, error) {
	var c EmployeeChecklist
	err := s.pool.QueryRow(ctx, `SELECT `+checklistColumns+` `+checklistFrom+` WHERE cl.id = $1`, id).
		Scan(&c.ID, &c.EmployeeNo, &c.EmployeeName, &c.TemplateCode, &c.Kind, &c.Status,
			&c.ReferenceDate, &c.CompletedOn, &c.Notes, &c.CreatedAt, &c.Outstanding)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	items, err := s.checklistItems(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Items = items
	return &c, nil
}

func (s *Store) ListChecklists(ctx context.Context, employeeNo, kind, status string, restrictTo []string, limit, offset int) ([]EmployeeChecklist, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + checklistColumns + ` ` + checklistFrom + ` WHERE 1=1`
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
	if kind != "" {
		q += ` AND cl.kind = $` + itoa(n)
		args = append(args, kind)
		n++
	}
	if status != "" {
		q += ` AND cl.status = $` + itoa(n)
		args = append(args, status)
		n++
	}
	_ = n
	q += ` ORDER BY cl.created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmployeeChecklist{}
	for rows.Next() {
		var c EmployeeChecklist
		if err := rows.Scan(&c.ID, &c.EmployeeNo, &c.EmployeeName, &c.TemplateCode, &c.Kind,
			&c.Status, &c.ReferenceDate, &c.CompletedOn, &c.Notes, &c.CreatedAt, &c.Outstanding); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) checklistItems(ctx context.Context, checklistID uuid.UUID) ([]EmployeeChecklistItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.seq, i.title, i.owner_role, a.employee_no, i.required, i.status,
		       i.due_on, i.completed_at, cb.employee_no, i.notes
		FROM erp_employee_checklist_items i
		LEFT JOIN erp_employees a  ON a.id  = i.assignee_employee_id
		LEFT JOIN erp_employees cb ON cb.id = i.completed_by_employee_id
		WHERE i.checklist_id = $1 ORDER BY i.seq`, checklistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmployeeChecklistItem{}
	for rows.Next() {
		var i EmployeeChecklistItem
		if err := rows.Scan(&i.ID, &i.Seq, &i.Title, &i.OwnerRole, &i.AssigneeNo, &i.Required,
			&i.Status, &i.DueOn, &i.CompletedAt, &i.CompletedBy, &i.Notes); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// SetChecklistItemStatus ticks, unticks or excuses one item.
func (s *Store) SetChecklistItemStatus(ctx context.Context, itemID uuid.UUID, to, notes, actorEmployeeNo string) (*EmployeeChecklist, error) {
	var checklistID uuid.UUID
	var current, checklistStatus string
	err := s.pool.QueryRow(ctx, `
		SELECT i.checklist_id, i.status, cl.status
		FROM erp_employee_checklist_items i
		JOIN erp_employee_checklists cl ON cl.id = i.checklist_id
		WHERE i.id = $1`, itemID).Scan(&checklistID, &current, &checklistStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// A closed checklist is a record, not a worklist.
	if checklistStatus != "open" {
		return nil, ErrConflict
	}
	if err := ChecklistItemStates.Transition(current, to); err != nil {
		return nil, err
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_employee_checklist_items SET
		  status = $2,
		  notes = CASE WHEN $3 = '' THEN notes ELSE $3 END,
		  completed_at = CASE WHEN $2 IN ('done','not_applicable') THEN NOW() ELSE NULL END,
		  completed_by_employee_id = CASE WHEN $2 IN ('done','not_applicable')
		      THEN (SELECT id FROM erp_employees WHERE employee_no = NULLIF($4,'')) ELSE NULL END
		WHERE id = $1`, itemID, to, notes, actorEmployeeNo); err != nil {
		return nil, err
	}
	return s.GetChecklist(ctx, checklistID)
}

// CompleteChecklist closes a checklist, refusing while required items are open.
//
// The refusal is the point of the feature. Closing an offboarding with assets
// unreturned and access still live is exactly what a checklist exists to stop.
func (s *Store) CompleteChecklist(ctx context.Context, id uuid.UUID) (*EmployeeChecklist, error) {
	existing, err := s.GetChecklist(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ChecklistStates.Transition(existing.Status, "completed"); err != nil {
		return nil, err
	}
	if existing.Outstanding > 0 {
		return nil, ErrChecklistIncomplete
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_employee_checklists SET status = 'completed', completed_on = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'open'`, id); err != nil {
		return nil, err
	}
	return s.GetChecklist(ctx, id)
}

func (s *Store) CancelChecklist(ctx context.Context, id uuid.UUID) (*EmployeeChecklist, error) {
	existing, err := s.GetChecklist(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ChecklistStates.Transition(existing.Status, "cancelled"); err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_employee_checklists SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND status = 'open'`, id); err != nil {
		return nil, err
	}
	return s.GetChecklist(ctx, id)
}

// ErrChecklistIncomplete means required items are still outstanding.
var ErrChecklistIncomplete = errors.New("checklist has required items outstanding")
