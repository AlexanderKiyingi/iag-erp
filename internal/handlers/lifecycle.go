package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"iag-erp/backend/internal/store"
)

// Recruitment, checklists, performance, disciplinary and training.
//
// These replace the generic /hr/:module CRUD for the five domains that have a
// workflow. That endpoint still exists and still serves the modules which have
// not been promoted, so nothing that works today stops working.
//
// Records about a person are scoped like the rest of HR: an employee sees their
// own, a manager their team's, HR everyone's. Recruitment is not — a candidate
// is not an employee, and there is no reporting line to scope them by.

// writeLifecycleError turns a refused state transition into a 409 that says
// what was attempted and what was allowed. "Invalid input" on a workflow move
// tells an operator nothing about which move to make instead.
func writeLifecycleError(c *gin.Context, err error) {
	var invalid store.ErrInvalidTransition
	if errors.As(err, &invalid) {
		c.JSON(http.StatusConflict, gin.H{
			"error":   invalid.Error(),
			"code":    "invalid_transition",
			"from":    invalid.From,
			"to":      invalid.To,
			"allowed": invalid.Allowed,
		})
		return
	}
	switch {
	case errors.Is(err, store.ErrChecklistIncomplete):
		c.JSON(http.StatusConflict, gin.H{
			"error": "required checklist items are still outstanding",
			"code":  "checklist_incomplete",
		})
	case errors.Is(err, store.ErrHearingRequired):
		c.JSON(http.StatusConflict, gin.H{
			"error": "a dismissal requires a hearing to have been held",
			"code":  "hearing_required",
		})
	default:
		writeStoreError(c, err)
	}
}

func parseID(c *gin.Context, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + param})
		return uuid.Nil, false
	}
	return id, true
}

// ---------------------------------------------------------------------------
// Recruitment
// ---------------------------------------------------------------------------

