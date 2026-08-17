package store

import (
	"math"
	"testing"
)

// ugBands is the seeded Uganda monthly PAYE scale (migration 010). The tests
// restate it rather than reading the database so a change to the seeded figures
// shows up here as a deliberate edit rather than as a silently passing suite.
func ugBands() []TaxBand {
	f := func(v float64) *float64 { return &v }
	return []TaxBand{
		{Seq: 1, LowerBound: 0, UpperBound: f(235000), Rate: 0.00, BaseTax: 0},
		{Seq: 2, LowerBound: 235000, UpperBound: f(335000), Rate: 0.10, BaseTax: 0},
		{Seq: 3, LowerBound: 335000, UpperBound: f(410000), Rate: 0.20, BaseTax: 10000},
		{Seq: 4, LowerBound: 410000, UpperBound: f(10000000), Rate: 0.30, BaseTax: 25000},
		{Seq: 5, LowerBound: 10000000, UpperBound: nil, Rate: 0.40, BaseTax: 2902000},
	}
}

func TestComputePAYEBands(t *testing.T) {
	bands := ugBands()
	cases := []struct {
		name       string
		chargeable float64
		want       float64
	}{
		{"below threshold", 200000, 0},
		{"at threshold", 235000, 0},
		{"first taxed band", 300000, 6500},          // 10% of 65,000
		{"top of first taxed band", 335000, 10000},  // 10% of 100,000
		{"second taxed band", 400000, 23000},        // 10,000 + 20% of 65,000
		{"top of second taxed band", 410000, 25000}, // 10,000 + 20% of 75,000
		{"third band", 1000000, 202000},             // 25,000 + 30% of 590,000
		{"top of third band", 10000000, 2902000},    // 25,000 + 30% of 9,590,000
		{"additional rate band", 12000000, 3702000}, // 2,902,000 + 40% of 2,000,000
		{"zero", 0, 0},
		{"negative", -5000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputePAYE(tc.chargeable, bands); got != tc.want {
				t.Fatalf("ComputePAYE(%.2f) = %.2f, want %.2f", tc.chargeable, got, tc.want)
			}
		})
	}
}

// The cumulative base of each band must equal the tax charged on everything
// below it, or the scale double-counts or skips a slice of income at the seam.
func TestPAYEBandsAreContinuous(t *testing.T) {
	bands := ugBands()
	for i := 1; i < len(bands); i++ {
		prev, cur := bands[i-1], bands[i]
		if prev.UpperBound == nil {
			t.Fatalf("band %d is unbounded but is not the last", prev.Seq)
		}
		if *prev.UpperBound != cur.LowerBound {
			t.Fatalf("gap between band %d and %d: %.2f vs %.2f",
				prev.Seq, cur.Seq, *prev.UpperBound, cur.LowerBound)
		}
		wantBase := prev.BaseTax + prev.Rate*(*prev.UpperBound-prev.LowerBound)
		if math.Abs(wantBase-cur.BaseTax) > 0.01 {
			t.Fatalf("band %d base_tax = %.2f, but tax below it is %.2f",
				cur.Seq, cur.BaseTax, wantBase)
		}
	}
}

func TestComputePAYENoBands(t *testing.T) {
	if got := ComputePAYE(1000000, nil); got != 0 {
		t.Fatalf("with no bands loaded PAYE = %.2f, want 0", got)
	}
}

func TestContributionOnCap(t *testing.T) {
	cap := 500000.0
	r := StatutoryRate{Code: "NSSF", EmployeeRate: 0.05, EmployerRate: 0.10, MonthlyCap: &cap}
	employee, employer := r.ContributionOn(800000)
	if employee != 25000 || employer != 50000 {
		t.Fatalf("capped contribution = (%.2f, %.2f), want (25000, 50000)", employee, employer)
	}

	uncapped := StatutoryRate{Code: "NSSF", EmployeeRate: 0.05, EmployerRate: 0.10}
	employee, employer = uncapped.ContributionOn(800000)
	if employee != 40000 || employer != 80000 {
		t.Fatalf("uncapped contribution = (%.2f, %.2f), want (40000, 80000)", employee, employer)
	}
}

func nssf() []StatutoryRate {
	return []StatutoryRate{{Code: "NSSF", Name: "NSSF", EmployeeRate: 0.05, EmployerRate: 0.10}}
}

func TestComputePayslipSalaryOnly(t *testing.T) {
	slip := ComputePayslip(PayslipInput{
		EmployeeNo:   "EMP-001",
		MonthlyBasic: 1000000,
		WorkingDays:  22,
		Bands:        ugBands(),
		Statutory:    nssf(),
	})

	if slip.Gross != 1000000 {
		t.Fatalf("gross = %.2f, want 1000000", slip.Gross)
	}
	if slip.PAYE != 202000 {
		t.Fatalf("paye = %.2f, want 202000", slip.PAYE)
	}
	if slip.NSSFEmployee != 50000 || slip.NSSFEmployer != 100000 {
		t.Fatalf("nssf = (%.2f, %.2f), want (50000, 100000)", slip.NSSFEmployee, slip.NSSFEmployer)
	}
	if slip.Net != 748000 { // 1,000,000 − 202,000 − 50,000
		t.Fatalf("net = %.2f, want 748000", slip.Net)
	}
	// The employer's share is a cost, not a deduction: it must not reduce net.
	if slip.TotalDeductions != slip.PAYE+slip.NSSFEmployee {
		t.Fatalf("employer NSSF leaked into deductions: %.2f", slip.TotalDeductions)
	}
}

