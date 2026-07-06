package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// HR module keys served by the generic records table (HRMIAG frontend modules).
var HRModuleKeys = []string{
	"shifts", "recruitment", "onboarding", "performance", "training",
	"helpdesk", "documents", "disciplinary", "assets", "offboarding",
	"payroll", "settings",
}

func IsHRModule(module string) bool {
	module = strings.ToLower(strings.TrimSpace(module))
	for _, k := range HRModuleKeys {
		if k == module {
			return true
		}
	}
	return false
}

type HRModuleRecord struct {
	ID         uuid.UUID      `json:"id"`
	Module     string         `json:"module"`
	Department string         `json:"department"`
	Status     string         `json:"status"`
	Data       map[string]any `json:"data"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// FrontendShape flattens id + data for HRMIAG HRRecord compatibility.
func (r HRModuleRecord) FrontendShape() map[string]string {
	out := map[string]string{"id": r.ID.String()}
	for k, v := range r.Data {
		if k == "id" {
			continue
		}
		out[k] = stringifyHRField(v)
	}
	return out
}

func stringifyHRField(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

type ListHRModuleFilter struct {
	Module     string
	Department string
	Status     string
	Search     string
	Limit      int
	Offset     int
}

func (s *Store) ModuleRecordCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT module, COUNT(*)::int FROM erp_hr_module_records GROUP BY module`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int, len(HRModuleKeys))
	for _, k := range HRModuleKeys {
		out[k] = 0
	}
	for rows.Next() {
		var module string
		var n int
		if err := rows.Scan(&module, &n); err != nil {
			return nil, err
		}
		out[module] = n
	}
	return out, rows.Err()
}

func (s *Store) ListHRModuleRecords(ctx context.Context, f ListHRModuleFilter) ([]HRModuleRecord, error) {
	if !IsHRModule(f.Module) {
		return nil, ErrBadInput
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `
		SELECT id, module, department, status, data, created_at, updated_at
		FROM erp_hr_module_records WHERE module = $1`
	args := []any{f.Module}
	n := 2
	if f.Department != "" {
		q += ` AND department = $` + itoa(n)
		args = append(args, f.Department)
		n++
	}
	if f.Status != "" {
		q += ` AND status = $` + itoa(n)
		args = append(args, f.Status)
		n++
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		q += ` AND (department ILIKE $` + itoa(n) + ` OR status ILIKE $` + itoa(n) +
			` OR data::text ILIKE $` + itoa(n) + `)`
		args = append(args, "%"+search+"%")
		n++
	}
	_ = n
	q += ` ORDER BY updated_at DESC LIMIT ` + itoa(f.Limit) + ` OFFSET ` + itoa(f.Offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHRModuleRows(rows)
}

func (s *Store) GetHRModuleRecord(ctx context.Context, module string, id uuid.UUID) (*HRModuleRecord, error) {
	if !IsHRModule(module) {
		return nil, ErrBadInput
	}
	row := s.pool.QueryRow(ctx, `
		SELECT id, module, department, status, data, created_at, updated_at
		FROM erp_hr_module_records WHERE module = $1 AND id = $2`, module, id)
	return scanHRModuleRow(row)
}

type UpsertHRModuleInput struct {
	Department string         `json:"department"`
	Status     string         `json:"status"`
	Data       map[string]any `json:"data"`
}

func hrModuleMetaFromData(data map[string]any) (department, status string) {
	if data == nil {
		return "", ""
	}
	if v, ok := data["department"].(string); ok {
		department = v
	}
	if v, ok := data["status"].(string); ok {
		status = v
	}
	return department, status
}

func (s *Store) CreateHRModuleRecord(ctx context.Context, module string, in UpsertHRModuleInput) (*HRModuleRecord, error) {
	if !IsHRModule(module) {
		return nil, ErrBadInput
	}
	dept := strings.TrimSpace(in.Department)
	status := strings.TrimSpace(in.Status)
	if dept == "" || status == "" {
		d, st := hrModuleMetaFromData(in.Data)
		if dept == "" {
			dept = d
		}
		if status == "" {
			status = st
		}
	}
	data := in.Data
	if data == nil {
		data = map[string]any{}
	}
	delete(data, "id")
	raw, _ := json.Marshal(data)
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_hr_module_records (module, department, status, data)
		VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id`, module, dept, status, raw).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetHRModuleRecord(ctx, module, id)
}

func (s *Store) UpdateHRModuleRecord(ctx context.Context, module string, id uuid.UUID, in UpsertHRModuleInput) (*HRModuleRecord, error) {
	if !IsHRModule(module) {
		return nil, ErrBadInput
	}
	existing, err := s.GetHRModuleRecord(ctx, module, id)
	if err != nil {
		return nil, err
	}
	dept := strings.TrimSpace(in.Department)
	status := strings.TrimSpace(in.Status)
	data := existing.Data
	if in.Data != nil {
		data = in.Data
		delete(data, "id")
	}
	if dept == "" {
		dept = existing.Department
	}
	if status == "" {
		status = existing.Status
	}
	if dept == "" || status == "" {
		d, st := hrModuleMetaFromData(data)
		if dept == "" {
			dept = d
		}
		if status == "" {
			status = st
		}
	}
	raw, _ := json.Marshal(data)
	err = s.pool.QueryRow(ctx, `
		UPDATE erp_hr_module_records SET
		  department = $3, status = $4, data = $5::jsonb, updated_at = NOW()
		WHERE module = $1 AND id = $2
		RETURNING id`, module, id, dept, status, raw).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.GetHRModuleRecord(ctx, module, id)
}

func (s *Store) DeleteHRModuleRecord(ctx context.Context, module string, id uuid.UUID) error {
	if !IsHRModule(module) {
		return ErrBadInput
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM erp_hr_module_records WHERE module = $1 AND id = $2`, module, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ImportHRModuleRecords(ctx context.Context, module string, items []map[string]any) (int, error) {
	if !IsHRModule(module) {
		return 0, ErrBadInput
	}
	if len(items) == 0 {
		return 0, ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	n := 0
	for _, item := range items {
		data := item
		if data == nil {
			continue
		}
		delete(data, "id")
		dept, status := hrModuleMetaFromData(data)
		raw, _ := json.Marshal(data)
		if _, err := tx.Exec(ctx, `
			INSERT INTO erp_hr_module_records (module, department, status, data)
			VALUES ($1, $2, $3, $4::jsonb)`, module, dept, status, raw); err != nil {
			return 0, err
		}
		n++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}

func scanHRModuleRow(row pgx.Row) (*HRModuleRecord, error) {
	var r HRModuleRecord
	var raw []byte
	if err := row.Scan(&r.ID, &r.Module, &r.Department, &r.Status, &raw, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.Data = scanAttrs(raw)
	return &r, nil
}

func scanHRModuleRows(rows pgx.Rows) ([]HRModuleRecord, error) {
	var out []HRModuleRecord
	for rows.Next() {
		var r HRModuleRecord
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Module, &r.Department, &r.Status, &raw, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Data = scanAttrs(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}
