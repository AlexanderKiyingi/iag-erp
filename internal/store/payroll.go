package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"iag-erp/backend/internal/events"
)

// Payroll runs.
//
// A run is computed as a draft, approved by someone other than whoever computed
// it, and only then posted to the ledger. Each step is a state the run is in,
// not a flag, so a half-finished payroll is visible as a draft rather than as a
// posted run that happens to be wrong.

// PayrollScheme is the tax scale payroll is computed against. One deployment,
// one jurisdiction; this becomes configuration the day that stops being true.
const PayrollScheme = "UG_PAYE_MONTHLY"

var (
	// ErrRunNotDraft means the run has already been approved or posted, and the
	// operation asked for only makes sense before that.
	ErrRunNotDraft = errors.New("payroll run is not a draft")
	// ErrRunNotApproved means posting was attempted on a run nobody approved.
	ErrRunNotApproved = errors.New("payroll run is not approved")
	// ErrSelfApproval means the approver computed the run themselves.
	ErrSelfApproval = errors.New("payroll must be approved by someone other than its preparer")
	// ErrPeriodAlreadyPosted means this month has already reached the ledger.
	ErrPeriodAlreadyPosted = errors.New("a payroll run for this period has already been posted")
)

// PayrollRun is the header for one period's payroll.
type PayrollRun struct {
	ID                   uuid.UUID  `json:"id"`
	RunRef               string     `json:"run_ref"`
	Period               string     `json:"period"`
	Status               string     `json:"status"`
	Currency             string     `json:"currency"`
	EmployeeCount        int        `json:"employee_count"`
	Gross                float64    `json:"gross"`
	TaxableGross         float64    `json:"taxable_gross"`
	PAYE                 float64    `json:"paye"`
	NSSFEmployee         float64    `json:"nssf_employee"`
	NSSFEmployer         float64    `json:"nssf_employer"`
	OtherDeductions      float64    `json:"other_deductions"`
	Net                  float64    `json:"net"`
	CreatedByEmployeeNo  *string    `json:"created_by_employee_no,omitempty"`
	ApprovedByEmployeeNo *string    `json:"approved_by_employee_no,omitempty"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
	PostedAt             *time.Time `json:"posted_at,omitempty"`
	Notes                string     `json:"notes"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	// Skipped names the employees left out and why. A run that silently covered
	// fewer people than the roster would be indistinguishable from a correct one.
	Skipped []PayrollSkip `json:"skipped,omitempty"`
}

// PayrollSkip is one employee the run could not pay, and the reason.
type PayrollSkip struct {
	EmployeeNo string `json:"employee_no"`
	Reason     string `json:"reason"`
}

// Payslip is one employee's stored pay for one run.
type Payslip struct {
	ID     uuid.UUID `json:"id"`
	RunID  uuid.UUID `json:"run_id"`
	Period string    `json:"period"`
	ComputedPayslip
}

const payrollRunColumns = `id, run_ref, period, status, currency, employee_count,
	gross, taxable_gross, paye, nssf_employee, nssf_employer, other_deductions, net,
	created_by_employee_no, approved_by_employee_no, approved_at, posted_at, notes,
	created_at, updated_at`

