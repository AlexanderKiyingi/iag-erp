package store

import (
	"testing"
	"time"
)

// Accrual arithmetic. The existing GetLeaveBalance grants a whole year's
// entitlement on the hire date, which overstates what a mid-year joiner has
// earned. Finance now accrues a liability from these figures, so an overstated
// balance becomes an overstated obligation.

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestEarnedIsProRatedForServiceWithinTheYear(t *testing.T) {
	cases := []struct {
		name string
		hire time.Time
		asOf time.Time
		want float64
	}{
		// Employed all year: the full entitlement by December.
		{"full year", day(2020, time.March, 1), day(2026, time.December, 31), 24},
		// Joined at the start of July: six months counted, half the entitlement.
		{"joined mid-year", day(2026, time.July, 1), day(2026, time.December, 31), 12},
		// A month counts once it has started, so January alone earns one month.
		{"first month", day(2026, time.January, 10), day(2026, time.January, 31), 2},
		// Not yet hired: nothing earned.
		{"hired later this year", day(2026, time.November, 1), day(2026, time.June, 30), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := earnedToDate(24, tc.hire, 0, tc.asOf)
			if got != tc.want {
				t.Errorf("earned = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProbationDelaysAccrual(t *testing.T) {
	hire := day(2026, time.January, 1)
	// Six months' probation: nothing accrues until July.
	if got := earnedToDate(24, hire, 6, day(2026, time.May, 31)); got != 0 {
		t.Errorf("during probation earned = %v, want 0", got)
	}
	// From July, six months of the year remain.
	if got := earnedToDate(24, hire, 6, day(2026, time.December, 31)); got != 12 {
		t.Errorf("after probation earned = %v, want 12", got)
	}
}

func TestEarnedNeverExceedsTheAnnualEntitlement(t *testing.T) {
	// A long-serving employee cannot accrue more than a year's worth in a year,
	// however far back the hire date runs.
	if got := earnedToDate(24, day(1999, time.January, 1), 0, day(2026, time.December, 31)); got != 24 {
		t.Errorf("earned = %v, want the annual entitlement capped at 24", got)
	}
}

func TestZeroEntitlementEarnsNothing(t *testing.T) {
	if got := earnedToDate(0, day(2020, time.January, 1), 0, day(2026, time.December, 31)); got != 0 {
		t.Errorf("earned = %v, want 0 for a type with no entitlement", got)
	}
}

// Leave is administered in days and half-days; carrying more precision than
// that into a rate multiplication downstream is noise, not accuracy.
func TestEarnedRoundsToTwoDecimals(t *testing.T) {
	got := earnedToDate(21, day(2026, time.January, 1), 0, day(2026, time.February, 28))
	// 21 × 2/12 = 3.5
	if got != 3.5 {
		t.Errorf("earned = %v, want 3.5", got)
	}
	got = earnedToDate(10, day(2026, time.January, 1), 0, day(2026, time.March, 31))
	// 10 × 3/12 = 2.5
	if got != 2.5 {
		t.Errorf("earned = %v, want 2.5", got)
	}
}

func TestDailyRateDividesGrossByWorkingDays(t *testing.T) {
	c := Compensation{MonthlyGross: 2_200_000, WorkingDaysPerMonth: 22}
	if got := c.DailyRate(); got != 100_000 {
		t.Errorf("daily rate = %v, want 100000", got)
	}
}

func TestDailyRateIsZeroWithoutWorkingDays(t *testing.T) {
	// Guards the division rather than panicking; the constraint on the table
	// makes this unreachable through the API, but a zero rate values leave at
	// nothing rather than crashing a valuation run.
	c := Compensation{MonthlyGross: 1_000_000}
	if got := c.DailyRate(); got != 0 {
		t.Errorf("daily rate = %v, want 0", got)
	}
}
