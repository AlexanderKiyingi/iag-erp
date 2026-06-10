package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"iag-erp/backend/internal/events"
)

type ProductionOrder struct {
	ID        uuid.UUID  `json:"id"`
	PONum     string     `json:"po_num"`
	Customer  string     `json:"customer"`
	Product   string     `json:"product"`
	QtyKg     float64    `json:"qty_kg"`
	OriginLot *string    `json:"origin_lot,omitempty"`
	AssetTag  *string    `json:"asset_tag,omitempty"`
	Status    string     `json:"status"`
	DueAt     *time.Time `json:"due_at,omitempty"`
	ERPRef    *string    `json:"erp_ref,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type ListProductionOrdersFilter struct {
	Status string
	Since  string
	Limit  int
	Offset int
}

func (s *Store) ListProductionOrders(ctx context.Context, f ListProductionOrdersFilter) ([]ProductionOrder, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	q := `
		SELECT id, po_num, customer, product, qty_kg, origin_lot, asset_tag, status, due_at, erp_ref, created_at, updated_at
		FROM erp_production_orders WHERE 1=1`
	args := []any{}
	n := 1
	if f.Status != "" {
		q += ` AND status = $` + itoa(n)
		args = append(args, f.Status)
		n++
	}
	if f.Since != "" {
		if since, err := time.Parse(time.RFC3339, f.Since); err == nil {
			q += ` AND updated_at >= $` + itoa(n)
			args = append(args, since)
			n++
		}
	}
	_ = n
	q += ` ORDER BY due_at NULLS LAST, created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProductionOrder
	for rows.Next() {
		var po ProductionOrder
		if err := rows.Scan(&po.ID, &po.PONum, &po.Customer, &po.Product, &po.QtyKg, &po.OriginLot,
			&po.AssetTag, &po.Status, &po.DueAt, &po.ERPRef, &po.CreatedAt, &po.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, po)
	}
	return out, rows.Err()
}

type CreateProductionOrderInput struct {
	PONum     string  `json:"po_num"`
	Customer  string  `json:"customer"`
	Product   string  `json:"product"`
	QtyKg     float64 `json:"qty_kg"`
	OriginLot string  `json:"origin_lot"`
	AssetTag  string  `json:"asset_tag"`
	Status    string  `json:"status"`
	DueAt     string  `json:"due_at"`
	ERPRef    string  `json:"erp_ref"`
}

