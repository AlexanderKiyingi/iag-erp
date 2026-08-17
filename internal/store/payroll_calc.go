package store

import (
	"context"
	"math"
	"time"
)

// Payroll arithmetic.
//
// Kept apart from the persistence in payroll.go so the part that has to be
// right can be read, and tested, without a database. Every rate this file
// applies is passed in from erp_tax_bands and erp_statutory_rates: there is no
// percentage written down here.

// TaxBand is one band of a progressive scale.
type TaxBand struct {
	Seq        int      `json:"seq"`
	LowerBound float64  `json:"lower_bound"`
	UpperBound *float64 `json:"upper_bound,omitempty"`
	Rate       float64  `json:"rate"`
	BaseTax    float64  `json:"base_tax"`
}

// StatutoryRate is a flat contribution split between employee and employer.
type StatutoryRate struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	EmployeeRate float64  `json:"employee_rate"`
	EmployerRate float64  `json:"employer_rate"`
	MonthlyCap   *float64 `json:"monthly_cap,omitempty"`
}

// round2 is applied at every boundary where a figure becomes money. Payroll is
// summed across hundreds of payslips and reconciled against a bank file, so a
// half-cent carried through the arithmetic becomes a variance somebody has to
// explain.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// ComputePAYE applies a progressive scale to chargeable income.
//
// The bands must be ordered by lower bound; the caller reads them that way. An
// income below the first band's lower bound is untaxed rather than an error:
// somebody earning less than the threshold is the ordinary case, not a bad one.
func ComputePAYE(chargeable float64, bands []TaxBand) float64 {
	if chargeable <= 0 || len(bands) == 0 {
		return 0
	}
	var tax float64
	for _, b := range bands {
		if chargeable <= b.LowerBound {
			break
		}
		// The applicable band is the last one the income reaches into.
		if b.UpperBound == nil || chargeable <= *b.UpperBound {
			tax = b.BaseTax + b.Rate*(chargeable-b.LowerBound)
			break
		}
		// Income runs past this band; keep going. The cumulative base of the
		// next band already contains everything charged below it.
		tax = b.BaseTax + b.Rate*(*b.UpperBound-b.LowerBound)
	}
	if tax < 0 {
		return 0
	}
	return round2(tax)
}

// ContributionOn returns the employee and employer shares of a statutory
// contribution on pensionable pay.
func (r StatutoryRate) ContributionOn(pensionable float64) (employee, employer float64) {
	if pensionable <= 0 {
		return 0, 0
	}
	base := pensionable
	if r.MonthlyCap != nil && base > *r.MonthlyCap {
		base = *r.MonthlyCap
	}
	return round2(base * r.EmployeeRate), round2(base * r.EmployerRate)
}

// PayComponent is one earning or deduction applied to an employee.
type PayComponent struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"` // earning | deduction
	Amount      float64 `json:"amount"`
	Taxable     bool    `json:"taxable"`
	Pensionable bool    `json:"pensionable"`
}

// PayslipInput is everything needed to compute one payslip.
type PayslipInput struct {
	EmployeeNo      string
	EmployeeName    string
	DepartmentCode  string
	Currency        string
	MonthlyBasic    float64
	WorkingDays     float64
	UnpaidLeaveDays float64
	Components      []PayComponent
	Bands           []TaxBand
	Statutory       []StatutoryRate
}

// PayslipLine is one line of the computed payslip.
type PayslipLine struct {
	ComponentCode string  `json:"component_code"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"` // earning | deduction | statutory
	Amount        float64 `json:"amount"`
	Seq           int     `json:"seq"`
}