func scanPayrollRun(row pgx.Row) (*PayrollRun, error) {
	var r PayrollRun
	err := row.Scan(&r.ID, &r.RunRef, &r.Period, &r.Status, &r.Currency, &r.EmployeeCount,
		&r.Gross, &r.TaxableGross, &r.PAYE, &r.NSSFEmployee, &r.NSSFEmployer,
		&r.OtherDeductions, &r.Net, &r.CreatedByEmployeeNo, &r.ApprovedByEmployeeNo,
		&r.ApprovedAt, &r.PostedAt, &r.Notes, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

// payrollEmployee is one person's inputs for a run, gathered before any
// arithmetic happens.
type payrollEmployee struct {
	id           uuid.UUID
	employeeNo   string
	name         string
	departmentCd string
	hireDate     *time.Time
	status       string
}

// CreatePayrollRun computes a draft payroll for a period.
//
// Recomputing a period replaces the draft that was there: payroll is worked and
// reworked as timesheets and allowances land, and keeping every intermediate
// attempt would leave nobody able to say which draft was the one under review.
// A posted period is refused outright.
func (s *Store) CreatePayrollRun(ctx context.Context, period, currency, createdBy, notes string) (*PayrollRun, error) {
	periodStart, periodEnd, err := PeriodBounds(period)
	if err != nil {
		return nil, err
	}
	if currency == "" {
		currency = "UGX"
	}

	var postedExists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM erp_payroll_runs WHERE period = $1 AND status = 'posted')`,
		period).Scan(&postedExists); err != nil {
		return nil, err
	}
	if postedExists {
		return nil, ErrPeriodAlreadyPosted
	}

	bands, err := s.LoadTaxBands(ctx, PayrollScheme, periodEnd)
	if err != nil {
		return nil, err
	}
	if len(bands) == 0 {
		return nil, fmt.Errorf("no %s tax bands effective on %s", PayrollScheme, periodEnd.Format("2006-01-02"))
	}
	rates, err := s.LoadStatutoryRates(ctx, periodEnd)
	if err != nil {
		return nil, err
	}

	workingDays, _, err := s.WorkingDaysBetween(ctx, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	staff, err := s.payrollEmployees(ctx, periodEnd)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Drafts for the period are replaced, not accumulated.
	if _, err := tx.Exec(ctx,
		`DELETE FROM erp_payroll_runs WHERE period = $1 AND status IN ('draft', 'cancelled')`,
		period); err != nil {
		return nil, err
	}

	run := PayrollRun{
		Period:   period,
		Status:   "draft",
		Currency: currency,
		RunRef:   fmt.Sprintf("PR-%s-%s", period, strings.ToUpper(uuid.NewString()[:6])),
		Notes:    notes,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO erp_payroll_runs (run_ref, period, status, currency, created_by_employee_no, notes)
		VALUES ($1, $2, 'draft', $3, NULLIF($4,''), $5)
		RETURNING id`, run.RunRef, period, currency, createdBy, notes).Scan(&run.ID); err != nil {
		return nil, err
	}

	for _, emp := range staff {
		comp, err := s.currentCompensationTx(ctx, tx, emp.id, periodEnd)
		if err != nil {
			if errors.Is(err, ErrNoCompensation) {
				run.Skipped = append(run.Skipped, PayrollSkip{emp.employeeNo, "no effective compensation record"})
				continue
			}
			return nil, err
		}

		components, err := s.employeePayComponentsTx(ctx, tx, emp.id, periodStart, periodEnd)
		if err != nil {
			return nil, err
		}
		unpaidDays, err := s.unpaidLeaveDaysInPeriod(ctx, emp.id, periodStart, periodEnd)
		if err != nil {
			return nil, err
		}

		slip := ComputePayslip(PayslipInput{
			EmployeeNo:      emp.employeeNo,
			EmployeeName:    emp.name,
			DepartmentCode:  emp.departmentCd,
			Currency:        comp.Currency,
			MonthlyBasic:    comp.MonthlyGross,
			WorkingDays:     workingDays,
			UnpaidLeaveDays: unpaidDays,
			Components:      components,
			Bands:           bands,
			Statutory:       rates,
		})

		if err := s.insertPayslipTx(ctx, tx, run.ID, emp.id, slip); err != nil {
			return nil, err
		}

		run.EmployeeCount++
		run.Gross += slip.Gross
		run.TaxableGross += slip.TaxableGross
		run.PAYE += slip.PAYE
		run.NSSFEmployee += slip.NSSFEmployee
		run.NSSFEmployer += slip.NSSFEmployer
		run.OtherDeductions += slip.OtherDeductions
		run.Net += slip.Net
	}

	if _, err := tx.Exec(ctx, `
		UPDATE erp_payroll_runs SET employee_count = $2, gross = $3, taxable_gross = $4,
		  paye = $5, nssf_employee = $6, nssf_employer = $7, other_deductions = $8,
		  net = $9, updated_at = NOW()
		WHERE id = $1`,
		run.ID, run.EmployeeCount, round2(run.Gross), round2(run.TaxableGross), round2(run.PAYE),
		round2(run.NSSFEmployee), round2(run.NSSFEmployer), round2(run.OtherDeductions),
		round2(run.Net)); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	stored, err := s.GetPayrollRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	stored.Skipped = run.Skipped
	return stored, nil
}