func (s *Store) CreateProductionOrder(ctx context.Context, in CreateProductionOrderInput) (*ProductionOrder, error) {
	if strings.TrimSpace(in.PONum) == "" {
		return nil, ErrBadInput
	}
	var dueAt *time.Time
	if in.DueAt != "" {
		if t, err := time.Parse(time.RFC3339, in.DueAt); err == nil {
			dueAt = &t
		}
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "queued"
	}
	var po ProductionOrder
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_production_orders (po_num, customer, product, qty_kg, origin_lot, asset_tag, status, due_at, erp_ref)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,NULLIF($9,''))
		RETURNING id, po_num, customer, product, qty_kg, origin_lot, asset_tag, status, due_at, erp_ref, created_at, updated_at`,
		in.PONum, in.Customer, in.Product, in.QtyKg, in.OriginLot, in.AssetTag, status, dueAt, in.ERPRef).Scan(
		&po.ID, &po.PONum, &po.Customer, &po.Product, &po.QtyKg, &po.OriginLot,
		&po.AssetTag, &po.Status, &po.DueAt, &po.ERPRef, &po.CreatedAt, &po.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.emitProductionOrder(ctx, events.TypeProductionOrderCreated, &po)
	return &po, nil
}

type UpdateProductionOrderInput struct {
	Customer  string  `json:"customer"`
	Product   string  `json:"product"`
	QtyKg     float64 `json:"qty_kg"`
	OriginLot string  `json:"origin_lot"`
	AssetTag  string  `json:"asset_tag"`
	Status    string  `json:"status"`
	DueAt     string  `json:"due_at"`
	ERPRef    string  `json:"erp_ref"`
}

func (s *Store) UpdateProductionOrder(ctx context.Context, poNum string, in UpdateProductionOrderInput) (*ProductionOrder, error) {
	poNum = strings.TrimSpace(poNum)
	if poNum == "" {
		return nil, ErrBadInput
	}
	var dueAt *time.Time
	if in.DueAt != "" {
		if t, err := time.Parse(time.RFC3339, in.DueAt); err == nil {
			dueAt = &t
		}
	}
	var po ProductionOrder
	err := s.pool.QueryRow(ctx, `
		UPDATE erp_production_orders SET
		  customer = COALESCE(NULLIF($2,''), customer),
		  product = COALESCE(NULLIF($3,''), product),
		  qty_kg = CASE WHEN $4 = 0 THEN qty_kg ELSE $4 END,
		  origin_lot = CASE WHEN $5 = '' THEN origin_lot ELSE NULLIF($5,'') END,
		  asset_tag = CASE WHEN $6 = '' THEN asset_tag ELSE NULLIF($6,'') END,
		  status = COALESCE(NULLIF($7,''), status),
		  due_at = COALESCE($8, due_at),
		  erp_ref = CASE WHEN $9 = '' THEN erp_ref ELSE NULLIF($9,'') END,
		  updated_at = NOW()
		WHERE po_num = $1
		RETURNING id, po_num, customer, product, qty_kg, origin_lot, asset_tag, status, due_at, erp_ref, created_at, updated_at`,
		poNum, in.Customer, in.Product, in.QtyKg, in.OriginLot, in.AssetTag, in.Status, dueAt, in.ERPRef).Scan(
		&po.ID, &po.PONum, &po.Customer, &po.Product, &po.QtyKg, &po.OriginLot,
		&po.AssetTag, &po.Status, &po.DueAt, &po.ERPRef, &po.CreatedAt, &po.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.emitProductionOrder(ctx, events.TypeProductionOrderUpdated, &po)
	return &po, nil
}

func (s *Store) DeleteProductionOrder(ctx context.Context, poNum string) error {
	poNum = strings.TrimSpace(poNum)
	if poNum == "" {
		return ErrBadInput
	}
	var po ProductionOrder
	err := s.pool.QueryRow(ctx, `
		DELETE FROM erp_production_orders WHERE po_num = $1
		RETURNING id, po_num, customer, product, qty_kg, origin_lot, asset_tag, status, due_at, erp_ref, created_at, updated_at`,
		poNum).Scan(&po.ID, &po.PONum, &po.Customer, &po.Product, &po.QtyKg, &po.OriginLot,
		&po.AssetTag, &po.Status, &po.DueAt, &po.ERPRef, &po.CreatedAt, &po.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	s.emitProductionOrder(ctx, events.TypeProductionOrderDeleted, &po)
	return nil
}

type ProductionOrderWebhookInput struct {
	Action string `json:"action"`
	CreateProductionOrderInput
}

func (s *Store) ApplyProductionOrderWebhook(ctx context.Context, in ProductionOrderWebhookInput) (*ProductionOrder, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		action = "upsert"
	}
	switch action {
	case "delete", "cancel":
		if err := s.DeleteProductionOrder(ctx, in.PONum); err != nil {
			return nil, err
		}
		return nil, nil
	case "upsert", "create", "update":
		existing, err := s.getProductionOrderByNum(ctx, in.PONum)
		if err == ErrNotFound {
			return s.CreateProductionOrder(ctx, in.CreateProductionOrderInput)
		}
		if err != nil {
			return nil, err
		}
		_ = existing
		upd := UpdateProductionOrderInput{
			Customer:  in.Customer,
			Product:   in.Product,
			QtyKg:     in.QtyKg,
			OriginLot: in.OriginLot,
			AssetTag:  in.AssetTag,
			Status:    in.Status,
			DueAt:     in.DueAt,
			ERPRef:    in.ERPRef,
		}
		return s.UpdateProductionOrder(ctx, in.PONum, upd)
	default:
		return nil, ErrBadInput
	}
}

func (s *Store) getProductionOrderByNum(ctx context.Context, poNum string) (*ProductionOrder, error) {
	poNum = strings.TrimSpace(poNum)
	var po ProductionOrder
	err := s.pool.QueryRow(ctx, `
		SELECT id, po_num, customer, product, qty_kg, origin_lot, asset_tag, status, due_at, erp_ref, created_at, updated_at
		FROM erp_production_orders WHERE po_num = $1`, poNum).Scan(
		&po.ID, &po.PONum, &po.Customer, &po.Product, &po.QtyKg, &po.OriginLot,
		&po.AssetTag, &po.Status, &po.DueAt, &po.ERPRef, &po.CreatedAt, &po.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &po, nil
}
