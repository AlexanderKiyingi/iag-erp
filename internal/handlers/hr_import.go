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
	item, err := a.Store.UpdateLeaveRequest(c.Request.Context(), id, body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ImportEmployees(c *gin.Context) {
	var body struct {
		Items []store.CreateEmployeeInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n, err := a.Store.ImportEmployees(c.Request.Context(), body.Items)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"imported": n})
}

func (a *API) ImportLeaveRequests(c *gin.Context) {
	var body struct {
		Items []store.CreateLeaveRequestInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n, err := a.Store.ImportLeaveRequests(c.Request.Context(), body.Items)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"imported": n})
}

func (a *API) ImportAttendance(c *gin.Context) {
	var body struct {
		Items []store.CreateAttendanceInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n, err := a.Store.ImportAttendance(c.Request.Context(), body.Items)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"imported": n})
}
