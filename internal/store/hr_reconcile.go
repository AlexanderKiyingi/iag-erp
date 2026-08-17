package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// How a leave request is checked against a balance.
const (
	// LeaveBasisEntitlement allows the whole year's entitlement from 1 January.
	// This is the historical behaviour and the default.
	LeaveBasisEntitlement = "entitlement"
	// LeaveBasisAccrual allows only what has been earned by the request date.
	LeaveBasisAccrual = "accrual"
)

// LeaveBalance is one employee's position on one leave type for one year,
// stated on both of the two bases that exist.
//
// The two answer different questions and are both correct:
//
//	RemainingDays = opening + entitled − taken   what may still be booked
//	BalanceDays   = opening + earned   − taken   what is owed today
//
// Entitlement is the whole year granted up front; accrual is the part of it
// served for so far. BalanceDays is the obligation, and the figure published to
// finance — a liability is measured from what has been earned, never from what
// somebody may eventually become entitled to.
type LeaveBalance struct {
	EmployeeNo    string `json:"employee_no"`
	LeaveTypeCode string `json:"leave_type_code"`
	Year          int    `json:"year"`
	// EntitledDays is the leave type's full annual entitlement.
	EntitledDays float64 `json:"entitled_days"`
	// UsedDays mirrors TakenDays, kept for callers that read the older name.
	UsedDays float64 `json:"used_days"`
	// RemainingDays is the entitlement basis: opening + entitled − taken.
	RemainingDays float64 `json:"remaining_days"`

	// OpeningDays is last year's unused balance, capped by policy.
	OpeningDays float64 `json:"opening_days"`
	// EarnedDays is entitlement accrued to date, pro-rated for service.
	EarnedDays float64 `json:"earned_days"`
	// TakenDays is approved days falling in the year.
	TakenDays float64 `json:"taken_days"`
	// BalanceDays is the accrual basis: opening + earned − taken.
	BalanceDays float64 `json:"balance_days"`

	EmployeeID uuid.UUID `json:"-"`
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

// GetLeaveBalance returns one employee's position on one leave type for a year,
// on both bases at once.
//
// There used to be two answers to "what is the balance". This function returned
// the entitlement one — a whole year granted on 1 January — and left the accrual
// fields it declares at zero, while RecomputeLeaveBalanceTx wrote the accrual
// one into erp_leave_balances and published *that* to finance. So the figure an
// employee was shown and the obligation finance carried were computed
// differently and could not be reconciled to each other.
//
// Both are now computed here, from the same inputs, by the same arithmetic the
// materialised balance uses. They are different questions, not different
// answers: "how much may I book this year" and "how much is owed today".
func (s *Store) GetLeaveBalance(ctx context.Context, employeeNo, leaveTypeCode string, year int) (*LeaveBalance, error) {
	asOf := time.Now().UTC()
	if year <= 0 {
		year = asOf.Year()
	}
	// For a past or future year, accrual is measured at that year's end rather
	// than today, or a closed year would report itself part-earned forever.
	if year != asOf.Year() {
		asOf = time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	}

	var (
		bal                LeaveBalance
		hire               *time.Time
		paid               bool
		carryOverMax       float64
		accruesAfterMonths int
	)
	bal.EmployeeNo = employeeNo
	bal.LeaveTypeCode = leaveTypeCode
	bal.Year = year

	err := s.pool.QueryRow(ctx, `
		SELECT e.id, e.hire_date, lt.days_per_year, lt.paid,
		       lt.carry_over_max_days, lt.accrues_after_months
		FROM erp_employees e, erp_leave_types lt
		WHERE e.employee_no = $1 AND lt.code = $2`, employeeNo, leaveTypeCode).
		Scan(&bal.EmployeeID, &hire, &bal.EntitledDays, &paid, &carryOverMax, &accruesAfterMonths)
	if err != nil {
		return nil, ErrNotFound
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(lr.days), 0)
		FROM erp_leave_requests lr
		JOIN erp_leave_types lt ON lt.id = lr.leave_type_id
		WHERE lr.employee_id = $1 AND lt.code = $2 AND lr.status = 'approved'
		  AND EXTRACT(YEAR FROM lr.starts_on) = $3`,
		bal.EmployeeID, leaveTypeCode, year).Scan(&bal.TakenDays); err != nil {
		return nil, err
	}
	bal.UsedDays = bal.TakenDays

	// Unpaid leave accrues nothing and is owed nothing, so every derived figure
	// stays at zero. It is still taken, and TakenDays above records that.
	if !paid {
		return &bal, nil
	}

	if hire != nil {
		bal.EarnedDays = earnedToDate(bal.EntitledDays, hire.UTC(), accruesAfterMonths, asOf)
	}

	if carryOverMax > 0 {
		var prior float64
		err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(opening_days + earned_days - taken_days, 0)
			FROM erp_leave_balances
			WHERE employee_id = $1 AND accrual_year = $2
			  AND leave_type_id = (SELECT id FROM erp_leave_types WHERE code = $3)`,
			bal.EmployeeID, year-1, leaveTypeCode).Scan(&prior)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		bal.OpeningDays = math.Max(0, math.Min(prior, carryOverMax))
	}

	// Entitlement basis: what may still be booked this year.
	bal.RemainingDays = bal.OpeningDays + bal.EntitledDays - bal.TakenDays
	// Accrual basis: what is owed today. This is the figure published to finance.
	bal.BalanceDays = bal.OpeningDays + bal.EarnedDays - bal.TakenDays
	return &bal, nil
}

// BookableDays is the figure a new leave request is checked against, under the
// deployment's policy.
//
// Entitlement lets someone take their whole year in January and is what most
// organisations actually operate; accrual only lets them take what they have
// earned so far and is the stricter, cash-safer reading. Which one applies is a
// policy decision, so it is configuration rather than a constant here.
func (b LeaveBalance) BookableDays(basis string) float64 {
	if basis == LeaveBasisAccrual {
		return b.BalanceDays
	}
	return b.RemainingDays
}
