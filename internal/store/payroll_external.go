package store

// Storing a payroll this service did not compute.
//
// iag-erp has its own engine, and it stays the one that runs when somebody asks
// for /payroll/runs. But the HR app has computed payroll in the browser since
// before this service existed, and that engine is the one an operator actually
// drives -- its payslips had nowhere to go, so they were written to a read-only
// adapter that silently stored nothing.
//
// The shape here is deliberate in three ways.
//
// One call creates the header and every payslip together, inside one
// transaction. A per-run endpoint taking slips one at a time would need an
// iag-erp run id the caller does not have (it mints its own), and would leave a
// half-written payroll behind on any failure.
//
// The figures are stored, not recomputed. They are the caller's arithmetic and
// this service does not second-guess it -- but it does check that each payslip
// agrees with itself, and that the header agrees with the slips. Storing totals
// that contradict their own components would make the run unexplainable later,
// and "the other engine said so" is not an explanation.
//
// It is idempotent on the caller's own run id through erp_external_refs. A
// retried submission returns the run that already exists rather than paying the
// month twice, which is the failure mode that matters most here.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// externalSourceService is the source_service value in erp_external_refs. One
// app submits external payroll today; the column exists so that stays true when
// a second one appears.
const externalSourceService = "iag-hr"

// ExternalPayslipInput is one employee's pay as the caller computed it.
type ExternalPayslipInput struct {
	EmployeeNo      string        `json:"employee_no"`
	EmployeeName    string        `json:"employee_name"`
	DepartmentCode  string        `json:"department_code"`
	Currency        string        `json:"currency"`
	Basic           float64       `json:"basic"`
	EarnedBasic     float64       `json:"earned_basic"`
	Allowances      float64       `json:"allowances"`
	Gross           float64       `json:"gross"`
	TaxableGross    float64       `json:"taxable_gross"`
	PAYE            float64       `json:"paye"`
	NSSFEmployee    float64       `json:"nssf_employee"`
	NSSFEmployer    float64       `json:"nssf_employer"`
	OtherDeductions float64       `json:"other_deductions"`
	TotalDeductions float64       `json:"total_deductions"`
	Net             float64       `json:"net"`
	WorkingDays     float64       `json:"working_days"`
	UnpaidLeaveDays float64       `json:"unpaid_leave_days"`
	Lines           []PayslipLine `json:"lines"`
}

// ExternalRunInput is a whole payroll run computed elsewhere.
type ExternalRunInput struct {
	// ExternalRef is the caller's own id for this run, and the idempotency key.
	ExternalRef string                 `json:"external_ref"`
	Period      string                 `json:"period"`
	Currency    string                 `json:"currency"`
	Notes       string                 `json:"notes"`
	Engine      string                 `json:"engine"`
	Payslips    []ExternalPayslipInput `json:"payslips"`
	// Attrs is how the caller computed it -- overrides applied, default working
	// days assumed, the payslip ids it minted. Kept so the figures can be
	// explained afterwards; never read back into the arithmetic.
	Attrs       map[string]any         `json:"attrs"`
}

// balanced reports whether a payslip's own totals agree with its parts.
//
// A cent of tolerance: the caller rounds in floating point and so does this
// service, and refusing a payroll over a rounding artefact would be its own
// kind of wrong.
func (p ExternalPayslipInput) balanced() bool {
	const tolerance = 0.011
	if math.Abs(p.Gross-(p.EarnedBasic+p.Allowances)) > tolerance {
		return false
	}
	if math.Abs(p.Net-(p.Gross-p.TotalDeductions)) > tolerance {
		return false
	}
	return true
}

func (p ExternalPayslipInput) computed() ComputedPayslip {
	return ComputedPayslip{
		EmployeeNo:      p.EmployeeNo,
		EmployeeName:    p.EmployeeName,
		DepartmentCode:  p.DepartmentCode,
		Currency:        p.Currency,
		Basic:           p.Basic,
		EarnedBasic:     p.EarnedBasic,
		Allowances:      p.Allowances,
		Gross:           p.Gross,
		TaxableGross:    p.TaxableGross,
		PAYE:            p.PAYE,
		NSSFEmployee:    p.NSSFEmployee,
		NSSFEmployer:    p.NSSFEmployer,
		OtherDeductions: p.OtherDeductions,
		TotalDeductions: p.TotalDeductions,
		Net:             p.Net,
		WorkingDays:     p.WorkingDays,
		UnpaidLeaveDays: p.UnpaidLeaveDays,
		Lines:           p.Lines,
	}
}

