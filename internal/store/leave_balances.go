package store

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"iag-erp/backend/internal/events"
)

// Leave balances.
//
// Entitlement lives on the leave type, consumption on approved requests, and
// until now nothing turned the two into a balance. Finance was therefore told
// about leave being taken and never about the obligation being earned, which is
// the side an accrued-leave liability comes from.
//
// The rules that vary by organisation — carry-over cap, probation — are columns
// on erp_leave_types, not constants here. What this file owns is the arithmetic
// they feed.

// earnedToDate pro-rates an annual entitlement across the part of the year the
// employee has actually served, after any probation.
//
// Accrual is monthly rather than daily: leave is granted in days, and a daily
// accrual produces balances like 11.6438 that nobody can act on. A month is
// counted once it has started, which is the reading that favours the employee
// and, for a liability, is the more prudent one.
func earnedToDate(daysPerYear float64, hire time.Time, accruesAfterMonths int, asOf time.Time) float64 {
	if daysPerYear <= 0 {
		return 0
	}
	year := asOf.Year()
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)

	// Entitlement begins at hire plus probation, and never before the year did.
	eligible := hire.AddDate(0, accruesAfterMonths, 0)
	if eligible.After(start) {
		start = eligible
	}
	if start.After(asOf) {
		return 0
	}

	months := (asOf.Year()-start.Year())*12 + int(asOf.Month()) - int(start.Month()) + 1
	if months < 0 {
		months = 0
	}
	if months > 12 {
		months = 12
	}
	earned := daysPerYear * float64(months) / 12
	// Two decimals: leave is administered in half and quarter days, and more
	// precision than that is noise carried into a money figure downstream.
	return math.Round(earned*100) / 100
}

// RecomputeLeaveBalanceTx recalculates one employee/leave-type balance for the
// year containing asOf, inside the caller's transaction, and returns it.
//
// It runs in the same transaction as the leave decision that triggered it, so
// the balance and the request that changed it commit together or not at all.
func (s *Store) RecomputeLeaveBalanceTx(
	ctx context.Context, tx pgx.Tx, employeeID, leaveTypeID uuid.UUID, asOf time.Time,
) (*LeaveBalance, error) {
	var (
		bal                LeaveBalance
		hire               *time.Time
		daysPerYear        float64
		carryOverMax       float64
		accruesAfterMonths int
		paid               bool
	)
	err := tx.QueryRow(ctx, `
		SELECT e.employee_no, e.hire_date, t.code, t.days_per_year, t.paid,
		       t.carry_over_max_days, t.accrues_after_months
		FROM erp_employees e, erp_leave_types t
		WHERE e.id = $1 AND t.id = $2`, employeeID, leaveTypeID).
		Scan(&bal.EmployeeNo, &hire, &bal.LeaveTypeCode, &daysPerYear, &paid,
			&carryOverMax, &accruesAfterMonths)
	if err != nil {
		return nil, err
	}

	bal.EmployeeID = employeeID
	bal.Year = asOf.Year()

	// Unpaid leave is time off, not an obligation: no entitlement accrues and
	// no liability follows, so the balance stays at zero rather than tracking
	// days nobody owes anything for.
	if !paid {
		return &bal, s.upsertLeaveBalanceTx(ctx, tx, leaveTypeID, bal)
	}

	if hire != nil {
		bal.EarnedDays = earnedToDate(daysPerYear, hire.UTC(), accruesAfterMonths, asOf)
	}

	// Approved days falling in this accrual year. Rejected and cancelled
	// requests never consumed anything.
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(days), 0)
		FROM erp_leave_requests
		WHERE employee_id = $1 AND leave_type_id = $2
		  AND status = 'approved'
		  AND EXTRACT(YEAR FROM starts_on) = $3`,
		employeeID, leaveTypeID, bal.Year).Scan(&bal.TakenDays); err != nil {
		return nil, err
	}

	// Carry-over: last year's unused balance, capped by policy. A cap of zero
	// means leave expires at year end.
	if carryOverMax > 0 {
		var prior float64
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(opening_days + earned_days - taken_days, 0)
			FROM erp_leave_balances
			WHERE employee_id = $1 AND leave_type_id = $2 AND accrual_year = $3`,
			employeeID, leaveTypeID, bal.Year-1).Scan(&prior)
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		bal.OpeningDays = math.Max(0, math.Min(prior, carryOverMax))
	}

	bal.BalanceDays = bal.OpeningDays + bal.EarnedDays - bal.TakenDays
	return &bal, s.upsertLeaveBalanceTx(ctx, tx, leaveTypeID, bal)
}

func (s *Store) upsertLeaveBalanceTx(ctx context.Context, tx pgx.Tx, leaveTypeID uuid.UUID, b LeaveBalance) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO erp_leave_balances
			(employee_id, leave_type_id, accrual_year, opening_days, earned_days, taken_days, computed_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (employee_id, leave_type_id, accrual_year) DO UPDATE
		SET opening_days = EXCLUDED.opening_days,
		    earned_days  = EXCLUDED.earned_days,
		    taken_days   = EXCLUDED.taken_days,
		    computed_at  = NOW()`,
		b.EmployeeID, leaveTypeID, b.Year, b.OpeningDays, b.EarnedDays, b.TakenDays)
	return err
}

// PublishLeaveBalanceTx announces a balance change on the same transaction that
// produced it, so finance cannot be left holding a stale obligation because an
// enqueue failed after the commit.
func (s *Store) PublishLeaveBalanceTx(ctx context.Context, tx pgx.Tx, b *LeaveBalance) error {
	if s == nil || b == nil {
		return nil
	}
	txPub, ok := s.events.(TxEventPublisher)
	if !ok {
		return nil
	}
	return txPub.PublishTx(ctx, tx, events.TypeLeaveBalanceChanged, map[string]any{
		"employee_no":     b.EmployeeNo,
		"leave_type_code": b.LeaveTypeCode,
		"accrual_year":    b.Year,
		"opening_days":    b.OpeningDays,
		"earned_days":     b.EarnedDays,
		"taken_days":      b.TakenDays,
		"balance_days":    b.BalanceDays,
	}, b.EmployeeNo+":"+b.LeaveTypeCode+":"+strconv.Itoa(b.Year))
}