// payrollEmployees is who gets paid: everyone on the roster who had not left by
// the end of the period. Terminated staff are excluded; someone on leave, on
// probation or suspended is still owed their pay.
func (s *Store) payrollEmployees(ctx context.Context, asOf time.Time) ([]payrollEmployee, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.employee_no, e.first_name || ' ' || e.last_name,
		       COALESCE(d.code, ''), e.hire_date, e.status
		FROM erp_employees e
		LEFT JOIN erp_departments d ON d.id = e.department_id
		WHERE e.status <> 'terminated'
		  AND (e.hire_date IS NULL OR e.hire_date <= $1::date)
		ORDER BY e.employee_no`, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []payrollEmployee
	for rows.Next() {
		var e payrollEmployee
		if err := rows.Scan(&e.id, &e.employeeNo, &e.name, &e.departmentCd, &e.hireDate, &e.status); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) currentCompensationTx(ctx context.Context, tx pgx.Tx, employeeID uuid.UUID, asOf time.Time) (*Compensation, error) {
	var c Compensation
	c.EmployeeID = employeeID
	err := tx.QueryRow(ctx, `
		SELECT monthly_gross, currency, working_days_per_month, effective_from
		FROM erp_employee_compensation
		WHERE employee_id = $1 AND effective_from <= $2::date
		ORDER BY effective_from DESC LIMIT 1`, employeeID, asOf).
		Scan(&c.MonthlyGross, &c.Currency, &c.WorkingDaysPerMonth, &c.EffectiveFrom)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoCompensation
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) employeePayComponentsTx(ctx context.Context, tx pgx.Tx, employeeID uuid.UUID,
	periodStart, periodEnd time.Time) ([]PayComponent, error) {

	rows, err := tx.Query(ctx, `
		SELECT c.code, c.name, c.kind, epc.amount, c.taxable, c.pensionable
		FROM erp_employee_pay_components epc
		JOIN erp_pay_components c ON c.code = epc.component_code
		WHERE epc.employee_id = $1 AND c.active = true
		  AND epc.effective_from <= $3::date
		  AND (epc.effective_to IS NULL OR epc.effective_to >= $2::date)
		ORDER BY c.kind, c.code`, employeeID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayComponent
	for rows.Next() {
		var p PayComponent
		if err := rows.Scan(&p.Code, &p.Name, &p.Kind, &p.Amount, &p.Taxable, &p.Pensionable); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// unpaidLeaveDaysInPeriod counts approved unpaid leave falling inside the
// period, in working days.
//
// A request is clipped to the period rather than counted whole: leave running
// from the 28th to the 3rd belongs to two payrolls, and charging all of it to
// either one underpays somebody by the difference.
func (s *Store) unpaidLeaveDaysInPeriod(ctx context.Context, employeeID uuid.UUID,
	periodStart, periodEnd time.Time) (float64, error) {

	rows, err := s.pool.Query(ctx, `
		SELECT lr.starts_on, lr.ends_on
		FROM erp_leave_requests lr
		JOIN erp_leave_types lt ON lt.id = lr.leave_type_id
		WHERE lr.employee_id = $1 AND lr.status = 'approved' AND lt.paid = false
		  AND lr.starts_on <= $3::date AND lr.ends_on >= $2::date`,
		employeeID, periodStart, periodEnd)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type span struct{ start, end time.Time }
	var spans []span
	for rows.Next() {
		var sp span
		if err := rows.Scan(&sp.start, &sp.end); err != nil {
			return 0, err
		}
		spans = append(spans, sp)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var total float64
	for _, sp := range spans {
		start, end := sp.start.UTC(), sp.end.UTC()
		if start.Before(periodStart) {
			start = periodStart
		}
		if end.After(periodEnd) {
			end = periodEnd
		}
		days, _, err := s.WorkingDaysBetween(ctx, start, end)
		if err != nil {
			// A span that clips to nothing is not an error worth failing a
			// whole payroll run over.
			if errors.Is(err, ErrBadInput) {
				continue
			}
			return 0, err
		}
		total += days
	}
	return total, nil
}

func (s *Store) insertPayslipTx(ctx context.Context, tx pgx.Tx, runID, employeeID uuid.UUID, slip ComputedPayslip) error {
	var payslipID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO erp_payslips (run_id, employee_id, employee_no, employee_name, department_code,
			currency, basic, earned_basic, allowances, gross, taxable_gross, paye,
			nssf_employee, nssf_employer, other_deductions, total_deductions, net,
			working_days, unpaid_leave_days)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id`,
		runID, employeeID, slip.EmployeeNo, slip.EmployeeName, slip.DepartmentCode,
		slip.Currency, slip.Basic, slip.EarnedBasic, slip.Allowances, slip.Gross,
		slip.TaxableGross, slip.PAYE, slip.NSSFEmployee, slip.NSSFEmployer,
		slip.OtherDeductions, slip.TotalDeductions, slip.Net,
		slip.WorkingDays, slip.UnpaidLeaveDays).Scan(&payslipID); err != nil {
		return err
	}
	for _, line := range slip.Lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO erp_payslip_lines (payslip_id, component_code, name, kind, amount, seq)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			payslipID, line.ComponentCode, line.Name, line.Kind, line.Amount, line.Seq); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetPayrollRun(ctx context.Context, id uuid.UUID) (*PayrollRun, error) {
	return scanPayrollRun(s.pool.QueryRow(ctx,
		`SELECT `+payrollRunColumns+` FROM erp_payroll_runs WHERE id = $1`, id))
}

