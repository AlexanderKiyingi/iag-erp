package store

import (
	"testing"
	"time"
)

func TestUnrestrictedScopeAllowsEverything(t *testing.T) {
	sc := UnrestrictedScope()
	if !sc.Allows("EMP-999") {
		t.Fatal("unrestricted scope refused an employee")
	}
	if sc.Filter() != nil {
		t.Fatal("unrestricted scope produced a list filter; the query must stay unfiltered")
	}
}

func TestScopeAllowsSelfAndTeam(t *testing.T) {
	sc := AccessScope{EmployeeNo: "EMP-005", TeamNos: []string{"EMP-005", "EMP-001", "EMP-002"}}

	for _, no := range []string{"EMP-005", "EMP-001", "EMP-002"} {
		if !sc.Allows(no) {
			t.Fatalf("scope refused %s, which is in the tree", no)
		}
	}
	if sc.Allows("EMP-999") {
		t.Fatal("scope allowed an employee outside the tree")
	}
	if sc.Allows("") {
		t.Fatal("scope allowed an empty employee number")
	}
}

// The distinction the approval path depends on: being in your tree is not the
// same as being you.
func TestScopeSelfIsNotManaged(t *testing.T) {
	sc := AccessScope{EmployeeNo: "EMP-005", TeamNos: []string{"EMP-005", "EMP-001"}}

	if !sc.IsSelf("EMP-005") {
		t.Fatal("IsSelf missed the caller")
	}
	if sc.Manages("EMP-005") {
		t.Fatal("a manager must not count as managing themselves — this is what blocks self-approval")
	}
	if !sc.Manages("EMP-001") {
		t.Fatal("Manages missed a direct report")
	}
	if sc.Manages("EMP-999") {
		t.Fatal("Manages allowed someone outside the tree")
	}
}

// A platform user with no employee record must see nothing. The failure this
// guards is an empty allowlist reading as "no filter" and returning the roster.
func TestUnlinkedScopeSeesNothing(t *testing.T) {
	sc := AccessScope{}

	if sc.Allows("EMP-001") {
		t.Fatal("an unlinked caller was allowed an employee")
	}
	filter := sc.Filter()
	if filter == nil {
		t.Fatal("an unlinked caller produced a nil filter, which would return every row")
	}
	if len(filter) != 1 || filter[0] != "" {
		t.Fatalf("expected a filter that matches nothing, got %v", filter)
	}
}

func TestParseWorkWeek(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		working []time.Weekday
		off     []time.Weekday
	}{
		{
			name:    "default when empty",
			spec:    "",
			working: []time.Weekday{time.Monday, time.Friday},
			off:     []time.Weekday{time.Saturday, time.Sunday},
		},
		{
			name:    "monday to friday",
			spec:    "1,2,3,4,5",
			working: []time.Weekday{time.Monday, time.Wednesday, time.Friday},
			off:     []time.Weekday{time.Saturday, time.Sunday},
		},
		{
			name:    "six day week",
			spec:    "1,2,3,4,5,6",
			working: []time.Weekday{time.Monday, time.Saturday},
			off:     []time.Weekday{time.Sunday},
		},
		{
			name:    "sunday is ISO 7",
			spec:    "7",
			working: []time.Weekday{time.Sunday},
			off:     []time.Weekday{time.Monday},
		},
		{
			// A spec of nothing valid must not produce a week with no working
			// days, which would make every leave request free.
			name:    "garbage falls back to the default",
			spec:    "x,y,99",
			working: []time.Weekday{time.Monday, time.Friday},
			off:     []time.Weekday{time.Sunday},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ww := ParseWorkWeek(tc.spec)
			for _, d := range tc.working {
				if !ww[d] {
					t.Fatalf("%s should be a working day under %q", d, tc.spec)
				}
			}
			for _, d := range tc.off {
				if ww[d] {
					t.Fatalf("%s should not be a working day under %q", d, tc.spec)
				}
			}
		})
	}
}
