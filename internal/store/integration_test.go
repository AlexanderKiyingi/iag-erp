package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"iag-erp/backend/internal/migrate"
)

// Regression tests for defects that only a database can catch.
//
// Three fixes in this package live entirely in SQL — a join, a conditional
// balance check, and a date calculation — so no amount of pure-Go testing
// proves them. These do, against a real Postgres.
//
// They are skipped unless TEST_DATABASE_URL is set. That variable is
// deliberately not DATABASE_URL: these tests write and delete rows, and a
// developer with a service DSN already exported should not have their data
// touched because they ran `go test ./...`.
//
//	TEST_DATABASE_URL='postgres://user:pass@localhost:5432/iag_test?sslmode=disable' go test ./internal/store/
//
// Every test namespaces its rows with a unique prefix and removes them
// afterwards, so a shared database is safe and runs do not collide.

var (
	testPoolOnce sync.Once
	testPool     *pgxpool.Pool
	testPoolErr  error
)

// testStore returns a Store against TEST_DATABASE_URL, skipping if unset.
//
// Migrations run once per process: they are idempotent, but running them per
// test would dominate the runtime.
func testStore(t *testing.T) *Store {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database-backed regression tests")
	}

	testPoolOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testPool, testPoolErr = pgxpool.New(ctx, dsn)
		if testPoolErr != nil {
			return
		}
		if testPoolErr = testPool.Ping(ctx); testPoolErr != nil {
			return
		}
		// The schema under test includes migrations 009-011, so applying them
		// is part of the fixture rather than a separate manual step.
		testPoolErr = migrate.Up(ctx, testPool)
	})
	if testPoolErr != nil {
		t.Fatalf("TEST_DATABASE_URL is set but unusable: %v", testPoolErr)
	}

	s := New(testPool)
	s.SetWorkWeek(DefaultWorkWeek())
	return s
}

// fixture namespaces one test's rows and cleans them up.
type fixture struct {
	store  *Store
	prefix string
	t      *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s := testStore(t)
	// Unique per test, so parallel runs and repeated runs never collide.
	f := &fixture{store: s, prefix: "T" + strings.ToUpper(uuid.NewString()[:8]), t: t}
	t.Cleanup(f.cleanup)
	return f
}

// cleanup removes this test's rows, children first.
func (f *fixture) cleanup() {
	ctx := context.Background()
	pool := f.store.pool
	like := f.prefix + "%"

	// Leave requests and attendance reference employees; employees reference
	// departments. Deleting in that order avoids relying on cascade rules that
	// are not all declared.
	for _, q := range []string{
		`DELETE FROM erp_leave_requests WHERE employee_id IN
		   (SELECT id FROM erp_employees WHERE employee_no LIKE $1)`,
		`DELETE FROM erp_attendance_records WHERE employee_id IN
		   (SELECT id FROM erp_employees WHERE employee_no LIKE $1)`,
		`DELETE FROM erp_leave_balances WHERE employee_id IN
		   (SELECT id FROM erp_employees WHERE employee_no LIKE $1)`,
		`DELETE FROM erp_employees WHERE employee_no LIKE $1`,
		`DELETE FROM erp_departments WHERE code LIKE $1`,
		`DELETE FROM erp_setup_items WHERE code LIKE $1`,
	} {
		if _, err := pool.Exec(ctx, q, like); err != nil {
			f.t.Errorf("cleanup %q: %v", strings.Fields(q)[2], err)
		}
	}
}

func (f *fixture) employeeNo(suffix string) string { return f.prefix + "-" + suffix }

// newEmployee creates an employee. departmentCode may be empty, which is the
// case the leave-list regression is about.
func (f *fixture) newEmployee(suffix, departmentCode string) *Employee {
	f.t.Helper()
	emp, err := f.store.CreateEmployee(context.Background(), CreateEmployeeInput{
		EmployeeNo:     f.employeeNo(suffix),
		FirstName:      "Test",
		LastName:       suffix,
		DepartmentCode: departmentCode,
		HireDate:       "2020-01-01",
	})
	if err != nil {
		f.t.Fatalf("create employee %s: %v", suffix, err)
	}
	return emp
}

// ---------------------------------------------------------------------------
// Regression: leave of an employee with no department was invisible
// ---------------------------------------------------------------------------

