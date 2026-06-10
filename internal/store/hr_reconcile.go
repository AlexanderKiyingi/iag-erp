package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type LeaveBalance struct {
	EmployeeNo     string  `json:"employee_no"`
	LeaveTypeCode  string  `json:"leave_type_code"`
	Year           int     `json:"year"`
	EntitledDays   float64 `json:"entitled_days"`
	UsedDays       float64 `json:"used_days"`
	RemainingDays  float64 `json:"remaining_days"`
}

func (s *Store) ReconcileEmployeeLeaveStatus(ctx context.Context, employeeID uuid.UUID) error {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var activeLeave bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM erp_leave_requests
		  WHERE employee_id = $1 AND status = 'approved'
		    AND starts_on <= $2::date AND ends_on >= $2::date
		)`, employeeID, today).Scan(&activeLeave)
	if err != nil {
		return err
	}
	status := "active"
	if activeLeave {
		status = "on_leave"
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE erp_employees SET status = $2, updated_at = NOW()
		WHERE id = $1 AND status IN ('active', 'on_leave')`, employeeID, status)
	return err
}

func (s *Store) ReconcileAllEmployeeLeaveStatuses(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM erp_employees WHERE status IN ('active', 'on_leave')`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return n, err
		}
		if err := s.ReconcileEmployeeLeaveStatus(ctx, id); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func (s *Store) HasOverlappingLeave(ctx context.Context, employeeID uuid.UUID, start, end time.Time, excludeID *uuid.UUID) (bool, error) {
	q := `
		SELECT EXISTS (
		  SELECT 1 FROM erp_leave_requests
		  WHERE employee_id = $1 AND status IN ('pending', 'approved')
		    AND starts_on <= $3::date AND ends_on >= $2::date`
	args := []any{employeeID, start, end}
	if excludeID != nil {
		q += ` AND id <> $4`
		args = append(args, *excludeID)
	}
	q += `)`
	var overlap bool
	err := s.pool.QueryRow(ctx, q, args...).Scan(&overlap)
	return overlap, err
}

func (s *Store) GetLeaveBalance(ctx context.Context, employeeNo, leaveTypeCode string, year int) (*LeaveBalance, error) {
	if year <= 0 {
		year = time.Now().UTC().Year()
	}
	var entitled float64
	err := s.pool.QueryRow(ctx, `
		SELECT lt.days_per_year FROM erp_leave_types lt WHERE lt.code = $1`, leaveTypeCode).Scan(&entitled)
	if err != nil {
		return nil, ErrNotFound
	}
	var used float64
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(lr.days), 0)
		FROM erp_leave_requests lr
		JOIN erp_employees e ON e.id = lr.employee_id
		JOIN erp_leave_types lt ON lt.id = lr.leave_type_id
		WHERE e.employee_no = $1 AND lt.code = $2 AND lr.status = 'approved'
		  AND EXTRACT(YEAR FROM lr.starts_on) = $3`, employeeNo, leaveTypeCode, year).Scan(&used)
	if err != nil {
		return nil, err
	}
	return &LeaveBalance{
		EmployeeNo:    employeeNo,
		LeaveTypeCode: leaveTypeCode,
		Year:          year,
		EntitledDays:  entitled,
		UsedDays:      used,
		RemainingDays: entitled - used,
	}, nil
}