func (s *Store) ListPayrollRuns(ctx context.Context, period, status string, limit, offset int) ([]PayrollRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + payrollRunColumns + ` FROM erp_payroll_runs WHERE 1=1`
	args := []any{}
	n := 1
	if period != "" {
		q += ` AND period = $` + itoa(n)
		args = append(args, period)
		n++
	}
	if status != "" {
		q += ` AND status = $` + itoa(n)
		args = append(args, status)
		n++
	}
	_ = n
	q += ` ORDER BY period DESC, created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PayrollRun{}
	for rows.Next() {
		r, err := scanPayrollRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// One column list and one reader for payslips. Both the run view and the
// employee's own history select the same shape, and writing the twenty
// destinations twice is how the two drift apart on the next column added.
const payslipColumns = `p.id, p.run_id, r.period, p.employee_no, p.employee_name,
	p.department_code, p.currency, p.basic, p.earned_basic, p.allowances, p.gross,
	p.taxable_gross, p.paye, p.nssf_employee, p.nssf_employer, p.other_deductions,
	p.total_deductions, p.net, p.working_days, p.unpaid_leave_days`

const payslipFrom = `FROM erp_payslips p
	JOIN erp_payroll_runs r ON r.id = p.run_id`

func scanPayslip(row pgx.Row) (*Payslip, error) {
	var p Payslip
	err := row.Scan(&p.ID, &p.RunID, &p.Period, &p.EmployeeNo, &p.EmployeeName,
		&p.DepartmentCode, &p.Currency, &p.Basic, &p.EarnedBasic, &p.Allowances,
		&p.Gross, &p.TaxableGross, &p.PAYE, &p.NSSFEmployee, &p.NSSFEmployer,
		&p.OtherDeductions, &p.TotalDeductions, &p.Net, &p.WorkingDays,
		&p.UnpaidLeaveDays)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

// withPayslipLines loads the lines for each payslip. A payslip without the
// components that produced its net figure cannot be queried by the person it
// belongs to, which is the only reason a payslip exists.
func (s *Store) withPayslipLines(ctx context.Context, slips []Payslip) ([]Payslip, error) {
	for i := range slips {
		lines, err := s.payslipLines(ctx, slips[i].ID)
		if err != nil {
			return nil, err
		}
		slips[i].Lines = lines
	}
	return slips, nil
}

// ListPayslips returns the payslips of a run, optionally narrowed to an access
// scope so an employee reading their own payslip does not read the run.
func (s *Store) ListPayslips(ctx context.Context, runID uuid.UUID, restrictToEmployeeNos []string) ([]Payslip, error) {
	q := `SELECT ` + payslipColumns + ` ` + payslipFrom + ` WHERE p.run_id = $1`
	args := []any{runID}
	if len(restrictToEmployeeNos) > 0 {
		q += ` AND p.employee_no = ANY($2)`
		args = append(args, restrictToEmployeeNos)
	}
	q += ` ORDER BY p.employee_no`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payslip{}
	for rows.Next() {
		p, err := scanPayslip(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.withPayslipLines(ctx, out)
}

// ListEmployeePayslips returns one employee's payslip history — the self-service
// view, which reads payslips without reading the run they belong to.
func (s *Store) ListEmployeePayslips(ctx context.Context, employeeNo string, limit int) ([]Payslip, error) {
	if limit <= 0 || limit > 120 {
		limit = 24
	}
	// Only posted runs: a draft is a working figure, and an employee shown one
	// would be reading a number that is still being changed.
	rows, err := s.pool.Query(ctx, `SELECT `+payslipColumns+` `+payslipFrom+`
		WHERE p.employee_no = $1 AND r.status = 'posted'
		ORDER BY r.period DESC
		LIMIT `+itoa(limit), employeeNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payslip{}
	for rows.Next() {
		p, err := scanPayslip(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.withPayslipLines(ctx, out)
}

func (s *Store) payslipLines(ctx context.Context, payslipID uuid.UUID) ([]PayslipLine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT component_code, name, kind, amount, seq
		FROM erp_payslip_lines WHERE payslip_id = $1 ORDER BY seq`, payslipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PayslipLine{}
	for rows.Next() {
		var l PayslipLine
		if err := rows.Scan(&l.ComponentCode, &l.Name, &l.Kind, &l.Amount, &l.Seq); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ApprovePayrollRun moves a draft to approved.
//
// The approver must not be the preparer. This is the separation of duties that
// payroll fraud is defined by the absence of, and it is enforced here rather
// than in the UI because the UI is not what an auditor tests.
func (s *Store) ApprovePayrollRun(ctx context.Context, id uuid.UUID, approvedBy string) (*PayrollRun, error) {
	approvedBy = strings.TrimSpace(approvedBy)
	if approvedBy == "" {
		return nil, ErrBadInput
	}
	run, err := s.GetPayrollRun(ctx, id)
	if err != nil {
		return nil, err
	}
	if run.Status != "draft" {
		return nil, ErrRunNotDraft
	}
	if run.CreatedByEmployeeNo != nil && *run.CreatedByEmployeeNo == approvedBy {
		return nil, ErrSelfApproval
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_payroll_runs
		SET status = 'approved', approved_by_employee_no = $2, approved_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'draft'`, id, approvedBy); err != nil {
		return nil, err
	}
	return s.GetPayrollRun(ctx, id)
}

// PostPayrollRun books an approved run and announces it to finance.
//
// The event is enqueued on the same transaction that marks the run posted, so a
// run cannot be posted here and never reach the ledger, nor reach the ledger
// twice because a retry re-sent it.
func (s *Store) PostPayrollRun(ctx context.Context, id uuid.UUID) (*PayrollRun, error) {
	run, err := s.GetPayrollRun(ctx, id)
	if err != nil {
		return nil, err
	}
	if run.Status != "approved" {
		return nil, ErrRunNotApproved
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE erp_payroll_runs SET status = 'posted', posted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'approved'`, id)
	if err != nil {
		// The partial unique index on (period) where status = 'posted' is what
		// turns a concurrent second post into an error instead of a duplicate.
		if strings.Contains(err.Error(), "erp_payroll_runs_posted_period_idx") {
			return nil, ErrPeriodAlreadyPosted
		}
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrRunNotApproved
	}

	if txPub, ok := s.events.(TxEventPublisher); ok {
		if err := txPub.PublishTx(ctx, tx, events.TypePayrollRunPosted, map[string]any{
			"run_ref":          run.RunRef,
			"period":           run.Period,
			"currency":         run.Currency,
			"employee_count":   run.EmployeeCount,
			"gross":            run.Gross,
			"paye":             run.PAYE,
			"nssf_employee":    run.NSSFEmployee,
			"nssf_employer":    run.NSSFEmployer,
			"other_deductions": run.OtherDeductions,
			"net":              run.Net,
		}, run.RunRef); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetPayrollRun(ctx, id)
}

// CancelPayrollRun abandons a draft or an approved run that will not be posted.
func (s *Store) CancelPayrollRun(ctx context.Context, id uuid.UUID) (*PayrollRun, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE erp_payroll_runs SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND status IN ('draft', 'approved')`, id)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		// Posted runs are not cancelled. A payroll that has reached the ledger
		// is reversed there, by finance, with a journal that says so.
		return nil, ErrConflict
	}
	return s.GetPayrollRun(ctx, id)
}