// ListLeaveRequests joined erp_departments with an inner join, so any employee
// who had not been assigned a department had their leave silently omitted —
// from the list, and from the approval queue built on it. Their request existed
// and could not be seen, so it was never approved.
func TestListLeaveRequestsIncludesEmployeeWithNoDepartment(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// The employee at the centre of the bug: no department.
	orphan := f.newEmployee("ORPHAN", "")
	if orphan.DepartmentCode != nil {
		t.Fatalf("fixture is wrong: employee has department %q", *orphan.DepartmentCode)
	}

	created, err := f.store.CreateLeaveRequest(ctx, CreateLeaveRequestInput{
		EmployeeNo:    orphan.EmployeeNo,
		LeaveTypeCode: "ANNUAL",
		StartsOn:      "2026-09-07", // Monday
		EndsOn:        "2026-09-08", // Tuesday
		Reason:        "regression fixture",
	})
	if err != nil {
		t.Fatalf("create leave request: %v", err)
	}

	items, err := f.store.ListLeaveRequests(ctx, ListLeaveRequestsFilter{Limit: 200})
	if err != nil {
		t.Fatalf("list leave requests: %v", err)
	}
	for _, lr := range items {
		if lr.ID == created.ID {
			return // present, as it must be
		}
	}
	t.Fatalf("leave request %s for department-less employee %s is missing from the list "+
		"of %d — the department join has regressed to an inner join",
		created.ID, orphan.EmployeeNo, len(items))
}

// ---------------------------------------------------------------------------
// Regression: unpaid leave could never be filed
// ---------------------------------------------------------------------------

// Unpaid leave types carry days_per_year = 0, and the balance check compared
// the request against that, so every unpaid request was rejected as exceeding
// a zero entitlement. Unpaid leave has no entitlement to exhaust; it is also
// the absence payroll pro-rates pay from, so being unable to record it made
// that feature unreachable.
func TestCreateUnpaidLeaveRequestIsNotBalanceChecked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	emp := f.newEmployee("UNPAID", "")

	var paid bool
	if err := f.store.pool.QueryRow(ctx,
		`SELECT paid FROM erp_leave_types WHERE code = 'UNPAID'`).Scan(&paid); err != nil {
		t.Fatalf("read UNPAID leave type (is migration 003 applied?): %v", err)
	}
	if paid {
		t.Skip("UNPAID leave type is configured as paid in this database; the premise does not hold")
	}

	got, err := f.store.CreateLeaveRequest(ctx, CreateLeaveRequestInput{
		EmployeeNo:    emp.EmployeeNo,
		LeaveTypeCode: "UNPAID",
		StartsOn:      "2026-09-14", // Monday
		EndsOn:        "2026-09-16", // Wednesday
		Reason:        "regression fixture",
	})
	if err != nil {
		t.Fatalf("unpaid leave was rejected: %v — unpaid leave has no entitlement to "+
			"check against and must not be balance-checked", err)
	}
	if got.Days != 3 {
		t.Fatalf("unpaid leave days = %.2f, want 3", got.Days)
	}

	// Paid leave must still be checked, or the fix has removed the control
	// rather than narrowed it to unpaid types.
	//
	// This uses a second employee deliberately. Asking the same one would
	// overlap the unpaid request above and be refused as a conflict — the test
	// would still pass, while proving nothing about the balance check.
	other := f.newEmployee("PAIDCHECK", "")
	_, err = f.store.CreateLeaveRequest(ctx, CreateLeaveRequestInput{
		EmployeeNo:    other.EmployeeNo,
		LeaveTypeCode: "ANNUAL",
		StartsOn:      "2026-01-05",
		EndsOn:        "2026-12-31",
		Reason:        "should exceed entitlement",
	})
	if !errors.Is(err, ErrInsufficientLeave) {
		t.Fatalf("a paid request far exceeding entitlement returned %v, want "+
			"ErrInsufficientLeave; the balance check has been removed rather than "+
			"skipped for unpaid types", err)
	}
}

// ---------------------------------------------------------------------------
// Regression: leave was charged in calendar days
// ---------------------------------------------------------------------------