// ComputedPayslip is one employee's pay for one period.
type ComputedPayslip struct {
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

// ComputePayslip turns one employee's pay data into a payslip.
//
// The order matters and is the order a payslip is read in: earn the basic,
// add the allowances, decide what of that is taxable and what is pensionable,
// apply the statutory contributions, apply PAYE, then take the voluntary
// deductions off what is left.
func ComputePayslip(in PayslipInput) ComputedPayslip {
	out := ComputedPayslip{
		EmployeeNo:      in.EmployeeNo,
		EmployeeName:    in.EmployeeName,
		DepartmentCode:  in.DepartmentCode,
		Currency:        in.Currency,
		Basic:           round2(in.MonthlyBasic),
		WorkingDays:     in.WorkingDays,
		UnpaidLeaveDays: in.UnpaidLeaveDays,
	}
	if out.Currency == "" {
		out.Currency = "UGX"
	}

	// Unpaid absence reduces the basic pro rata. Unpaid leave that did not
	// reduce pay would make the leave type's `paid = false` mean nothing.
	out.EarnedBasic = round2(in.MonthlyBasic)
	if in.WorkingDays > 0 && in.UnpaidLeaveDays > 0 {
		unpaid := math.Min(in.UnpaidLeaveDays, in.WorkingDays)
		out.EarnedBasic = round2(in.MonthlyBasic * (in.WorkingDays - unpaid) / in.WorkingDays)
	}

	seq := 0
	addLine := func(code, name, kind string, amount float64) {
		seq++
		out.Lines = append(out.Lines, PayslipLine{
			ComponentCode: code, Name: name, Kind: kind, Amount: round2(amount), Seq: seq,
		})
	}
	addLine("BASIC", "Basic pay", "earning", out.EarnedBasic)

	// Basic pay is both taxable and pensionable everywhere this runs; the
	// components decide for themselves.
	taxable := out.EarnedBasic
	pensionable := out.EarnedBasic

	for _, comp := range in.Components {
		if comp.Amount <= 0 {
			continue
		}
		switch comp.Kind {
		case "earning":
			out.Allowances += comp.Amount
			if comp.Taxable {
				taxable += comp.Amount
			}
			if comp.Pensionable {
				pensionable += comp.Amount
			}
			addLine(comp.Code, comp.Name, "earning", comp.Amount)
		case "deduction":
			out.OtherDeductions += comp.Amount
			addLine(comp.Code, comp.Name, "deduction", comp.Amount)
		}
	}

	out.Allowances = round2(out.Allowances)
	out.Gross = round2(out.EarnedBasic + out.Allowances)
	out.TaxableGross = round2(taxable)

	for _, rate := range in.Statutory {
		employee, employer := rate.ContributionOn(pensionable)
		if employee == 0 && employer == 0 {
			continue
		}
		// NSSF is named explicitly on the payslip totals because finance posts
		// it to its own payable account; anything else statutory joins the
		// other deductions.
		if rate.Code == "NSSF" {
			out.NSSFEmployee += employee
			out.NSSFEmployer += employer
		} else {
			out.OtherDeductions += employee
		}
		if employee > 0 {
			addLine(rate.Code, rate.Name+" (employee)", "statutory", employee)
		}
	}
	out.NSSFEmployee = round2(out.NSSFEmployee)
	out.NSSFEmployer = round2(out.NSSFEmployer)

	// PAYE is charged on employment income. The employee's NSSF contribution is
	// not an allowable deduction against it in Uganda, so it is not netted off
	// the chargeable figure.
	out.PAYE = ComputePAYE(out.TaxableGross, in.Bands)
	if out.PAYE > 0 {
		addLine("PAYE", "PAYE", "statutory", out.PAYE)
	}

	out.OtherDeductions = round2(out.OtherDeductions)
	out.TotalDeductions = round2(out.PAYE + out.NSSFEmployee + out.OtherDeductions)
	out.Net = round2(out.Gross - out.TotalDeductions)

	return out
}

// PeriodBounds turns a 'YYYY-MM' period into its first and last dates.
func PeriodBounds(period string) (start, end time.Time, err error) {
	start, err = time.Parse("2006-01", period)
	if err != nil {
		return time.Time{}, time.Time{}, ErrBadInput
	}
	start = start.UTC()
	end = start.AddDate(0, 1, -1)
	return start, end, nil
}

// LoadTaxBands returns the scale in force on a date, most recent effective
// version only.
func (s *Store) LoadTaxBands(ctx context.Context, scheme string, asOf time.Time) ([]TaxBand, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq, lower_bound, upper_bound, rate, base_tax
		FROM erp_tax_bands
		WHERE scheme = $1
		  AND effective_from = (
		      SELECT MAX(effective_from) FROM erp_tax_bands
		      WHERE scheme = $1 AND effective_from <= $2::date
		  )
		ORDER BY seq`, scheme, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaxBand
	for rows.Next() {
		var b TaxBand
		if err := rows.Scan(&b.Seq, &b.LowerBound, &b.UpperBound, &b.Rate, &b.BaseTax); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// LoadStatutoryRates returns the active contributions in force on a date.
func (s *Store) LoadStatutoryRates(ctx context.Context, asOf time.Time) ([]StatutoryRate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (code) code, name, employee_rate, employer_rate, monthly_cap
		FROM erp_statutory_rates
		WHERE active = true AND effective_from <= $1::date
		ORDER BY code, effective_from DESC`, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatutoryRate
	for rows.Next() {
		var r StatutoryRate
		if err := rows.Scan(&r.Code, &r.Name, &r.EmployeeRate, &r.EmployerRate, &r.MonthlyCap); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
