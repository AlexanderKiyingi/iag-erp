package store

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Performance reviews and goals.
//
// The JSONB version stored a rating and a comment with no cycle, no reviewer,
// and no notion of whether the employee had ever seen it. A review the person
// being reviewed never saw is not a review, so the state machine ends at
// 'acknowledged' rather than at 'shared'.

type ReviewCycle struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Status      string    `json:"status"`
	// ReviewCount and AcknowledgedCount are how a cycle reports its own progress.
	ReviewCount       int       `json:"review_count"`
	AcknowledgedCount int       `json:"acknowledged_count"`
	CreatedAt         time.Time `json:"created_at"`
}

type PerformanceReview struct {
	ID              uuid.UUID      `json:"id"`
	CycleID         uuid.UUID      `json:"cycle_id"`
	CycleCode       string         `json:"cycle_code"`
	EmployeeNo      string         `json:"employee_no"`
	EmployeeName    string         `json:"employee_name"`
	ReviewerNo      *string        `json:"reviewer_employee_no,omitempty"`
	Status          string         `json:"status"`
	OverallRating   *float64       `json:"overall_rating,omitempty"`
	SelfComments    string         `json:"self_comments"`
	ManagerComments string         `json:"manager_comments"`
	SubmittedAt     *time.Time     `json:"submitted_at,omitempty"`
	SharedAt        *time.Time     `json:"shared_at,omitempty"`
	AcknowledgedAt  *time.Time     `json:"acknowledged_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	Ratings         []ReviewRating `json:"ratings,omitempty"`
}

type ReviewRating struct {
	Competency string  `json:"competency"`
	Rating     float64 `json:"rating"`
	Weight     float64 `json:"weight"`
	Comment    string  `json:"comment"`
}

type PerformanceGoal struct {
	ID          uuid.UUID  `json:"id"`
	EmployeeNo  string     `json:"employee_no"`
	CycleCode   *string    `json:"cycle_code,omitempty"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Metric      string     `json:"metric"`
	Target      string     `json:"target"`
	Weight      float64    `json:"weight"`
	ProgressPct float64    `json:"progress_pct"`
	Status      string     `json:"status"`
	DueOn       *time.Time `json:"due_on,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (s *Store) ListReviewCycles(ctx context.Context, status string) ([]ReviewCycle, error) {
	q := `SELECT c.id, c.code, c.name, c.period_start, c.period_end, c.status,
		(SELECT COUNT(*)::int FROM erp_performance_reviews r WHERE r.cycle_id = c.id),
		(SELECT COUNT(*)::int FROM erp_performance_reviews r WHERE r.cycle_id = c.id AND r.status = 'acknowledged'),
		c.created_at
		FROM erp_review_cycles c WHERE 1=1`
	args := []any{}
	if status != "" {
		q += ` AND c.status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY c.period_start DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReviewCycle{}
	for rows.Next() {
		var c ReviewCycle
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.PeriodStart, &c.PeriodEnd, &c.Status,
			&c.ReviewCount, &c.AcknowledgedCount, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type CreateCycleInput struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
}

func (s *Store) CreateReviewCycle(ctx context.Context, in CreateCycleInput) (*ReviewCycle, error) {
	start, err1 := time.Parse("2006-01-02", in.PeriodStart)
	end, err2 := time.Parse("2006-01-02", in.PeriodEnd)
	if err1 != nil || err2 != nil || end.Before(start) || strings.TrimSpace(in.Code) == "" {
		return nil, ErrBadInput
	}
	name := in.Name
	if strings.TrimSpace(name) == "" {
		name = in.Code
	}
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_review_cycles (code, name, period_start, period_end)
		VALUES (UPPER($1), $2, $3, $4) RETURNING id`,
		in.Code, name, start, end).Scan(&id); err != nil {
		if strings.Contains(err.Error(), "erp_review_cycles_code_key") {
			return nil, ErrConflict
		}
		return nil, err
	}
	cycles, err := s.ListReviewCycles(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range cycles {
		if cycles[i].ID == id {
			return &cycles[i], nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) SetReviewCycleStatus(ctx context.Context, id uuid.UUID, to string) (*ReviewCycle, error) {
	var current string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM erp_review_cycles WHERE id = $1`, id).
		Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := ReviewCycleStates.Transition(current, to); err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE erp_review_cycles SET status = $2, updated_at = NOW() WHERE id = $1`, id, to); err != nil {
		return nil, err
	}
	cycles, err := s.ListReviewCycles(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range cycles {
		if cycles[i].ID == id {
			return &cycles[i], nil
		}
	}
	return nil, ErrNotFound
}

const reviewColumns = `r.id, r.cycle_id, c.code, e.employee_no, e.first_name || ' ' || e.last_name,
	rv.employee_no, r.status, r.overall_rating, r.self_comments, r.manager_comments,
	r.submitted_at, r.shared_at, r.acknowledged_at, r.created_at`

const reviewFrom = `FROM erp_performance_reviews r
	JOIN erp_review_cycles c ON c.id = r.cycle_id
	JOIN erp_employees e ON e.id = r.employee_id
	LEFT JOIN erp_employees rv ON rv.id = r.reviewer_employee_id`

func (s *Store) GetReview(ctx context.Context, id uuid.UUID) (*PerformanceReview, error) {
	var r PerformanceReview
	err := s.pool.QueryRow(ctx, `SELECT `+reviewColumns+` `+reviewFrom+` WHERE r.id = $1`, id).
		Scan(&r.ID, &r.CycleID, &r.CycleCode, &r.EmployeeNo, &r.EmployeeName, &r.ReviewerNo,
			&r.Status, &r.OverallRating, &r.SelfComments, &r.ManagerComments,
			&r.SubmittedAt, &r.SharedAt, &r.AcknowledgedAt, &r.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ratings, err := s.reviewRatings(ctx, id)
	if err != nil {
		return nil, err
	}
	r.Ratings = ratings
	return &r, nil
}

func (s *Store) ListReviews(ctx context.Context, cycleCode, employeeNo, status string, restrictTo []string, limit, offset int) ([]PerformanceReview, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + reviewColumns + ` ` + reviewFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if len(restrictTo) > 0 {
		q += ` AND e.employee_no = ANY($` + itoa(n) + `)`
		args = append(args, restrictTo)
		n++
	}
	if cycleCode != "" {
		q += ` AND c.code = $` + itoa(n)
		args = append(args, strings.ToUpper(cycleCode))
		n++
	}
	if employeeNo != "" {
		q += ` AND e.employee_no = $` + itoa(n)
		args = append(args, employeeNo)
		n++
	}
	if status != "" {
		q += ` AND r.status = $` + itoa(n)
		args = append(args, status)
		n++
	}
	_ = n
	q += ` ORDER BY r.created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PerformanceReview{}
	for rows.Next() {
		var r PerformanceReview
		if err := rows.Scan(&r.ID, &r.CycleID, &r.CycleCode, &r.EmployeeNo, &r.EmployeeName,
			&r.ReviewerNo, &r.Status, &r.OverallRating, &r.SelfComments, &r.ManagerComments,
			&r.SubmittedAt, &r.SharedAt, &r.AcknowledgedAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// OpenReview creates a review for an employee in an open cycle.
//
// A closed cycle takes no new reviews: reopening the period after ratings were
// calibrated against each other is how a moderated cycle stops being moderated.
func (s *Store) OpenReview(ctx context.Context, cycleCode, employeeNo, reviewerNo string) (*PerformanceReview, error) {
	var cycleID uuid.UUID
	var cycleStatus string
	if err := s.pool.QueryRow(ctx,
		`SELECT id, status FROM erp_review_cycles WHERE code = $1`, strings.ToUpper(cycleCode)).
		Scan(&cycleID, &cycleStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if cycleStatus != "open" {
		return nil, ErrConflict
	}

	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_performance_reviews (cycle_id, employee_id, reviewer_employee_id)
		VALUES ($1,
			(SELECT id FROM erp_employees WHERE employee_no = $2),
			(SELECT id FROM erp_employees WHERE employee_no = NULLIF($3,'')))
		RETURNING id`, cycleID, employeeNo, reviewerNo).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "erp_performance_reviews_cycle_id_employee_id_key") {
			return nil, ErrConflict
		}
		if strings.Contains(err.Error(), "employee_id") {
			return nil, ErrBadInput
		}
		return nil, err
	}
	return s.GetReview(ctx, id)
}

// AdvanceReviewInput carries what a move contributes to the review.
type AdvanceReviewInput struct {
	To              string         `json:"to"`
	SelfComments    string         `json:"self_comments"`
	ManagerComments string         `json:"manager_comments"`
	OverallRating   *float64       `json:"overall_rating"`
	Ratings         []ReviewRating `json:"ratings"`
}

// AdvanceReview moves a review and records what the move added.
func (s *Store) AdvanceReview(ctx context.Context, id uuid.UUID, in AdvanceReviewInput) (*PerformanceReview, error) {
	existing, err := s.GetReview(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ReviewStates.Transition(existing.Status, in.To); err != nil {
		return nil, err
	}

	// Sharing a review with no rating on it gives the employee nothing to
	// acknowledge, and nothing for a later cycle to compare against.
	if in.To == "shared" {
		rating := existing.OverallRating
		if in.OverallRating != nil {
			rating = in.OverallRating
		}
		if rating == nil {
			return nil, ErrBadInput
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE erp_performance_reviews SET
		  status = $2,
		  self_comments    = CASE WHEN $3 = '' THEN self_comments ELSE $3 END,
		  manager_comments = CASE WHEN $4 = '' THEN manager_comments ELSE $4 END,
		  overall_rating   = COALESCE($5, overall_rating),
		  submitted_at    = CASE WHEN $2 = 'manager_review' AND submitted_at IS NULL THEN NOW() ELSE submitted_at END,
		  shared_at       = CASE WHEN $2 = 'shared' AND shared_at IS NULL THEN NOW() ELSE shared_at END,
		  acknowledged_at = CASE WHEN $2 = 'acknowledged' THEN NOW() ELSE acknowledged_at END,
		  updated_at = NOW()
		WHERE id = $1`, id, in.To, in.SelfComments, in.ManagerComments, in.OverallRating); err != nil {
		return nil, err
	}

	for _, r := range in.Ratings {
		if strings.TrimSpace(r.Competency) == "" || r.Rating < 1 || r.Rating > 5 {
			return nil, ErrBadInput
		}
		weight := r.Weight
		if weight <= 0 {
			weight = 1
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO erp_review_ratings (review_id, competency, rating, weight, comment)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (review_id, competency) DO UPDATE
			SET rating = EXCLUDED.rating, weight = EXCLUDED.weight, comment = EXCLUDED.comment`,
			id, r.Competency, r.Rating, weight, r.Comment); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetReview(ctx, id)
}

func (s *Store) reviewRatings(ctx context.Context, reviewID uuid.UUID) ([]ReviewRating, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT competency, rating, weight, comment
		FROM erp_review_ratings WHERE review_id = $1 ORDER BY competency`, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReviewRating{}
	for rows.Next() {
		var r ReviewRating
		if err := rows.Scan(&r.Competency, &r.Rating, &r.Weight, &r.Comment); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// WeightedRating is the rating the competency scores imply, for a reviewer who
// wants the arithmetic done rather than done differently by each manager.
func WeightedRating(ratings []ReviewRating) float64 {
	var weighted, weight float64
	for _, r := range ratings {
		w := r.Weight
		if w <= 0 {
			w = 1
		}
		weighted += r.Rating * w
		weight += w
	}
	if weight == 0 {
		return 0
	}
	return math.Round(weighted/weight*100) / 100
}

type UpsertGoalInput struct {
	EmployeeNo  string   `json:"employee_no"`
	CycleCode   string   `json:"cycle_code"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Metric      string   `json:"metric"`
	Target      string   `json:"target"`
	Weight      float64  `json:"weight"`
	DueOn       string   `json:"due_on"`
	ProgressPct *float64 `json:"progress_pct"`
	Status      string   `json:"status"`
}

func (s *Store) CreateGoal(ctx context.Context, in UpsertGoalInput) (*PerformanceGoal, error) {
	if strings.TrimSpace(in.EmployeeNo) == "" || strings.TrimSpace(in.Title) == "" {
		return nil, ErrBadInput
	}
	weight := in.Weight
	if weight <= 0 {
		weight = 1
	}
	var due *time.Time
	if in.DueOn != "" {
		t, err := time.Parse("2006-01-02", in.DueOn)
		if err != nil {
			return nil, ErrBadInput
		}
		due = &t
	}

	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_performance_goals
			(employee_id, cycle_id, title, description, metric, target, weight, due_on)
		VALUES (
			(SELECT id FROM erp_employees WHERE employee_no = $1),
			(SELECT id FROM erp_review_cycles WHERE code = UPPER(NULLIF($2,''))),
			$3, $4, $5, $6, $7, $8)
		RETURNING id`, in.EmployeeNo, in.CycleCode, in.Title, in.Description,
		in.Metric, in.Target, weight, due).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "employee_id") {
			return nil, ErrBadInput
		}
		return nil, err
	}
	return s.GetGoal(ctx, id)
}

const goalColumns = `g.id, e.employee_no, c.code, g.title, g.description, g.metric, g.target,
	g.weight, g.progress_pct, g.status, g.due_on, g.created_at`

const goalFrom = `FROM erp_performance_goals g
	JOIN erp_employees e ON e.id = g.employee_id
	LEFT JOIN erp_review_cycles c ON c.id = g.cycle_id`

func (s *Store) GetGoal(ctx context.Context, id uuid.UUID) (*PerformanceGoal, error) {
	var g PerformanceGoal
	err := s.pool.QueryRow(ctx, `SELECT `+goalColumns+` `+goalFrom+` WHERE g.id = $1`, id).
		Scan(&g.ID, &g.EmployeeNo, &g.CycleCode, &g.Title, &g.Description, &g.Metric,
			&g.Target, &g.Weight, &g.ProgressPct, &g.Status, &g.DueOn, &g.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &g, nil
}

func (s *Store) ListGoals(ctx context.Context, employeeNo, cycleCode, status string, restrictTo []string, limit, offset int) ([]PerformanceGoal, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + goalColumns + ` ` + goalFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if len(restrictTo) > 0 {
		q += ` AND e.employee_no = ANY($` + itoa(n) + `)`
		args = append(args, restrictTo)
		n++
	}
	if employeeNo != "" {
		q += ` AND e.employee_no = $` + itoa(n)
		args = append(args, employeeNo)
		n++
	}
	if cycleCode != "" {
		q += ` AND c.code = $` + itoa(n)
		args = append(args, strings.ToUpper(cycleCode))
		n++
	}
	if status != "" {
		q += ` AND g.status = $` + itoa(n)
		args = append(args, status)
		n++
	}
	_ = n
	q += ` ORDER BY g.created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PerformanceGoal{}
	for rows.Next() {
		var g PerformanceGoal
		if err := rows.Scan(&g.ID, &g.EmployeeNo, &g.CycleCode, &g.Title, &g.Description,
			&g.Metric, &g.Target, &g.Weight, &g.ProgressPct, &g.Status, &g.DueOn,
			&g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UpdateGoal changes progress and status, enforcing the goal state machine.
func (s *Store) UpdateGoal(ctx context.Context, id uuid.UUID, in UpsertGoalInput) (*PerformanceGoal, error) {
	existing, err := s.GetGoal(ctx, id)
	if err != nil {
		return nil, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = existing.Status
	}
	if err := GoalStates.Transition(existing.Status, status); err != nil {
		return nil, err
	}

	progress := existing.ProgressPct
	if in.ProgressPct != nil {
		if *in.ProgressPct < 0 || *in.ProgressPct > 100 {
			return nil, ErrBadInput
		}
		progress = *in.ProgressPct
	}
	// A goal declared achieved is complete by definition; saying so at 60%
	// leaves two contradictory statements on the same row.
	if status == "achieved" {
		progress = 100
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE erp_performance_goals SET
		  title       = CASE WHEN $2 = '' THEN title ELSE $2 END,
		  description = CASE WHEN $3 = '' THEN description ELSE $3 END,
		  metric      = CASE WHEN $4 = '' THEN metric ELSE $4 END,
		  target      = CASE WHEN $5 = '' THEN target ELSE $5 END,
		  progress_pct = $6, status = $7, updated_at = NOW()
		WHERE id = $1`, id, in.Title, in.Description, in.Metric, in.Target, progress, status); err != nil {
		return nil, err
	}
	return s.GetGoal(ctx, id)
}