// days was (end - start) + 1, so a Friday-to-Monday request cost four days
// instead of two. That figure is multiplied by a daily rate and posted to
// finance as an accrued-leave liability, so the error carried into money.
func TestLeaveIsChargedInWorkingDays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	emp := f.newEmployee("CALENDAR", "")

	// 2026-09-04 is a Friday, 2026-09-07 the following Monday: four calendar
	// days spanning a weekend, two working days.
	got, err := f.store.CreateLeaveRequest(ctx, CreateLeaveRequestInput{
		EmployeeNo:    emp.EmployeeNo,
		LeaveTypeCode: "ANNUAL",
		StartsOn:      "2026-09-04",
		EndsOn:        "2026-09-07",
		Reason:        "regression fixture",
	})
	if err != nil {
		t.Fatalf("create leave request: %v", err)
	}
	if got.Days != 2 {
		t.Errorf("days = %.2f, want 2 — the weekend is being charged as leave", got.Days)
	}

	var calendarDays float64
	if err := f.store.pool.QueryRow(ctx,
		`SELECT calendar_days FROM erp_leave_requests WHERE id = $1`, got.ID).Scan(&calendarDays); err != nil {
		t.Fatalf("read calendar_days: %v", err)
	}
	if calendarDays != 4 {
		t.Errorf("calendar_days = %.2f, want 4 — the span is not being recorded", calendarDays)
	}
}

// A public holiday inside the range is not leave either. This also proves the
// 'holiday' setup-item type is actually read, which it was not before.
func TestPublicHolidayIsNotChargedAsLeave(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	emp := f.newEmployee("HOLIDAY", "")

	// A Wednesday in the middle of the week under test.
	holiday := "2026-09-23"
	if _, err := f.store.CreateSetupItem(ctx, UpsertSetupInput{
		ItemType:      "holiday",
		Name:          "Regression test holiday",
		Code:          f.prefix + "-HOL",
		EffectiveDate: holiday,
	}); err != nil {
		t.Fatalf("create holiday setup item: %v", err)
	}

	// Monday 21st to Friday 25th: five working days, one of them the holiday.
	got, err := f.store.CreateLeaveRequest(ctx, CreateLeaveRequestInput{
		EmployeeNo:    emp.EmployeeNo,
		LeaveTypeCode: "ANNUAL",
		StartsOn:      "2026-09-21",
		EndsOn:        "2026-09-25",
		Reason:        "regression fixture",
	})
	if err != nil {
		t.Fatalf("create leave request: %v", err)
	}
	if got.Days != 4 {
		t.Errorf("days = %.2f, want 4 — the public holiday is being charged as leave", got.Days)
	}
}

// A range made only of non-working days is refused rather than recorded as a
// zero-day request sitting in the approval queue.
func TestWeekendOnlyLeaveIsRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	emp := f.newEmployee("WEEKEND", "")

	// 2026-09-05 and 06 are a Saturday and Sunday.
	_, err := f.store.CreateLeaveRequest(ctx, CreateLeaveRequestInput{
		EmployeeNo:    emp.EmployeeNo,
		LeaveTypeCode: "ANNUAL",
		StartsOn:      "2026-09-05",
		EndsOn:        "2026-09-06",
		Reason:        "regression fixture",
	})
	if !errors.Is(err, ErrNoWorkingDays) {
		t.Fatalf("weekend-only leave returned %v, want ErrNoWorkingDays", err)
	}
}

// ---------------------------------------------------------------------------
// Regression: the two balance definitions must agree on their inputs
// ---------------------------------------------------------------------------

// GetLeaveBalance used to return entitlement and leave the accrual fields at
// zero, while the materialised balance published a different figure to finance.
// Both are now computed here, and both must be populated and self-consistent.
func TestLeaveBalanceReportsBothBases(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	emp := f.newEmployee("BALANCE", "")

	bal, err := f.store.GetLeaveBalance(ctx, emp.EmployeeNo, "ANNUAL", time.Now().UTC().Year())
	if err != nil {
		t.Fatalf("get leave balance: %v", err)
	}

	if bal.EntitledDays <= 0 {
		t.Fatalf("entitled_days = %.2f; the ANNUAL leave type looks unseeded", bal.EntitledDays)
	}
	// The accrual side must be populated, not left at zero as it used to be.
	if bal.EarnedDays <= 0 {
		t.Errorf("earned_days = %.2f for an employee hired in 2020 — the accrual "+
			"fields are not being computed", bal.EarnedDays)
	}
	if bal.EarnedDays > bal.EntitledDays {
		t.Errorf("earned_days %.2f exceeds the annual entitlement %.2f",
			bal.EarnedDays, bal.EntitledDays)
	}

	// Both identities must hold against the same inputs.
	if want := bal.OpeningDays + bal.EntitledDays - bal.TakenDays; !approx(bal.RemainingDays, want) {
		t.Errorf("remaining_days = %.2f, want opening+entitled-taken = %.2f", bal.RemainingDays, want)
	}
	if want := bal.OpeningDays + bal.EarnedDays - bal.TakenDays; !approx(bal.BalanceDays, want) {
		t.Errorf("balance_days = %.2f, want opening+earned-taken = %.2f", bal.BalanceDays, want)
	}
	if bal.UsedDays != bal.TakenDays {
		t.Errorf("used_days %.2f and taken_days %.2f disagree", bal.UsedDays, bal.TakenDays)
	}
}

