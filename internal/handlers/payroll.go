package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	appmw "iag-erp/backend/internal/middleware"
	"iag-erp/backend/internal/store"
)

// Payroll endpoints.
//
// The lifecycle is compute → approve → post, and each step is a separate call
// by design: the person who computes a payroll must not be the person who
// releases it, and a single "run payroll" button cannot express that.

func writePayrollError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrSelfApproval):
		c.JSON(http.StatusForbidden, gin.H{
			"error": "payroll must be approved by someone other than its preparer",
			"code":  "self_approval",
		})
	case errors.Is(err, store.ErrRunNotDraft):
		c.JSON(http.StatusConflict, gin.H{"error": "run is no longer a draft", "code": "not_draft"})
	case errors.Is(err, store.ErrRunNotApproved):
		c.JSON(http.StatusConflict, gin.H{"error": "run is not approved", "code": "not_approved"})
	case errors.Is(err, store.ErrPeriodAlreadyPosted):
		c.JSON(http.StatusConflict, gin.H{
			"error": "a payroll run for this period has already been posted",
			"code":  "period_posted",
		})
	default:
		writeStoreError(c, err)
	}
}

// callerEmployeeNo is who the caller is as an employee. Payroll records both
// the preparer and the approver, and an unlinked platform user cannot be either
// — an approval attributed to nobody is not an approval.
func (a *API) callerEmployeeNo(c *gin.Context) (string, bool) {
	if no := a.resolveCallerEmployeeNo(c); no != "" {
		return no, true
	}
	c.JSON(http.StatusForbidden, gin.H{
		"error": "your platform user is not linked to an employee record",
		"code":  "employee_link_required",
	})
	return "", false
}

// resolveCallerEmployeeNo is the same lookup without the rejection, for callers
// that record who acted but do not require it — a leave decision is still valid
// when the approver's login was never linked to an employee record, it is just
// less well attributed.
func (a *API) resolveCallerEmployeeNo(c *gin.Context) string {
	if sc, err := a.scope(c); err == nil && sc.EmployeeNo != "" {
		return sc.EmployeeNo
	}
	// An unrestricted scope carries no identity of its own, so resolve the
	// caller's employee record directly.
	if userID, ok := appmw.UserID(c); ok {
		if emp, err := a.Store.GetEmployeeByUserID(c.Request.Context(), userID); err == nil {
			return emp.EmployeeNo
		}
	}
	return ""
}

func (a *API) ListPayrollRuns(c *gin.Context) {
	items, err := a.Store.ListPayrollRuns(c.Request.Context(),
		c.Query("period"), c.Query("status"),
		queryInt(c, "limit", 50), queryInt(c, "offset", 0))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetPayrollRun(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := a.Store.GetPayrollRun(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) CreatePayrollRun(c *gin.Context) {
	preparer, ok := a.callerEmployeeNo(c)
	if !ok {
		return
	}
	var body struct {
		Period   string `json:"period"`
		Currency string `json:"currency"`
		Notes    string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Period == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period is required (YYYY-MM)"})
		return
	}
	item, err := a.Store.CreatePayrollRun(c.Request.Context(), body.Period, body.Currency, preparer, body.Notes)
	if err != nil {
		writePayrollError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) ApprovePayrollRun(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	approver, ok := a.callerEmployeeNo(c)
	if !ok {
		return
	}
	item, err := a.Store.ApprovePayrollRun(c.Request.Context(), id, approver)
	if err != nil {
		writePayrollError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) PostPayrollRun(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := a.Store.PostPayrollRun(c.Request.Context(), id)
	if err != nil {
		writePayrollError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) CancelPayrollRun(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := a.Store.CancelPayrollRun(c.Request.Context(), id)
	if err != nil {
		writePayrollError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListPayslips(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	items, err := a.Store.ListPayslips(c.Request.Context(), id, sc.Filter())
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListEmployeePayslips is the self-service payslip history: an employee's own
// posted payslips, readable without any sight of the run they came from.
func (a *API) ListEmployeePayslips(c *gin.Context) {
	employeeNo := c.Param("employee_no")
	if !a.requireSelfOrUnrestricted(c, employeeNo) {
		return
	}
	items, err := a.Store.ListEmployeePayslips(c.Request.Context(), employeeNo, queryInt(c, "limit", 24))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) ListPayComponentDefinitions(c *gin.Context) {
	items, err := a.Store.ListPayComponentDefinitions(c.Request.Context())
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) ListEmployeePayComponents(c *gin.Context) {
	employeeNo := c.Param("employee_no")
	if !a.requireSelfOrUnrestricted(c, employeeNo) {
		return
	}
	items, err := a.Store.ListEmployeePayComponents(c.Request.Context(), employeeNo)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) AssignPayComponent(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	var body store.AssignPayComponentInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.AssignPayComponent(c.Request.Context(), c.Param("employee_no"), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) EndPayComponent(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	id, err := uuid.Parse(c.Param("component_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid component id"})
		return
	}
	effectiveTo := time.Now().UTC().Truncate(24 * time.Hour)
	if raw := c.Query("effective_to"); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "effective_to must be YYYY-MM-DD"})
			return
		}
		effectiveTo = parsed
	}
	if err := a.Store.EndPayComponent(c.Request.Context(), id, effectiveTo); err != nil {
		writeStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
