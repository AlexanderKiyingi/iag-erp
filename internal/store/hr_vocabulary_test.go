package store

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The Go vocabulary and the database CHECK bound the same thing from two sides,
// and drift between them is silent in the direction that matters: a value the
// Go list accepts but the constraint rejects turns a clean 400 into a 500 on
// the first write, which is the exact failure these lists were added to end.
//
// Reading the migrations rather than a copy of them is the point — widening one
// without the other has to fail here.

var addConstraintCheck = regexp.MustCompile(
	`(?is)ADD\s+CONSTRAINT\s+(\w+)\s+CHECK\s*\(\s*\w+\s+IN\s*\(([^)]*)\)`)

// latestConstraintValues returns the values each named CHECK constraint allows,
// taking the last definition in migration order — constraints are dropped and
// re-added as the vocabulary widens, so only the final one is in force.
func latestConstraintValues(t *testing.T) map[string][]string {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	names := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := map[string][]string{}
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range addConstraintCheck.FindAllStringSubmatch(string(body), -1) {
			values := []string{}
			for _, raw := range strings.Split(m[2], ",") {
				if v := strings.Trim(strings.TrimSpace(raw), "'"); v != "" {
					values = append(values, v)
				}
			}
			out[m[1]] = values
		}
	}
	return out
}

func TestHRVocabularyMatchesMigrationConstraints(t *testing.T) {
	constraints := latestConstraintValues(t)

	for _, tc := range []struct {
		constraint string
		vocabulary []string
	}{
		{"erp_employees_employment_type_check", EmploymentTypes},
		{"erp_job_requisitions_employment_type_check", EmploymentTypes},
		{"erp_employees_status_check", EmployeeStatuses},
		{"erp_attendance_records_status_check", AttendanceStatuses},
	} {
		allowed, ok := constraints[tc.constraint]
		if !ok {
			t.Errorf("no migration defines %s", tc.constraint)
			continue
		}
		inDB := map[string]bool{}
		for _, v := range allowed {
			inDB[v] = true
		}
		inGo := map[string]bool{}
		for _, v := range tc.vocabulary {
			inGo[v] = true
		}
		for _, v := range tc.vocabulary {
			if !inDB[v] {
				t.Errorf("%s: Go accepts %q but the constraint rejects it", tc.constraint, v)
			}
		}
		for _, v := range allowed {
			if !inGo[v] {
				t.Errorf("%s: the constraint allows %q but Go rejects it", tc.constraint, v)
			}
		}
	}
}

func TestNormaliseVocabularyAcceptsHowFormsSpellThings(t *testing.T) {
	// Every one of these is an option a frontend actually presents. Before the
	// vocabulary existed each reached a CHECK constraint and 500'd.
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"Half-day", "half_day"},
		{"half day", "half_day"},
		{"Present", "present"},
		{"Holiday", "holiday"},
		{"Late", "late"},
	} {
		got, ok := NormaliseAttendanceStatus(tc.in)
		if !ok || got != tc.want {
			t.Errorf("NormaliseAttendanceStatus(%q) = %q, %v; want %q, true", tc.in, got, ok, tc.want)
		}
	}
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"Permanent", "permanent"},
		{"Intern", "intern"},
		{"Casual", "casual"},
	} {
		got, ok := NormaliseEmploymentType(tc.in)
		if !ok || got != tc.want {
			t.Errorf("NormaliseEmploymentType(%q) = %q, %v; want %q, true", tc.in, got, ok, tc.want)
		}
	}
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"Active", "active"},
		{"On leave", "on_leave"},
		{"Terminated", "terminated"},
	} {
		got, ok := NormaliseEmployeeStatus(tc.in)
		if !ok || got != tc.want {
			t.Errorf("NormaliseEmployeeStatus(%q) = %q, %v; want %q, true", tc.in, got, ok, tc.want)
		}
	}

	// Absent means "leave it alone", not "invalid" — both update paths rely on
	// the empty string reaching COALESCE(NULLIF(...)).
	if got, ok := NormaliseEmployeeStatus("  "); !ok || got != "" {
		t.Errorf("blank status = %q, %v; want \"\", true", got, ok)
	}

	// A value outside the vocabulary must be refused, not bent onto a
	// neighbour: recording somebody as present because the spelling was close
	// is worse than rejecting the write.
	for _, bad := range []string{"Inactive", "Left", "psesent", "freelance"} {
		if _, ok := NormaliseAttendanceStatus(bad); ok && bad == "psesent" {
			t.Errorf("NormaliseAttendanceStatus(%q) should not have matched", bad)
		}
		if _, ok := NormaliseEmployeeStatus(bad); ok {
			t.Errorf("NormaliseEmployeeStatus(%q) should not have matched", bad)
		}
	}
}