// RecordExternalPayrollRun stores a payroll computed outside this service.
//
// Returns the run, plus the employees it could not file a payslip against. An
// unknown employee number is skipped rather than failing the whole submission,
// mirroring CreatePayrollRun: a payroll that quietly covered fewer people than
// intended is the failure PayrollSkip exists to make visible.
func (s *Store) RecordExternalPayrollRun(
	ctx context.Context,
	in ExternalRunInput,
	createdBy string,
) (*PayrollRun, []PayrollSkip, error) {
	externalRef := strings.TrimSpace(in.ExternalRef)
	period := strings.TrimSpace(in.Period)
	if externalRef == "" || len(in.Payslips) == 0 {
		return nil, nil, ErrBadInput
	}
	if _, _, err := PeriodBounds(period); err != nil {
		return nil, nil, ErrBadInput
	}
	currency := strings.TrimSpace(in.Currency)
	if currency == "" {
		currency = "UGX"
	}

	for _, p := range in.Payslips {
		if strings.TrimSpace(p.EmployeeNo) == "" {
			return nil, nil, ErrBadInput
		}
		if !p.balanced() {
			return nil, nil, fmt.Errorf("%w: %s", ErrPayslipNotBalanced, p.EmployeeNo)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	// Idempotency first. A retry must return the original run rather than pay
	// the month a second time.
	var existingID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT target_id::uuid FROM erp_external_refs
		WHERE source_service = $1 AND source_type = 'payroll_run' AND source_id = $2`,
		externalSourceService, externalRef).Scan(&existingID)
	if err == nil {
		run, err := scanPayrollRun(tx.QueryRow(ctx,
			`SELECT `+payrollRunColumns+` FROM erp_payroll_runs WHERE id = $1`, existingID))
		if err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return run, nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}

	// A computed run already holding this period is a disagreement between two
	// engines about one month, and not something to settle by overwriting.
	var computedExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM erp_payroll_runs
			WHERE period = $1 AND source = 'computed' AND status IN ('draft','approved','posted')
		)`, period).Scan(&computedExists); err != nil {
		return nil, nil, err
	}
	if computedExists {
		return nil, nil, ErrExternalRunConflict
	}

	run := PayrollRun{
		Period:       period,
		Status:       "draft",
		Currency:     currency,
		RunRef:       fmt.Sprintf("PX-%s-%s", period, strings.ToUpper(uuid.NewString()[:6])),
		Notes:        in.Notes,
		Source:       "external",
		SourceEngine: strings.TrimSpace(in.Engine),
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO erp_payroll_runs
			(run_ref, period, status, currency, created_by_employee_no, notes, source, source_engine, attrs)
		VALUES ($1, $2, 'draft', $3, NULLIF($4,''), $5, 'external', $6, $7)
		RETURNING id`,
		run.RunRef, period, currency, createdBy, in.Notes, run.SourceEngine,
		marshalAttrs(in.Attrs)).Scan(&run.ID); err != nil {
		return nil, nil, err
	}

	var skipped []PayrollSkip
	for _, p := range in.Payslips {
		var employeeID uuid.UUID
		err := tx.QueryRow(ctx,
			`SELECT id FROM erp_employees WHERE employee_no = $1`, p.EmployeeNo).Scan(&employeeID)
		if errors.Is(err, pgx.ErrNoRows) {
			skipped = append(skipped, PayrollSkip{p.EmployeeNo, "no employee with this number"})
			continue
		}
		if err != nil {
			return nil, nil, err
		}

		slip := p.computed()
		if slip.Currency == "" {
			slip.Currency = currency
		}
		if err := s.insertPayslipTx(ctx, tx, run.ID, employeeID, slip); err != nil {
			return nil, nil, err
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

	if run.EmployeeCount == 0 {
		// Every payslip was skipped. An empty run would look like a payroll that
		// legitimately paid nobody.
		return nil, skipped, ErrBadInput
	}

	if _, err := tx.Exec(ctx, `
		UPDATE erp_payroll_runs SET employee_count = $2, gross = $3, taxable_gross = $4,
		  paye = $5, nssf_employee = $6, nssf_employer = $7, other_deductions = $8,
		  net = $9, updated_at = NOW()
		WHERE id = $1`,
		run.ID, run.EmployeeCount, round2(run.Gross), round2(run.TaxableGross), round2(run.PAYE),
		round2(run.NSSFEmployee), round2(run.NSSFEmployer), round2(run.OtherDeductions),
		round2(run.Net)); err != nil {
		return nil, nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO erp_external_refs
			(source_service, source_type, source_id, target_type, target_id, origin, synced_at)
		VALUES ($1, 'payroll_run', $2, 'erp_payroll_run', $3, 'external-payroll', $4)`,
		externalSourceService, externalRef, run.ID.String(), time.Now().UTC()); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}

	run.Gross = round2(run.Gross)
	run.TaxableGross = round2(run.TaxableGross)
	run.PAYE = round2(run.PAYE)
	run.NSSFEmployee = round2(run.NSSFEmployee)
	run.NSSFEmployer = round2(run.NSSFEmployer)
	run.OtherDeductions = round2(run.OtherDeductions)
	run.Net = round2(run.Net)
	run.Skipped = skipped
	return &run, skipped, nil
}
