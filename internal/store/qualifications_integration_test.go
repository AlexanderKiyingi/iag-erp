package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Qualifications (026) against a real Postgres: a completed course that
// qualifies for a lab method answers quality-control's question, and one
// completed too long ago reports itself expired.
func TestQualificationsForLabMethods(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := time.Now().UTC().Format("150405.000")
	method := "LM-QUAL-" + run
	email := fmt.Sprintf("Ana.%s@IAG.test", run)

	emp, err := s.CreateEmployee(ctx, CreateEmployeeInput{EmployeeNo: "EMP-Q-" + run, FirstName: "Ana", LastName: "Lab", Email: email})
	if err != nil {
		t.Fatal(err)
	}
	twelve := 12
	course, err := s.CreateCourse(ctx, UpsertCourseInput{Code: "QC-MOIST-" + run, Name: "Oven moisture", ValidityMonths: &twelve,
		QualifiesFor: []string{method, " ", method}})
	if err != nil || len(course.QualifiesFor) != 1 {
		t.Fatalf("course: %+v %v", course, err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM erp_training_enrolments WHERE course_id = $1`, course.ID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM erp_training_courses WHERE id = $1`, course.ID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM erp_employees WHERE id = $1`, emp.ID)
	})

	none, err := s.Qualifications(ctx, QualificationFilter{Email: email, Method: method})
	if err != nil || len(none) != 0 {
		t.Fatalf("enrolled but not completed is not qualified: %+v %v", none, err)
	}
	en, err := s.Enrol(ctx, course.Code, emp.EmployeeNo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateEnrolment(ctx, en.ID, UpdateEnrolmentInput{To: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateEnrolment(ctx, en.ID, UpdateEnrolmentInput{To: "completed"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Qualifications(ctx, QualificationFilter{Email: email, Method: method})
	if err != nil || len(got) != 1 || got[0].Expired || got[0].ExpiresOn == nil || got[0].CourseCode != course.Code {
		t.Fatalf("qualified: %+v %v", got, err)
	}
	// Matched case-insensitively, as quality-control sends the token's email.
	if lower, _ := s.Qualifications(ctx, QualificationFilter{Email: "ana." + run + "@iag.test"}); len(lower) != 1 {
		t.Fatalf("email case: %+v", lower)
	}

	// The same course completed two years ago has lapsed.
	if _, err := s.pool.Exec(ctx, `UPDATE erp_training_enrolments SET completed_on = CURRENT_DATE - 730,
		expires_on = CURRENT_DATE - 365 WHERE id = $1`, en.ID); err != nil {
		t.Fatal(err)
	}
	old, _ := s.Qualifications(ctx, QualificationFilter{Method: method})
	if len(old) != 1 || !old[0].Expired {
		t.Fatalf("expired: %+v", old)
	}

	updated, err := s.SetCourseQualifiesFor(ctx, course.Code, []string{"LM-OTHER-" + run})
	if err != nil || len(updated.QualifiesFor) != 1 || updated.QualifiesFor[0] != "LM-OTHER-"+run {
		t.Fatalf("set qualifies_for: %+v %v", updated, err)
	}
	if after, _ := s.Qualifications(ctx, QualificationFilter{Method: method}); len(after) != 0 {
		t.Fatalf("no longer qualifies for the old method: %+v", after)
	}
}
