package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"iag-erp/backend/internal/events"
)

// Employee compensation.
//
// No service on the platform held a pay rate, so a day of leave could not be
// valued and the accrued-leave liability had to be stated by an operator. It
// lives here because HR owns the person and pay is an attribute of employment.
//
// Only the derived daily rate leaves this service. Gross, benefits and history
// stay put: finance needs to value an obligation, not to know what anyone earns.

// Compensation is one effective-dated pay record.
type Compensation struct {
	EmployeeID          uuid.UUID `json:"employeeId"`
	EmployeeNo          string    `json:"employeeNo"`
	MonthlyGross        float64   `json:"monthlyGross"`
	Currency            string    `json:"currency"`
	WorkingDaysPerMonth float64   `json:"workingDaysPerMonth"`
	EffectiveFrom       time.Time `json:"effectiveFrom"`
}

// DailyRate is what one day of this employee's time is worth — the figure a
// day of untaken leave is valued at.
func (c Compensation) DailyRate() float64 {
	if c.WorkingDaysPerMonth <= 0 {
		return 0
	}
	return math.Round(c.MonthlyGross/c.WorkingDaysPerMonth*100) / 100
}

// ErrNoCompensation means the employee has no pay record effective yet.
var ErrNoCompensation = errors.New("employee has no effective compensation record")

// SetCompensation records a new effective-dated pay record and publishes the
// resulting daily rate.
//
// Effective dating rather than overwriting: a pay rise re-measures the leave
// liability from the date it applies, and the prior rate is still needed to
// explain what was booked before it.
func (s *Store) SetCompensation(ctx context.Context, employeeNo string, monthlyGross float64,
	currency string, workingDays float64, effectiveFrom time.Time) (*Compensation, error) {

	if monthlyGross <= 0 || workingDays <= 0 {
		return nil, ErrBadInput
	}
	if currency == "" {
		currency = "UGX"
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	c := Compensation{
		EmployeeNo:          employeeNo,
		MonthlyGross:        monthlyGross,
		Currency:            currency,
		WorkingDaysPerMonth: workingDays,
		EffectiveFrom:       effectiveFrom.UTC().Truncate(24 * time.Hour),
	}
	if err := tx.QueryRow(ctx,
		`SELECT id FROM erp_employees WHERE employee_no = $1`, employeeNo).Scan(&c.EmployeeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO erp_employee_compensation
			(employee_id, monthly_gross, currency, working_days_per_month, effective_from)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (employee_id, effective_from) DO UPDATE
		SET monthly_gross = EXCLUDED.monthly_gross,
		    currency = EXCLUDED.currency,
		    working_days_per_month = EXCLUDED.working_days_per_month`,
		c.EmployeeID, c.MonthlyGross, c.Currency, c.WorkingDaysPerMonth, c.EffectiveFrom); err != nil {
		return nil, err
	}

	if err := s.publishRateTx(ctx, tx, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &c, nil
}

// publishRateTx announces the daily rate on the transaction that set it.
//
// Only the rate, the currency and the date it applies from cross the boundary.
// Publishing gross would put every salary on a topic several services read.
func (s *Store) publishRateTx(ctx context.Context, tx pgx.Tx, c Compensation) error {
	txPub, ok := s.events.(TxEventPublisher)
	if !ok {
		return nil
	}
	return txPub.PublishTx(ctx, tx, events.TypeEmployeeRateChanged, map[string]any{
		"employee_no":    c.EmployeeNo,
		"daily_rate":     c.DailyRate(),
		"currency":       c.Currency,
		"effective_from": c.EffectiveFrom.Format("2006-01-02"),
	}, c.EmployeeNo)
}

// CompensationHistory returns every pay record for an employee, most recent
// first. Effective dating is only useful if the earlier rates can be read: a
// figure booked last quarter is explained by the rate that applied then, not by
// the one in force today.
func (s *Store) CompensationHistory(ctx context.Context, employeeNo string) ([]Compensation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.employee_id, c.monthly_gross, c.currency, c.working_days_per_month, c.effective_from
		FROM erp_employee_compensation c
		JOIN erp_employees e ON e.id = c.employee_id
		WHERE e.employee_no = $1
		ORDER BY c.effective_from DESC`, employeeNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Compensation{}
	for rows.Next() {
		c := Compensation{EmployeeNo: employeeNo}
		if err := rows.Scan(&c.EmployeeID, &c.MonthlyGross, &c.Currency,
			&c.WorkingDaysPerMonth, &c.EffectiveFrom); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CurrentCompensation returns the record in force on the given date.
func (s *Store) CurrentCompensation(ctx context.Context, employeeNo string, asOf time.Time) (*Compensation, error) {
	var c Compensation
	c.EmployeeNo = employeeNo
	err := s.pool.QueryRow(ctx, `
		SELECT c.employee_id, c.monthly_gross, c.currency, c.working_days_per_month, c.effective_from
		FROM erp_employee_compensation c
		JOIN erp_employees e ON e.id = c.employee_id
		WHERE e.employee_no = $1 AND c.effective_from <= $2::date
		ORDER BY c.effective_from DESC
		LIMIT 1`, employeeNo, asOf.UTC()).
		Scan(&c.EmployeeID, &c.MonthlyGross, &c.Currency, &c.WorkingDaysPerMonth, &c.EffectiveFrom)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoCompensation
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