func approx(a, b float64) bool {
	d := a - b
	return d < 0.01 && d > -0.01
}

// ---------------------------------------------------------------------------
// Regression: access scope must filter, not merely annotate
// ---------------------------------------------------------------------------

// A caller with no linked employee record must see nothing. The failure this
// guards is an empty allowlist reaching the query as "no filter" and returning
// the whole roster — the exact shape of the original security gap.
func TestScopeFilterNarrowsListQueries(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	visible := f.newEmployee("INSCOPE", "")
	f.newEmployee("OUTSCOPE", "")

	scoped := AccessScope{EmployeeNo: visible.EmployeeNo, TeamNos: []string{visible.EmployeeNo}}
	items, err := f.store.ListEmployees(ctx, ListEmployeesFilter{
		Limit:                 200,
		RestrictToEmployeeNos: scoped.Filter(),
	})
	if err != nil {
		t.Fatalf("list employees: %v", err)
	}
	for _, e := range items {
		if e.EmployeeNo != visible.EmployeeNo {
			t.Fatalf("scoped list returned %s, which is outside the scope", e.EmployeeNo)
		}
	}
	if len(items) != 1 {
		t.Fatalf("scoped list returned %d employees, want exactly 1", len(items))
	}

	// The unlinked caller: must return nothing at all.
	empty, err := f.store.ListEmployees(ctx, ListEmployeesFilter{
		Limit:                 200,
		RestrictToEmployeeNos: AccessScope{}.Filter(),
	})
	if err != nil {
		t.Fatalf("list employees for unlinked caller: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("a caller with no employee record saw %d employees; an empty scope "+
			"is being read as no filter", len(empty))
	}
}

// ---------------------------------------------------------------------------
// Migrations
// ---------------------------------------------------------------------------

// The migrations must actually apply. Everything above depends on it, but a
// dedicated test names the failure rather than letting it surface as a missing
// column three tests later.
func TestMigrationsApplyAndCreateLifecycleTables(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, table := range []string{
		"erp_leave_requests", "erp_employee_compensation",
		"erp_tax_bands", "erp_statutory_rates", "erp_pay_components",
		"erp_payroll_runs", "erp_payslips", "erp_payslip_lines",
		"erp_job_requisitions", "erp_candidates", "erp_applications",
		"erp_checklist_templates", "erp_employee_checklists",
		"erp_review_cycles", "erp_performance_reviews", "erp_performance_goals",
		"erp_disciplinary_cases", "erp_training_courses", "erp_training_enrolments",
	} {
		var exists bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist after migrations", table)
		}
	}

	// calendar_days is what migration 009 adds and CreateLeaveRequest writes to;
	// deploying the code without it breaks leave filing outright.
	var hasColumn bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name = 'erp_leave_requests' AND column_name = 'calendar_days')`).
		Scan(&hasColumn); err != nil {
		t.Fatalf("check calendar_days: %v", err)
	}
	if !hasColumn {
		t.Error("erp_leave_requests.calendar_days is missing; migration 009 has not applied")
	}
}

// The seeded tax scale must load and compute, which is the one part of payroll
// that reads configuration rather than arithmetic.
func TestSeededTaxBandsLoadAndCompute(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	bands, err := s.LoadTaxBands(ctx, PayrollScheme, time.Now().UTC())
	if err != nil {
		t.Fatalf("load tax bands: %v", err)
	}
	if len(bands) == 0 {
		t.Fatalf("no %s bands are effective today; migration 010 has not seeded them", PayrollScheme)
	}
	// The same figure the pure unit test asserts, now through the database.
	if got := ComputePAYE(1000000, bands); got != 202000 {
		t.Errorf("PAYE on 1,000,000 from seeded bands = %.2f, want 202000", got)
	}

	rates, err := s.LoadStatutoryRates(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("load statutory rates: %v", err)
	}
	var found bool
	for _, r := range rates {
		if r.Code == "NSSF" {
			found = true
			employee, employer := r.ContributionOn(1000000)
			if employee != 50000 || employer != 100000 {
				t.Errorf("seeded NSSF on 1,000,000 = (%.2f, %.2f), want (50000, 100000)",
					employee, employer)
			}
		}
	}
	if !found {
		t.Error("no NSSF rate is effective today; migration 010 has not seeded it")
	}
}
