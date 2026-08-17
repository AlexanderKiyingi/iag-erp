package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"iag-erp/backend/internal/store"
)

func (a *API) UpdateLeaveRequest(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body store.UpdateLeaveRequestInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	item, err := a.Store.UpdateLeaveRequest(c.Request.Context(), id, body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// Bulk import is HR-only across the board. A scoped import would have to be
// checked row by row against the caller's tree, and an import that silently
// dropped the rows outside it is worse than one that is refused outright.
func (a *API) ImportEmployees(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	var body struct {
		Items []store.CreateEmployeeInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := a.Store.ImportEmployees(c.Request.Context(), body.Items)
	if err != nil && result.Imported == 0 {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (a *API) ImportLeaveRequests(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	var body struct {
		Items []store.CreateLeaveRequestInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := a.Store.ImportLeaveRequests(c.Request.Context(), body.Items)
	if err != nil && result.Imported == 0 {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (a *API) ImportAttendance(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	var body struct {
		Items []store.CreateAttendanceInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := a.Store.ImportAttendance(c.Request.Context(), body.Items)
	if err != nil && result.Imported == 0 {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
