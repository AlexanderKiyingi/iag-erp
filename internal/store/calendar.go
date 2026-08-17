package store

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Work calendar.
//
// Leave is charged in working days. A request spanning a weekend or a public
// holiday must not consume entitlement for days nobody was due to work, because
// that figure is not only the balance the employee sees: it is multiplied by a
// daily rate and posted to finance as an accrued-leave liability.
//
// The rules live outside this file. The work week is deployment configuration
// (it differs by site), and holidays are ordinary rows in erp_setup_items that
// an HR officer maintains. What this file owns is the counting.

// WorkWeek is the set of weekdays on which work is expected.
type WorkWeek map[time.Weekday]bool

// DefaultWorkWeek is Monday to Friday.
func DefaultWorkWeek() WorkWeek {
	return WorkWeek{
		time.Monday: true, time.Tuesday: true, time.Wednesday: true,
		time.Thursday: true, time.Friday: true,
	}
}

// ParseWorkWeek reads a work week from a comma-separated list of ISO weekday
// numbers (1 = Monday … 7 = Sunday), e.g. "1,2,3,4,5,6" for a six-day week.
//
// An unparseable or empty spec falls back to Monday–Friday rather than to an
// empty week: a week with no working days would make every leave request cost
// zero days, which is a worse failure than the wrong week.
func ParseWorkWeek(spec string) WorkWeek {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return DefaultWorkWeek()
	}
	ww := WorkWeek{}
	for _, part := range strings.Split(spec, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > 7 {
			continue
		}
		// ISO 7 (Sunday) is Go's time.Weekday 0.
		ww[time.Weekday(n%7)] = true
	}
	if len(ww) == 0 {
		return DefaultWorkWeek()
	}
	return ww
}

// SetWorkWeek installs the deployment's work week. Called once at boot.
func (s *Store) SetWorkWeek(ww WorkWeek) {
	if len(ww) > 0 {
		s.workWeek = ww
	}
}

func (s *Store) workingWeek() WorkWeek {
	if len(s.workWeek) == 0 {
		return DefaultWorkWeek()
	}
	return s.workWeek
}

// Holidays returns the non-working dates in [from, to], keyed by yyyy-mm-dd.
//
// Only active holiday setup items count: an HR officer marking a holiday
// inactive is how a gazetted date that moved gets withdrawn without deleting
// the record of it having been published.
func (s *Store) Holidays(ctx context.Context, from, to time.Time) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT effective_date, name
		FROM erp_setup_items
		WHERE item_type = 'holiday' AND status = 'active'
		  AND effective_date IS NOT NULL
		  AND effective_date BETWEEN $1::date AND $2::date`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var d time.Time
		var name string
		if err := rows.Scan(&d, &name); err != nil {
			return nil, err
		}
		out[d.Format("2006-01-02")] = name
	}
	return out, rows.Err()
}

// WorkingDaysBetween counts the working days in the inclusive range [start, end],
// excluding non-working weekdays and active public holidays.
//
// It returns the calendar span alongside so the caller can record both: `days`
// is what the employee is charged, `calendar_days` is how long they were away,
// and the two differing is the evidence the calendar was applied.
func (s *Store) WorkingDaysBetween(ctx context.Context, start, end time.Time) (working float64, calendar float64, err error) {
	start = start.UTC().Truncate(24 * time.Hour)
	end = end.UTC().Truncate(24 * time.Hour)
	if end.Before(start) {
		return 0, 0, ErrBadInput
	}

	holidays, err := s.Holidays(ctx, start, end)
	if err != nil {
		return 0, 0, err
	}

	ww := s.workingWeek()
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		calendar++
		if !ww[d.Weekday()] {
			continue
		}
		if _, isHoliday := holidays[d.Format("2006-01-02")]; isHoliday {
			continue
		}
		working++
	}
	return working, calendar, nil
}

// IsWorkingDay reports whether a single date is a working day.
func (s *Store) IsWorkingDay(ctx context.Context, day time.Time) (bool, error) {
	n, _, err := s.WorkingDaysBetween(ctx, day, day)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
