package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (a *API) ListAPIAuditLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, total, err := a.Audit.ListAPIAuditLogs(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list audit logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func (a *API) MonitoringSummary(c *gin.Context) {
	summary, err := a.Audit.MonitoringSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "monitoring failed"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (a *API) AdminConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service":  a.Cfg.ServiceName,
		"audience": a.Cfg.Audience,
		"gateway":  a.Cfg.GatewayAPIPrefix,
	})
}
