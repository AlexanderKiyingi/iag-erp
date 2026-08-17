package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Worksite struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	PlantCode string    `json:"plant_code"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type SetupItem struct {
	ID            uuid.UUID  `json:"id"`
	ItemType      string     `json:"item_type"`
	Name          string     `json:"name"`
	Code          string     `json:"code"`
	Owner         string     `json:"owner"`
	Status        string     `json:"status"`
	EffectiveDate *time.Time `json:"effective_date,omitempty"`
	Description   string     `json:"description"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (r SetupItem) FrontendShape() map[string]string {
	out := map[string]string{
		"id":          r.ID.String(),
		"name":        r.Name,
		"type":        r.ItemType,
		"code":        r.Code,
		"owner":       r.Owner,
		"status":      r.Status,
		"description": r.Description,
	}
	if r.EffectiveDate != nil {
		out["effectiveDate"] = r.EffectiveDate.Format("2006-01-02")
	}
	return out
}

func (s *Store) ListWorksites(ctx context.Context) ([]Worksite, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code, name, plant_code, active, created_at
		FROM erp_worksites WHERE active = true ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Worksite
	for rows.Next() {
		var w Worksite
		if err := rows.Scan(&w.Code, &w.Name, &w.PlantCode, &w.Active, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

type ListSetupFilter struct {
	ItemType string
	Status   string
	Limit    int
	Offset   int
}

func (s *Store) ListSetupItems(ctx context.Context, f ListSetupFilter) ([]SetupItem, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `SELECT id, item_type, name, code, owner, status, effective_date, description, created_at, updated_at
		FROM erp_setup_items WHERE 1=1`
	args := []any{}
	n := 1
	if f.ItemType != "" {
		q += ` AND item_type = $` + itoa(n)
		args = append(args, strings.ToLower(f.ItemType))
		n++
	}
	if f.Status != "" {
		q += ` AND status = $` + itoa(n)
		args = append(args, f.Status)
		n++
	}
	_ = n
	q += ` ORDER BY updated_at DESC LIMIT ` + itoa(f.Limit) + ` OFFSET ` + itoa(f.Offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SetupItem
	for rows.Next() {
		var item SetupItem
		if err := rows.Scan(&item.ID, &item.ItemType, &item.Name, &item.Code, &item.Owner,
			&item.Status, &item.EffectiveDate, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type UpsertSetupInput struct {
	ItemType      string `json:"item_type"`
	Name          string `json:"name"`
	Code          string `json:"code"`
	Owner         string `json:"owner"`
	Status        string `json:"status"`
	EffectiveDate string `json:"effective_date"`
	Description   string `json:"description"`
}

func (s *Store) CreateSetupItem(ctx context.Context, in UpsertSetupInput) (*SetupItem, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.ItemType) == "" {
		return nil, ErrBadInput
	}
	var eff *time.Time
	if in.EffectiveDate != "" {
		if t, err := time.Parse("2006-01-02", in.EffectiveDate); err == nil {
			eff = &t
		}
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "active"
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_setup_items (item_type, name, code, owner, status, effective_date, description)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		strings.ToLower(in.ItemType), in.Name, in.Code, in.Owner, status, eff, in.Description).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetSetupItem(ctx, id)
}

func (s *Store) GetSetupItem(ctx context.Context, id uuid.UUID) (*SetupItem, error) {
	var item SetupItem
	err := s.pool.QueryRow(ctx, `
		SELECT id, item_type, name, code, owner, status, effective_date, description, created_at, updated_at
		FROM erp_setup_items WHERE id = $1`, id).Scan(
		&item.ID, &item.ItemType, &item.Name, &item.Code, &item.Owner,
		&item.Status, &item.EffectiveDate, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *Store) UpdateSetupItem(ctx context.Context, id uuid.UUID, in UpsertSetupInput) (*SetupItem, error) {
	existing, err := s.GetSetupItem(ctx, id)
	if err != nil {
		return nil, err
	}
	name := in.Name
	if name == "" {
		name = existing.Name
	}
	itemType := in.ItemType
	if itemType == "" {
		itemType = existing.ItemType
	}
	code := in.Code
	if code == "" {
		code = existing.Code
	}
	owner := in.Owner
	if owner == "" {
		owner = existing.Owner
	}
	status := in.Status
	if status == "" {
		status = existing.Status
	}
	desc := in.Description
	if desc == "" {
		desc = existing.Description
	}
	var eff *time.Time = existing.EffectiveDate
	if in.EffectiveDate != "" {
		if t, err := time.Parse("2006-01-02", in.EffectiveDate); err == nil {
			eff = &t
		}
	}
	err = s.pool.QueryRow(ctx, `
		UPDATE erp_setup_items SET item_type=$2, name=$3, code=$4, owner=$5, status=$6,
		  effective_date=$7, description=$8, updated_at=NOW()
		WHERE id=$1 RETURNING id`, id, itemType, name, code, owner, status, eff, desc).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetSetupItem(ctx, id)
}

func (s *Store) DeleteSetupItem(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM erp_setup_items WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
