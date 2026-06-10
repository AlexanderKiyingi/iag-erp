package store

import (
	"context"
	"encoding/json"
)

func (s *Store) LogIntegrationCall(ctx context.Context, target, operation, correlation, status string, req, resp json.RawMessage, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO erp_integration_calls (target, operation, correlation, status, request_body, response_body, error_message)
		VALUES ($1, $2, NULLIF($3,''), $4, $5, $6, NULLIF($7,''))`,
		target, operation, correlation, status, req, resp, errMsg)
	return err
}

func (s *Store) ListIntegrationCalls(ctx context.Context, target string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT target, operation, correlation, status, error_message, created_at FROM erp_integration_calls WHERE 1=1`
	args := []any{}
	if target != "" {
		q += ` AND target = $1`
		args = append(args, target)
	}
	q += ` ORDER BY created_at DESC LIMIT ` + itoa(limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var targetVal, op, corr, status, errMsg string
		var createdAt any
		if err := rows.Scan(&targetVal, &op, &corr, &status, &errMsg, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"target": targetVal, "operation": op, "correlation": corr,
			"status": status, "error_message": errMsg, "created_at": createdAt,
		})
	}
	return out, rows.Err()
}
