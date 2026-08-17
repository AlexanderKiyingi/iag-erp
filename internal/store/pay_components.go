package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Pay components attached to people.
//
// The catalogue (erp_pay_components) says what a housing allowance is and
// whether it is taxable. This says who gets one, how much, and between which
// dates — the part that changes every month.

// PayComponentDefinition is one entry in the catalogue.
type PayComponentDefinition struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Taxable     bool   `json:"taxable"`
	Pensionable bool   `json:"pensionable"`
	Active      bool   `json:"active"`
}

// EmployeePayComponent is a component assigned to one employee.
type EmployeePayComponent struct {
	ID            uuid.UUID  `json:"id"`
	EmployeeNo    string     `json:"employee_no"`
	ComponentCode string     `json:"component_code"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Amount        float64    `json:"amount"`
	Taxable       bool       `json:"taxable"`
	Pensionable   bool       `json:"pensionable"`
	EffectiveFrom time.Time  `json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (s *Store) ListPayComponentDefinitions(ctx context.Context) ([]PayComponentDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code, name, kind, taxable, pensionable, active
		FROM erp_pay_components WHERE active = true ORDER BY kind, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PayComponentDefinition{}
	for rows.Next() {
		var d PayComponentDefinition
		if err := rows.Scan(&d.Code, &d.Name, &d.Kind, &d.Taxable, &d.Pensionable, &d.Active); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ListEmployeePayComponents(ctx context.Context, employeeNo string) ([]EmployeePayComponent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT epc.id, e.employee_no, c.code, c.name, c.kind, epc.amount,
		       c.taxable, c.pensionable, epc.effective_from, epc.effective_to, epc.created_at
		FROM erp_employee_pay_components epc
		JOIN erp_employees e ON e.id = epc.employee_id
		JOIN erp_pay_components c ON c.code = epc.component_code
		WHERE e.employee_no = $1
		ORDER BY epc.effective_from DESC, c.code`, employeeNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmployeePayComponent{}
	for rows.Next() {
		var p EmployeePayComponent
		if err := rows.Scan(&p.ID, &p.EmployeeNo, &p.ComponentCode, &p.Name, &p.Kind,
			&p.Amount, &p.Taxable, &p.Pensionable, &p.EffectiveFrom, &p.EffectiveTo,
			&p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AssignPayComponentInput adds a component to an employee.
type AssignPayComponentInput struct {
	ComponentCode string  `json:"component_code"`
	Amount        float64 `json:"amount"`
	EffectiveFrom string  `json:"effective_from"`
	EffectiveTo   string  `json:"effective_to"`
}

func (s *Store) AssignPayComponent(ctx context.Context, employeeNo string, in AssignPayComponentInput) (*EmployeePayComponent, error) {
	code := strings.ToUpper(strings.TrimSpace(in.ComponentCode))
	if code == "" || in.Amount < 0 {
		return nil, ErrBadInput
	}

	effectiveFrom := time.Now().UTC().Truncate(24 * time.Hour)
	if in.EffectiveFrom != "" {
		parsed, err := time.Parse("2006-01-02", in.EffectiveFrom)
		if err != nil {
			return nil, ErrBadInput
		}
		effectiveFrom = parsed
	}
	var effectiveTo *time.Time
	if in.EffectiveTo != "" {
		parsed, err := time.Parse("2006-01-02", in.EffectiveTo)
		if err != nil {
			return nil, ErrBadInput
		}
		if parsed.Before(effectiveFrom) {
			return nil, ErrBadInput
		}
		effectiveTo = &parsed
	}

	var employeeID uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM erp_employees WHERE employee_no = $1`, employeeNo).Scan(&employeeID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_employee_pay_components (employee_id, component_code, amount, effective_from, effective_to)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		employeeID, code, in.Amount, effectiveFrom, effectiveTo).Scan(&id); err != nil {
		return nil, err
	}

	items, err := s.ListEmployeePayComponents(ctx, employeeNo)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ID == id {
			return &items[i], nil
		}
	}
	return nil, ErrNotFound
}

// EndPayComponent closes an assignment as of a date rather than deleting it.
//
// Deleting would remove the allowance from payslips already computed under it,
// which is the one thing a payslip must never do.
func (s *Store) EndPayComponent(ctx context.Context, id uuid.UUID, effectiveTo time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE erp_employee_pay_components SET effective_to = $2
		WHERE id = $1 AND (effective_to IS NULL OR effective_to > $2)`, id, effectiveTo)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
