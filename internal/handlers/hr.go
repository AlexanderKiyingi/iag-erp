package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"iag-erp/backend/internal/store"
)

func (a *API) GetDepartment(c *gin.Context) {
	item, err := a.Store.GetDepartment(c.Request.Context(), c.Param("code"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListDepartments(c *gin.Context) {
	includeInactive := c.Query("all") == "true"
	items, err := a.Store.ListDepartments(c.Request.Context(), includeInactive)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateDepartment(c *gin.Context) {
	var body store.CreateDepartmentInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.CreateDepartment(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) UpdateDepartment(c *gin.Context) {
	var body store.UpdateDepartmentInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.UpdateDepartment(c.Request.Context(), c.Param("code"), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListEmployees(c *gin.Context) {
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	items, err := a.Store.ListEmployees(c.Request.Context(), store.ListEmployeesFilter{
		Status:                c.Query("status"),
		DepartmentCode:        c.Query("department"),
		PlantCode:             c.Query("plant"),
		Search:                c.Query("search"),
		Limit:                 queryInt(c, "limit", 50),
		Offset:                queryInt(c, "offset", 0),
		RestrictToEmployeeNos: sc.Filter(),
	})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetEmployee(c *gin.Context) {
	if !a.requireEmployeeInScope(c, c.Param("employee_no")) {
		return
	}
	item, err := a.Store.GetEmployee(c.Request.Context(), c.Param("employee_no"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) GetEmployeeByOperatorRef(c *gin.Context) {
	item, err := a.Store.GetEmployeeByOperatorRef(c.Request.Context(), c.Param("ref"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) CreateEmployee(c *gin.Context) {
	// Hiring is an HR act, not a self-service one: there is no scoped version of
	// "add a person to the roster".
	if !a.requireUnrestricted(c) {
		return
	}
	var body store.CreateEmployeeInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.CreateEmployee(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) UpdateEmployee(c *gin.Context) {
	// Employee records carry department, manager, status and employment type.
	// A manager editing their own reporting line or a report's status is the
	// kind of change HR exists to control, so this stays HR-only rather than
	// becoming a scoped edit.
	if !a.requireUnrestricted(c) {
		return
	}
	var body store.UpdateEmployeeInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.UpdateEmployee(c.Request.Context(), c.Param("employee_no"), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) GetLeaveBalance(c *gin.Context) {
	if !a.requireEmployeeInScope(c, c.Param("employee_no")) {
		return
	}
	year := queryInt(c, "year", 0)
	leaveType := c.DefaultQuery("type", "ANNUAL")
	item, err := a.Store.GetLeaveBalance(c.Request.Context(), c.Param("employee_no"), leaveType, year)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListLeaveTypes(c *gin.Context) {
	items, err := a.Store.ListLeaveTypes(c.Request.Context())
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) ListLeaveRequests(c *gin.Context) {
	ctx := c.Request.Context()
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	dept := c.Query("department")
	if dept != "" {
		if resolved, err := a.Store.ResolveDepartmentFilter(ctx, dept); err == nil {
			dept = resolved
		}
	}
	plant := store.ResolvePlantFilter(c.Query("plant"))
	items, err := a.Store.ListLeaveRequests(ctx, store.ListLeaveRequestsFilter{
		Status:                c.Query("status"),
		DepartmentCode:        dept,
		PlantCode:             plant,
		Limit:                 queryInt(c, "limit", 50),
		Offset:                queryInt(c, "offset", 0),
		RestrictToEmployeeNos: sc.Filter(),
	})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetLeaveRequest(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := a.Store.GetLeaveRequest(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, item.EmployeeNo) {
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) CreateLeaveRequest(c *gin.Context) {
	var body store.CreateLeaveRequestInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Filing leave in someone else's name is a manager's job, so the scope
	// check is "in your tree" rather than "is you".
	if !a.requireEmployeeInScope(c, body.EmployeeNo) {
		return
	}
	item, err := a.Store.CreateLeaveRequest(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) DecideLeaveRequest(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		Action      string `json:"action"`
		ApproverRef string `json:"approver_ref"`
		// Why. Optional, but the reason a rejection is worth reading — and the
		// field every approval desk in front of this service already collects.
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := a.Store.GetLeaveRequest(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	// Approval authority: HR decides anything, a manager decides for their tree,
	// and nobody decides their own request. Holding erp.approve_leave used to be
	// enough to approve one's own leave, which is the one case an approval
	// exists to prevent.
	if !sc.Unrestricted {
		if sc.IsSelf(existing.EmployeeNo) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "you cannot decide your own leave request",
				"code":  "self_approval",
			})
			return
		}
		if !sc.Manages(existing.EmployeeNo) {
			writeStoreError(c, store.ErrForbidden)
			return
		}
	}

	// Record the approver as an employee, not only as the free-text ref the
	// frontend sends. Resolved even when scoping is off, so the audit trail
	// starts filling before the flag is turned on rather than after.
	item, err := a.Store.DecideLeaveRequest(c.Request.Context(), id, body.Action, body.ApproverRef,
		body.Note, a.resolveCallerEmployeeNo(c))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	a.notifyLeaveDecision(c, item)
	c.JSON(http.StatusOK, item)
}

// notifyLeaveDecision emails the employee the outcome of their leave request.
// Leave is decided by one person in one step, so the requester is the only
// party to tell — there is no next approver. Best effort: the decision is
// already committed and must not fail on a notification problem.
func (a *API) notifyLeaveDecision(c *gin.Context, item *store.LeaveRequest) {
	if a.Notify == nil || !a.Notify.Enabled() || item == nil {
		return
	}
	ctx := c.Request.Context()
	emp, err := a.Store.GetEmployee(ctx, item.EmployeeNo)
	if err != nil || emp == nil || emp.Email == nil || strings.TrimSpace(*emp.Email) == "" {
		// No address on file: nothing to send. The decision still stands and is
		// visible in the employee's leave list.
		return
	}
	window := item.StartsOn.Format("2006-01-02") + " to " + item.EndsOn.Format("2006-01-02")
	status := strings.ToLower(strings.TrimSpace(item.Status))
	// Keyed on request + outcome so a retry does not double-send while a later
	// status change still notifies.
	eventID := "erp.leave:" + item.ID.String() + ":" + status
	_ = a.Notify.PublishEmail(ctx, eventID, strings.TrimSpace(*emp.Email), "approval.decision", map[string]string{
		"Title": "Leave " + status + ": " + window,
		"Body": item.LeaveTypeName + " leave for " + window + " (" +
			strconv.FormatFloat(item.Days, 'f', -1, 64) + " days) was " + status + ".",
	})
}

func (a *API) CancelLeaveRequest(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	existing, err := a.Store.GetLeaveRequest(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !a.requireEmployeeInScope(c, existing.EmployeeNo) {
		return
	}
	item, err := a.Store.CancelLeaveRequest(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListAttendance(c *gin.Context) {
	ctx := c.Request.Context()
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	dept := c.Query("department")
	if dept != "" {
		if resolved, err := a.Store.ResolveDepartmentFilter(ctx, dept); err == nil {
			dept = resolved
		}
	}
	items, err := a.Store.ListAttendanceScoped(ctx, c.Query("date"), store.ResolvePlantFilter(c.Query("plant")), dept,
		queryInt(c, "limit", 50), queryInt(c, "offset", 0), sc.Filter())
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) UpsertAttendance(c *gin.Context) {
	var body store.CreateAttendanceInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !a.requireEmployeeInScope(c, body.EmployeeNo) {
		return
	}
	item, err := a.Store.UpsertAttendance(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ClockIn(c *gin.Context) {
	var body struct {
		EmployeeNo string `json:"employee_no" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Clocking in is a claim about where *you* were. A manager correcting a
	// report's day does it through the attendance upsert, which is recorded as
	// an edit rather than as the employee's own punch.
	if !a.requireSelfOrUnrestricted(c, body.EmployeeNo) {
		return
	}
	item, err := a.Store.ClockIn(c.Request.Context(), body.EmployeeNo)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ClockOut(c *gin.Context) {
	var body struct {
		EmployeeNo string `json:"employee_no" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !a.requireSelfOrUnrestricted(c, body.EmployeeNo) {
		return
	}
	item, err := a.Store.ClockOut(c.Request.Context(), body.EmployeeNo)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) DeleteAttendance(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := a.Store.DeleteAttendance(c.Request.Context(), id); err != nil {
		writeStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *API) ReconcileLeaveStatuses(c *gin.Context) {
	n, err := a.Store.ReconcileAllEmployeeLeaveStatuses(c.Request.Context())
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"reconciled": n})
}

func (a *API) RunBirthdayReminders(c *gin.Context) {
	if a.Notify == nil || !a.Notify.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifications not configured (KAFKA_BROKERS)"})
		return
	}
	result, err := a.Store.SendBirthdayReminders(c.Request.Context(), a.Notify, a.Cfg)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// OperatorRoster is the production-facing view of the roster: employees
// with an operator_ref, the fields iag-production projects into its
// operator registry, and nothing personal. Open to allow-listed service
// callers; a person needs erp.view_employee and is not record-scoped
// because the payload carries nothing HR_SCOPE_ENFORCED protects.
func (a *API) OperatorRoster(c *gin.Context) {
	items, err := a.Store.ListOperatorRoster(c.Request.Context(), c.Query("plant"), queryInt(c, "limit", 200), queryInt(c, "offset", 0))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
