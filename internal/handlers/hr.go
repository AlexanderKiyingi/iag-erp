package handlers

import (
	"net/http"

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
	items, err := a.Store.ListEmployees(c.Request.Context(), store.ListEmployeesFilter{
		Status:         c.Query("status"),
		DepartmentCode: c.Query("department"),
		PlantCode:      c.Query("plant"),
		Search:         c.Query("search"),
		Limit:          queryInt(c, "limit", 50),
		Offset:         queryInt(c, "offset", 0),
	})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetEmployee(c *gin.Context) {
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
	items, err := a.Store.ListLeaveRequests(c.Request.Context(), store.ListLeaveRequestsFilter{
		Status: c.Query("status"),
		Limit:  queryInt(c, "limit", 50),
		Offset: queryInt(c, "offset", 0),
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
	c.JSON(http.StatusOK, item)
}

func (a *API) CreateLeaveRequest(c *gin.Context) {
	var body store.CreateLeaveRequestInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.DecideLeaveRequest(c.Request.Context(), id, body.Action, body.ApproverRef)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) CancelLeaveRequest(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
	items, err := a.Store.ListAttendance(c.Request.Context(), c.Query("date"), c.Query("plant"),
		queryInt(c, "limit", 50), queryInt(c, "offset", 0))
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
	item, err := a.Store.ClockOut(c.Request.Context(), body.EmployeeNo)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
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