func TestComputePayslipNonTaxableAllowanceIsExcludedFromPAYE(t *testing.T) {
	withTaxable := ComputePayslip(PayslipInput{
		MonthlyBasic: 1000000,
		WorkingDays:  22,
		Components: []PayComponent{
			{Code: "HOUSING", Name: "Housing", Kind: "earning", Amount: 200000, Taxable: true, Pensionable: true},
		},
		Bands:     ugBands(),
		Statutory: nssf(),
	})
	withExempt := ComputePayslip(PayslipInput{
		MonthlyBasic: 1000000,
		WorkingDays:  22,
		Components: []PayComponent{
			{Code: "AIRTIME", Name: "Airtime", Kind: "earning", Amount: 200000, Taxable: false, Pensionable: false},
		},
		Bands:     ugBands(),
		Statutory: nssf(),
	})

	if withTaxable.Gross != withExempt.Gross {
		t.Fatalf("gross differs: %.2f vs %.2f", withTaxable.Gross, withExempt.Gross)
	}
	if withExempt.TaxableGross != 1000000 {
		t.Fatalf("exempt allowance entered taxable gross: %.2f", withExempt.TaxableGross)
	}
	if withTaxable.PAYE <= withExempt.PAYE {
		t.Fatalf("taxable allowance did not raise PAYE: %.2f vs %.2f", withTaxable.PAYE, withExempt.PAYE)
	}
	// Non-pensionable means it is outside NSSF too.
	if withExempt.NSSFEmployee != 50000 {
		t.Fatalf("non-pensionable allowance entered NSSF: %.2f", withExempt.NSSFEmployee)
	}
}

func TestComputePayslipUnpaidLeaveReducesPay(t *testing.T) {
	full := ComputePayslip(PayslipInput{
		MonthlyBasic: 2200000, WorkingDays: 22,
		Bands: ugBands(), Statutory: nssf(),
	})
	withUnpaid := ComputePayslip(PayslipInput{
		MonthlyBasic: 2200000, WorkingDays: 22, UnpaidLeaveDays: 2,
		Bands: ugBands(), Statutory: nssf(),
	})

	if withUnpaid.EarnedBasic != 2000000 { // 2,200,000 x 20/22
		t.Fatalf("earned basic = %.2f, want 2000000", withUnpaid.EarnedBasic)
	}
	if withUnpaid.Basic != 2200000 {
		t.Fatalf("contracted basic should be unchanged, got %.2f", withUnpaid.Basic)
	}
	if withUnpaid.Net >= full.Net {
		t.Fatalf("unpaid leave did not reduce net: %.2f vs %.2f", withUnpaid.Net, full.Net)
	}
	// Tax follows the reduced figure, not the contracted one.
	if withUnpaid.TaxableGross != 2000000 {
		t.Fatalf("taxable gross = %.2f, want 2000000", withUnpaid.TaxableGross)
	}
}

// Unpaid leave longer than the month must not turn the basic negative.
func TestComputePayslipUnpaidLeaveClampedToPeriod(t *testing.T) {
	slip := ComputePayslip(PayslipInput{
		MonthlyBasic: 1000000, WorkingDays: 22, UnpaidLeaveDays: 40,
		Bands: ugBands(), Statutory: nssf(),
	})
	if slip.EarnedBasic != 0 {
		t.Fatalf("earned basic = %.2f, want 0", slip.EarnedBasic)
	}
	if slip.Net < 0 {
		t.Fatalf("net went negative: %.2f", slip.Net)
	}
}

func TestComputePayslipDeductionsAndBalance(t *testing.T) {
	slip := ComputePayslip(PayslipInput{
		MonthlyBasic: 1500000,
		WorkingDays:  22,
		Components: []PayComponent{
			{Code: "TRANSPORT", Name: "Transport", Kind: "earning", Amount: 150000, Taxable: true, Pensionable: true},
			{Code: "LOAN", Name: "Staff loan", Kind: "deduction", Amount: 100000},
		},
		Bands:     ugBands(),
		Statutory: nssf(),
	})

	if slip.Gross != 1650000 {
		t.Fatalf("gross = %.2f, want 1650000", slip.Gross)
	}
	// The identity the whole payslip has to satisfy.
	want := slip.Gross - slip.TotalDeductions
	if math.Abs(slip.Net-want) > 0.01 {
		t.Fatalf("net %.2f != gross − deductions %.2f", slip.Net, want)
	}
	if math.Abs(slip.TotalDeductions-(slip.PAYE+slip.NSSFEmployee+slip.OtherDeductions)) > 0.01 {
		t.Fatalf("deduction total does not equal its parts: %.2f", slip.TotalDeductions)
	}
	// Every figure on the slip should be explained by a line.
	var earnings, deductions float64
	for _, l := range slip.Lines {
		switch l.Kind {
		case "earning":
			earnings += l.Amount
		case "deduction", "statutory":
			deductions += l.Amount
		}
	}
	if math.Abs(earnings-slip.Gross) > 0.01 {
		t.Fatalf("earning lines sum to %.2f, gross is %.2f", earnings, slip.Gross)
	}
	if math.Abs(deductions-slip.TotalDeductions) > 0.01 {
		t.Fatalf("deduction lines sum to %.2f, total is %.2f", deductions, slip.TotalDeductions)
	}
}

func TestPeriodBounds(t *testing.T) {
	start, end, err := PeriodBounds("2026-02")
	if err != nil {
		t.Fatalf("PeriodBounds: %v", err)
	}
	if start.Format("2006-01-02") != "2026-02-01" {
		t.Fatalf("start = %s", start.Format("2006-01-02"))
	}
	if end.Format("2006-01-02") != "2026-02-28" {
		t.Fatalf("end = %s, want the last day of February", end.Format("2006-01-02"))
	}
	if _, _, err := PeriodBounds("February 2026"); err == nil {
		t.Fatal("expected an error for a non-YYYY-MM period")
	}
}