func (a *API) ListRequisitions(c *gin.Context) {
	items, err := a.Store.ListRequisitions(c.Request.Context(),
		c.Query("status"), c.Query("department"),
		queryInt(c, "limit", 50), queryInt(c, "offset", 0))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetRequisition(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := a.Store.GetRequisition(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) CreateRequisition(c *gin.Context) {
	var body store.CreateRequisitionInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.CreateRequisition(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) SetRequisitionStatus(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.SetRequisitionStatus(c.Request.Context(), id, body.Status,
		a.resolveCallerEmployeeNo(c))
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListApplications(c *gin.Context) {
	var requisitionID *uuid.UUID
	if raw := c.Query("requisition_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid requisition_id"})
			return
		}
		requisitionID = &id
	}
	items, err := a.Store.ListApplications(c.Request.Context(), requisitionID, c.Query("stage"),
		queryInt(c, "limit", 50), queryInt(c, "offset", 0))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetApplication(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := a.Store.GetApplication(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	events, err := a.Store.ListApplicationEvents(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"application": item, "events": events})
}

// CreateApplication takes the candidate's details and the requisition together:
// in practice an application arrives as one thing, and making the caller create
// a candidate first invites a duplicate person per application.
func (a *API) CreateApplication(c *gin.Context) {
	var body struct {
		RequisitionID string `json:"requisition_id"`
		FullName      string `json:"full_name"`
		Email         string `json:"email"`
		Phone         string `json:"phone"`
		Source        string `json:"source"`
		CVRef         string `json:"cv_ref"`
		Notes         string `json:"notes"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requisitionID, err := uuid.Parse(body.RequisitionID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid requisition_id"})
		return
	}

	ctx := c.Request.Context()
	candidate, err := a.Store.UpsertCandidate(ctx, body.FullName, body.Email, body.Phone,
		body.Source, body.CVRef, body.Notes)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	app, err := a.Store.CreateApplication(ctx, requisitionID, candidate.ID)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"application": app, "candidate": candidate})
}

func (a *API) AdvanceApplication(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Stage  string `json:"stage"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.AdvanceApplication(c.Request.Context(), id, body.Stage, body.Reason,
		a.resolveCallerEmployeeNo(c))
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// HireApplicant is the recruitment→roster handoff: it creates the employee and
// links the application to them in one call.
func (a *API) HireApplicant(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !a.requireUnrestricted(c) {
		return
	}
	var body store.CreateEmployeeInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	app, emp, err := a.Store.HireApplicant(c.Request.Context(), id, body, a.resolveCallerEmployeeNo(c))
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"application": app, "employee": emp})
}

// ---------------------------------------------------------------------------
// Onboarding / offboarding checklists
// ---------------------------------------------------------------------------

func (a *API) ListChecklistTemplates(c *gin.Context) {
	items, err := a.Store.ListChecklistTemplates(c.Request.Context(), c.Query("kind"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) ListChecklists(c *gin.Context) {
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	items, err := a.Store.ListChecklists(c.Request.Context(), c.Query("employee_no"),
		c.Query("kind"), c.Query("status"), sc.Filter(),
		queryInt(c, "limit", 50), queryInt(c, "offset", 0))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetChecklist(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := a.Store.GetChecklist(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, item.EmployeeNo) {
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) IssueChecklist(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	var body store.IssueChecklistInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.IssueChecklist(c.Request.Context(), body)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// SetChecklistItemStatus is scoped to the team rather than to HR: the people who
// tick off "assets returned" and "access revoked" are IT and the line manager,
// not the HR officer who issued the checklist.
func (a *API) SetChecklistItemStatus(c *gin.Context) {
	itemID, ok := parseID(c, "item_id")
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.SetChecklistItemStatus(c.Request.Context(), itemID,
		body.Status, body.Notes, a.resolveCallerEmployeeNo(c))
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, item.EmployeeNo) {
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) CompleteChecklist(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !a.requireUnrestricted(c) {
		return
	}
	item, err := a.Store.CompleteChecklist(c.Request.Context(), id)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) CancelChecklist(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !a.requireUnrestricted(c) {
		return
	}
	item, err := a.Store.CancelChecklist(c.Request.Context(), id)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// ---------------------------------------------------------------------------
// Performance
// ---------------------------------------------------------------------------

func (a *API) ListReviewCycles(c *gin.Context) {
	items, err := a.Store.ListReviewCycles(c.Request.Context(), c.Query("status"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateReviewCycle(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	var body store.CreateCycleInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.CreateReviewCycle(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) SetReviewCycleStatus(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !a.requireUnrestricted(c) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.SetReviewCycleStatus(c.Request.Context(), id, body.Status)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListReviews(c *gin.Context) {
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	items, err := a.Store.ListReviews(c.Request.Context(), c.Query("cycle"),
		c.Query("employee_no"), c.Query("status"), sc.Filter(),
		queryInt(c, "limit", 50), queryInt(c, "offset", 0))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetReview(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := a.Store.GetReview(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, item.EmployeeNo) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"review": item, "weighted_rating": store.WeightedRating(item.Ratings)})
}

func (a *API) OpenReview(c *gin.Context) {
	var body struct {
		CycleCode  string `json:"cycle_code"`
		EmployeeNo string `json:"employee_no"`
		ReviewerNo string `json:"reviewer_employee_no"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !a.requireEmployeeInScope(c, body.EmployeeNo) {
		return
	}
	item, err := a.Store.OpenReview(c.Request.Context(), body.CycleCode, body.EmployeeNo, body.ReviewerNo)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// AdvanceReview enforces the one rule the workflow exists for: only the
// employee acknowledges their own review. A manager clicking "acknowledged" on
// the employee's behalf turns the record into a claim nobody made.
func (a *API) AdvanceReview(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body store.AdvanceReviewInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := a.Store.GetReview(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, existing.EmployeeNo) {
		return
	}
	if body.To == "acknowledged" {
		sc, ok := a.requireScope(c)
		if !ok {
			return
		}
		if !sc.IsSelf(existing.EmployeeNo) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "only the employee may acknowledge their own review",
				"code":  "acknowledge_self_only",
			})
			return
		}
	}

	item, err := a.Store.AdvanceReview(c.Request.Context(), id, body)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListGoals(c *gin.Context) {
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	items, err := a.Store.ListGoals(c.Request.Context(), c.Query("employee_no"),
		c.Query("cycle"), c.Query("status"), sc.Filter(),
		queryInt(c, "limit", 50), queryInt(c, "offset", 0))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateGoal(c *gin.Context) {
	var body store.UpsertGoalInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !a.requireEmployeeInScope(c, body.EmployeeNo) {
		return
	}
	item, err := a.Store.CreateGoal(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) UpdateGoal(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body store.UpsertGoalInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := a.Store.GetGoal(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, existing.EmployeeNo) {
		return
	}
	item, err := a.Store.UpdateGoal(c.Request.Context(), id, body)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// ---------------------------------------------------------------------------
// Disciplinary
// ---------------------------------------------------------------------------

func (a *API) ListCases(c *gin.Context) {
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	items, err := a.Store.ListCases(c.Request.Context(), c.Query("employee_no"),
		c.Query("status"), sc.Filter(),
		queryInt(c, "limit", 50), queryInt(c, "offset", 0))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetCase(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := a.Store.GetCase(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, item.EmployeeNo) {
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) OpenCase(c *gin.Context) {
	var body store.OpenCaseInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !a.requireEmployeeInScope(c, body.EmployeeNo) {
		return
	}
	item, err := a.Store.OpenCase(c.Request.Context(), body, a.resolveCallerEmployeeNo(c))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) AdvanceCase(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body store.AdvanceCaseInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := a.Store.GetCase(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	// Nobody investigates, hears or decides their own case.
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	if sc.IsSelf(existing.EmployeeNo) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "you cannot act on a disciplinary case about yourself",
			"code":  "self_case",
		})
		return
	}
	if !sc.Allows(existing.EmployeeNo) {
		writeStoreError(c, store.ErrForbidden)
		return
	}

	item, err := a.Store.AdvanceCase(c.Request.Context(), id, body, a.resolveCallerEmployeeNo(c))
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// ---------------------------------------------------------------------------
// Training
// ---------------------------------------------------------------------------

func (a *API) ListCourses(c *gin.Context) {
	items, err := a.Store.ListCourses(c.Request.Context(), c.Query("mandatory") == "true")
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateCourse(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	var body store.UpsertCourseInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.CreateCourse(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) ListEnrolments(c *gin.Context) {
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	items, err := a.Store.ListEnrolments(c.Request.Context(), store.ListEnrolmentsFilter{
		EmployeeNo:         c.Query("employee_no"),
		CourseCode:         c.Query("course"),
		Status:             c.Query("status"),
		ExpiringWithinDays: queryInt(c, "expiring_within_days", 0),
		RestrictTo:         sc.Filter(),
		Limit:              queryInt(c, "limit", 100),
		Offset:             queryInt(c, "offset", 0),
	})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) Enrol(c *gin.Context) {
	var body struct {
		CourseCode string `json:"course_code"`
		EmployeeNo string `json:"employee_no"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !a.requireEmployeeInScope(c, body.EmployeeNo) {
		return
	}
	item, err := a.Store.Enrol(c.Request.Context(), body.CourseCode, body.EmployeeNo)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) UpdateEnrolment(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body store.UpdateEnrolmentInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := a.Store.GetEnrolment(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, existing.EmployeeNo) {
		return
	}
	item, err := a.Store.UpdateEnrolment(c.Request.Context(), id, body)
	if err != nil {
		writeLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
