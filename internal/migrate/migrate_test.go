package migrate

import (
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"iag-erp/backend/migrations"
)

// Migration checks that do not need a database.
//
// They cannot prove the SQL runs — only Postgres can say that. What they can
// prove is the things that break silently: a version that sorts out of order,
// and seeded tax figures drifting away from the arithmetic that is unit-tested
// against them.

func migrationFiles(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no migrations are embedded")
	}
	return files
}

// Migrations are applied in lexical filename order and recorded under that
// name. A prefix that sorts out of numeric order would run before the migration
// it depends on — and having already been recorded, would never be retried.
func TestMigrationOrderIsNumeric(t *testing.T) {
	prefix := regexp.MustCompile(`^(\d+)_`)
	last := -1
	for _, name := range migrationFiles(t) {
		m := prefix.FindStringSubmatch(name)
		if m == nil {
			t.Errorf("%s does not start with a numeric version prefix", name)
			continue
		}
		if len(m[1]) != 3 {
			t.Errorf("%s uses a %d-digit prefix; all migrations must use 3 digits or lexical "+
				"order stops matching numeric order", name, len(m[1]))
		}
		n, _ := strconv.Atoi(m[1])
		if n < last {
			t.Errorf("%s sorts after version %03d but is numerically lower", name, last)
		}
		last = n
	}
}

// Migrations must run after the schema they extend.
func TestMigrationDependencyOrder(t *testing.T) {
	index := map[string]int{}
	for i, name := range migrationFiles(t) {
		index[name] = i
	}
	for _, required := range []string{
		"002_hr_schema.sql",
		"005_employee_compensation.sql",
		"008_setup_worksites.sql",
		"009_work_calendar.sql",
		"010_payroll.sql",
		"011_hr_lifecycle.sql",
	} {
		if _, ok := index[required]; !ok {
			t.Fatalf("%s is not embedded", required)
		}
	}
	// 009 indexes and seeds erp_setup_items (008) and alters erp_leave_requests (002).
	if index["009_work_calendar.sql"] < index["008_setup_worksites.sql"] {
		t.Error("009 runs before the setup-items table it indexes and seeds")
	}
	// 010 references erp_employees (002) and erp_employee_compensation (005).
	if index["010_payroll.sql"] < index["005_employee_compensation.sql"] {
		t.Error("010 runs before the compensation table payroll reads")
	}
	// 011 references erp_employees and erp_departments (002).
	if index["011_hr_lifecycle.sql"] < index["002_hr_schema.sql"] {
		t.Error("011 runs before the employee and department tables it references")
	}
}

// Every state the lifecycle tables accept must be one the Go state machines
// know, and vice versa. A CHECK constraint and a transition table that disagree
// produce a move the code permits and the database rejects — a 500 on a valid
// workflow step, discoverable only in production.
func TestLifecycleCheckConstraintsMatchStateMachines(t *testing.T) {
	body, err := migrations.FS.ReadFile("011_hr_lifecycle.sql")
	if err != nil {
		t.Fatalf("read 011_hr_lifecycle.sql: %v", err)
	}
	sql := string(body)

	// The state vocabularies, mirrored here rather than imported: the store
	// package imports migrations, so reading it from here would be a cycle.
	// Mirroring is the point — the copy has to be updated deliberately, and
	// this test fails if the SQL moves without it.
	for _, tc := range []struct {
		name   string
		column string
		states []string
	}{
		{"application stage", "stage",
			[]string{"applied", "screening", "interview", "offer", "hired", "rejected", "withdrawn"}},
		{"requisition status", "status",
			[]string{"draft", "pending_approval", "open", "on_hold", "filled", "cancelled"}},
		{"offer status", "status",
			[]string{"draft", "sent", "accepted", "declined", "withdrawn", "expired"}},
		{"checklist status", "status",
			[]string{"open", "completed", "cancelled"}},
		{"checklist item status", "status",
			[]string{"pending", "done", "not_applicable", "blocked"}},
		{"review cycle status", "status",
			[]string{"draft", "open", "closed"}},
		{"review status", "status",
			[]string{"draft", "self_review", "manager_review", "calibration", "shared", "acknowledged", "cancelled"}},
		{"goal status", "status",
			[]string{"draft", "active", "achieved", "missed", "cancelled"}},
		{"disciplinary status", "status",
			[]string{"reported", "under_investigation", "hearing_scheduled", "decided", "appealed", "closed", "dismissed"}},
		{"enrolment status", "status",
			[]string{"enrolled", "in_progress", "completed", "failed", "cancelled"}},
	} {
		want := map[string][]string{}
		for _, s := range tc.states {
			want[s] = nil
		}
		if !checkConstraintCovers(sql, tc.column, want) {
			t.Errorf("no CHECK (%s IN ...) in migration 011 matches the %s state machine (%v)",
				tc.column, tc.name, tc.states)
		}
	}
}

