package store

import (
	"testing"
	"time"
)

// Hours is computed from the two clocks rather than stored, so that a
// correction to either cannot leave a third number disagreeing with them. These
// pin the cases where "just subtract them" is the wrong answer.
func TestAttendanceHours(t *testing.T) {
	at := func(h, m int) *time.Time {
		v := time.Date(2026, 9, 10, h, m, 0, 0, time.UTC)
		return &v
	}

	cases := []struct {
		name string
		in   *time.Time
		out  *time.Time
		want float64
	}{
		{"a full day", at(8, 0), at(17, 30), 9.5},
		// A shift still running has no duration yet. Guessing one would put
		// hours against a day nobody has finished.
		{"no check-out yet", at(8, 0), nil, 0},
		{"no check-in at all", nil, at(17, 0), 0},
		{"neither", nil, nil, 0},
		// Clocks out of order are a data error, not a negative shift. Returning
		// a negative would flow into a pay calculation as a deduction.
		{"check-out before check-in", at(17, 0), at(8, 0), 0},
		{"same instant", at(8, 0), at(8, 0), 0},
		// Rounded to the two decimals a payslip shows, so the figure on screen
		// is the figure that was computed.
		{"rounded to two decimals", at(8, 0), at(8, 20), 0.33},
	}

	for _, tc := range cases {
		if got := attendanceHours(tc.in, tc.out); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
