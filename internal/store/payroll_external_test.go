package store

import "testing"

// The service stores an external payroll rather than recomputing it, so the one
// thing it can still insist on is that the figures agree with themselves.
// Storing totals that contradict their own components makes a payslip
// unexplainable later, and "the other engine said so" is not an explanation.
func TestExternalPayslipBalanceCheck(t *testing.T) {
	base := ExternalPayslipInput{
		EmployeeNo:      "EMP 00204",
		EarnedBasic:     1_000_000,
		Allowances:      200_000,
		Gross:           1_200_000,
		TotalDeductions: 300_000,
		Net:             900_000,
	}

	if !base.balanced() {
		t.Fatal("a payslip whose own arithmetic is correct was rejected")
	}

	t.Run("gross must equal earned basic plus allowances", func(t *testing.T) {
		bad := base
		bad.Gross = 1_500_000 // does not match 1,000,000 + 200,000
		if bad.balanced() {
			t.Fatal("accepted a gross that does not match its components")
		}
	})

	t.Run("net must equal gross less deductions", func(t *testing.T) {
		bad := base
		bad.Net = 1_100_000 // does not match 1,200,000 - 300,000
		if bad.balanced() {
			t.Fatal("accepted a net that does not match gross less deductions")
		}
	})

	t.Run("tolerates a rounding artefact", func(t *testing.T) {
		// Both ends round in floating point. Refusing a whole payroll over one
		// cent would be its own kind of wrong.
		ok := base
		ok.Net = 900_000.01
		if !ok.balanced() {
			t.Fatal("rejected a payslip over a rounding artefact")
		}
	})

	t.Run("rejects a difference too large to be rounding", func(t *testing.T) {
		bad := base
		bad.Net = 900_001
		if bad.balanced() {
			t.Fatal("accepted a one-unit discrepancy as rounding")
		}
	})
}

// The figures the caller sent are the ones stored: this service does not
// recompute an external payroll, so a mapping that dropped or altered a field
// would silently change what somebody is paid.
func TestExternalPayslipCarriesEveryFigure(t *testing.T) {
	in := ExternalPayslipInput{
		EmployeeNo:      "EMP 00204",
		EmployeeName:    "Grace Namukasa",
		DepartmentCode:  "OPS",
		Currency:        "UGX",
		Basic:           1_000_000,
		EarnedBasic:     900_000,
		Allowances:      100_000,
		Gross:           1_000_000,
		TaxableGross:    950_000,
		PAYE:            120_000,
		NSSFEmployee:    50_000,
		NSSFEmployer:    100_000,
		OtherDeductions: 30_000,
		TotalDeductions: 200_000,
		Net:             800_000,
		WorkingDays:     26,
		UnpaidLeaveDays: 1,
		Lines:           []PayslipLine{{ComponentCode: "HOUSING", Kind: "earning", Amount: 100_000}},
	}

	got := in.computed()

	for _, c := range []struct {
		field string
		want  float64
		have  float64
	}{
		{"basic", in.Basic, got.Basic},
		{"earned_basic", in.EarnedBasic, got.EarnedBasic},
		{"allowances", in.Allowances, got.Allowances},
		{"gross", in.Gross, got.Gross},
		{"taxable_gross", in.TaxableGross, got.TaxableGross},
		{"paye", in.PAYE, got.PAYE},
		{"nssf_employee", in.NSSFEmployee, got.NSSFEmployee},
		{"nssf_employer", in.NSSFEmployer, got.NSSFEmployer},
		{"other_deductions", in.OtherDeductions, got.OtherDeductions},
		{"total_deductions", in.TotalDeductions, got.TotalDeductions},
		{"net", in.Net, got.Net},
		{"working_days", in.WorkingDays, got.WorkingDays},
		{"unpaid_leave_days", in.UnpaidLeaveDays, got.UnpaidLeaveDays},
	} {
		if c.have != c.want {
			t.Errorf("%s: stored %v, was sent %v", c.field, c.have, c.want)
		}
	}
	if got.EmployeeNo != in.EmployeeNo || got.EmployeeName != in.EmployeeName {
		t.Error("employee identity was not carried through")
	}
	if len(got.Lines) != 1 || got.Lines[0].ComponentCode != "HOUSING" {
		t.Error("payslip lines were dropped")
	}
}