// checkConstraintCovers finds a `CHECK (<column> IN (...))` in the SQL and
// reports whether its vocabulary equals the machine's.
func checkConstraintCovers(sql, column string, states map[string][]string) bool {
	re := regexp.MustCompile(`CHECK \(` + column + ` IN \(([^)]*)\)\)`)
	for _, m := range re.FindAllStringSubmatch(sql, -1) {
		listed := map[string]bool{}
		for _, raw := range strings.Split(m[1], ",") {
			v := strings.Trim(strings.TrimSpace(raw), "'")
			if v != "" {
				listed[v] = true
			}
		}
		if len(listed) != len(states) {
			continue
		}
		all := true
		for s := range states {
			if !listed[s] {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// seededPAYEBands pulls the UG_PAYE_MONTHLY VALUES block out of migration 010.
//
// The payroll arithmetic is unit-tested against a restated copy of this scale.
// Reading the real one here is what stops the two drifting apart: an edit to the
// seed that nobody mirrors into the arithmetic tests fails on the next run.
func seededPAYEBands(t *testing.T) [][5]float64 {
	t.Helper()
	body, err := migrations.FS.ReadFile("010_payroll.sql")
	if err != nil {
		t.Fatalf("read 010_payroll.sql: %v", err)
	}
	// (seq, lower, upper|NULL, rate, base_tax)
	row := regexp.MustCompile(`\(\s*(\d+),\s*([\d.]+),\s*(NULL|[\d.]+),\s*([\d.]+),\s*([\d.]+)\s*\)`)
	matches := row.FindAllStringSubmatch(string(body), -1)

	var out [][5]float64
	for _, m := range matches {
		upper := -1.0 // -1 stands for NULL: unbounded above
		if m[3] != "NULL" {
			upper, _ = strconv.ParseFloat(m[3], 64)
		}
		seq, _ := strconv.ParseFloat(m[1], 64)
		lower, _ := strconv.ParseFloat(m[2], 64)
		rate, _ := strconv.ParseFloat(m[4], 64)
		base, _ := strconv.ParseFloat(m[5], 64)
		out = append(out, [5]float64{seq, lower, upper, rate, base})
	}
	return out
}

func TestSeededPAYEBandsAreContinuous(t *testing.T) {
	bands := seededPAYEBands(t)
	if len(bands) != 5 {
		t.Fatalf("parsed %d PAYE bands from migration 010, want 5", len(bands))
	}

	// The scale must start at zero, or income below the first band is untaxed
	// by accident rather than by policy.
	if bands[0][1] != 0 {
		t.Errorf("first band starts at %.2f, want 0", bands[0][1])
	}
	// Exactly one unbounded band, and it must be the last.
	for i, b := range bands {
		if b[2] == -1 && i != len(bands)-1 {
			t.Errorf("band %d is unbounded but is not the last", int(b[0]))
		}
	}
	if bands[len(bands)-1][2] != -1 {
		t.Error("the top band is bounded; income above it would be untaxed")
	}

	for i := 1; i < len(bands); i++ {
		prev, cur := bands[i-1], bands[i]
		if prev[2] != cur[1] {
			t.Errorf("gap or overlap between band %d and %d: %.2f vs %.2f",
				int(prev[0]), int(cur[0]), prev[2], cur[1])
		}
		// base_tax must be the cumulative tax at the band's lower bound.
		want := prev[4] + prev[3]*(prev[2]-prev[1])
		if diff := want - cur[4]; diff > 0.01 || diff < -0.01 {
			t.Errorf("band %d base_tax = %.2f, but tax charged below it is %.2f",
				int(cur[0]), cur[4], want)
		}
	}
}

// The seeded NSSF split must stay the statutory 5% employee / 10% employer.
// A silent edit here changes every net-pay figure the service produces.
func TestSeededNSSFRates(t *testing.T) {
	body, err := migrations.FS.ReadFile("010_payroll.sql")
	if err != nil {
		t.Fatalf("read 010_payroll.sql: %v", err)
	}
	nssf := regexp.MustCompile(`'NSSF',\s*'[^']*',\s*'[\d-]+'::date,\s*([\d.]+),\s*([\d.]+)`)
	m := nssf.FindStringSubmatch(string(body))
	if m == nil {
		t.Fatal("no NSSF rate seed found in migration 010")
	}
	employee, _ := strconv.ParseFloat(m[1], 64)
	employer, _ := strconv.ParseFloat(m[2], 64)
	if employee != 0.05 {
		t.Errorf("NSSF employee rate seeded as %.4f, want 0.0500", employee)
	}
	if employer != 0.10 {
		t.Errorf("NSSF employer rate seeded as %.4f, want 0.1000", employer)
	}
}
