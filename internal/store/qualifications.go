package store

import (
	"context"
	"strings"
	"time"
)

// Qualification is one person's standing for one quality-control method: the
// course that qualifies them, when they completed it and when it lapses.
type Qualification struct {
	EmployeeNo   string     `json:"employee_no"`
	EmployeeName string     `json:"employee_name"`
	Email        string     `json:"email"`
	UserID       string     `json:"user_id,omitempty"`
	Method       string     `json:"method"`
	CourseCode   string     `json:"course_code"`
	CompletedOn  *time.Time `json:"completed_on,omitempty"`
	ExpiresOn    *time.Time `json:"expires_on,omitempty"`
	Expired      bool       `json:"expired"`
}

// QualificationFilter narrows the query; empty fields match everything.
type QualificationFilter struct {
	Method     string
	EmployeeNo string
	Email      string
	UserID     string
}

/*
Qualifications lists completed enrolments in courses that qualify for a lab
method — one row per person, method and course, newest completion first —
with whether each has lapsed. Quality control decides what to do with an
expired one; HR only reports it.
*/
func (s *Store) Qualifications(ctx context.Context, f QualificationFilter) ([]Qualification, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT emp.employee_no, trim(emp.first_name || ' ' || emp.last_name), COALESCE(emp.email, ''),
		       COALESCE(emp.user_id::text, ''), m.method, c.code, en.completed_on, en.expires_on
		FROM erp_training_enrolments en
		JOIN erp_training_courses c ON c.id = en.course_id
		JOIN erp_employees emp ON emp.id = en.employee_id
		CROSS JOIN LATERAL unnest(c.qualifies_for) AS m(method)
		WHERE en.status = 'completed' AND emp.status <> 'terminated'
		  AND ($1 = '' OR m.method = $1)
		  AND ($2 = '' OR emp.employee_no = $2)
		  AND ($3 = '' OR lower(emp.email) = lower($3))
		  AND ($4 = '' OR emp.user_id::text = $4)
		ORDER BY emp.employee_no, m.method, en.completed_on DESC NULLS LAST
		LIMIT 2000`,
		strings.TrimSpace(f.Method), strings.TrimSpace(f.EmployeeNo), strings.TrimSpace(f.Email), strings.TrimSpace(f.UserID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	out := []Qualification{}
	for rows.Next() {
		var q Qualification
		if err := rows.Scan(&q.EmployeeNo, &q.EmployeeName, &q.Email, &q.UserID, &q.Method, &q.CourseCode,
			&q.CompletedOn, &q.ExpiresOn); err != nil {
			return nil, err
		}
		q.Expired = q.ExpiresOn != nil && q.ExpiresOn.Before(today)
		out = append(out, q)
	}
	return out, rows.Err()
}

// SetCourseQualifiesFor replaces the methods a course qualifies for.
func (s *Store) SetCourseQualifiesFor(ctx context.Context, code string, methods []string) (*TrainingCourse, error) {
	var id string
	err := s.pool.QueryRow(ctx, `UPDATE erp_training_courses SET qualifies_for = $2, updated_at = NOW()
		WHERE code = UPPER($1) RETURNING id::text`, strings.TrimSpace(code), cleanMethods(methods)).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	courses, err := s.ListCourses(ctx, false)
	if err != nil {
		return nil, err
	}
	for i := range courses {
		if courses[i].ID.String() == id {
			return &courses[i], nil
		}
	}
	return nil, ErrNotFound
}

func cleanMethods(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}
