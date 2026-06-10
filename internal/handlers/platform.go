package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-erp/backend/internal/store"
)

func (a *API) Bootstrap(c *gin.Context) {
	ctx := c.Request.Context()
	_, _ = a.Store.ReconcileAllEmployeeLeaveStatuses(ctx)
	counts, _ := a.Store.HRCounts(ctx)
	departments, _ := a.Store.ListDepartments(ctx, false)
	pendingLeave, _ := a.Store.ListLeaveRequests(ctx, store.ListLeaveRequestsFilter{Status: "pending", Limit: 20})

	c.JSON(http.StatusOK, gin.H{
		"service":         a.Cfg.ServiceName,
		"gateway":         a.Cfg.GatewayAPIPrefix,
		"hr_counts":       counts,
		"departments":     departments,
		"pending_leave":   pendingLeave,
	})
}

func (a *API) PlatformStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service":  a.Cfg.ServiceName,
		"audience": a.Cfg.Audience,
		"gateway":  a.Cfg.GatewayAPIPrefix,
		"modules":  []string{"hr", "production_orders"},
	})
}
