package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"iag-erp/backend/internal/events"
	"iag-erp/backend/internal/store"
)

func (a *API) IntegrationStatus(c *gin.Context) {
	out := gin.H{
		"service":     a.Cfg.ServiceName,
		"modules":     []string{"hr", "production_orders"},
		"event_bus":   a.Bus != nil && a.Bus.Enabled(),
		"kafka_topic": events.TopicOperations,
		"event_types": []string{
			events.TypeEmployeeCreated,
			events.TypeEmployeeUpdated,
			events.TypeEmployeeTerminated,
			events.TypeLeaveApproved,
			events.TypeLeaveRejected,
			events.TypeLeaveCancelled,
			events.TypeProductionOrderCreated,
			events.TypeProductionOrderUpdated,
			events.TypeProductionOrderDeleted,
		},
		"webhooks": gin.H{
			"production_orders": "/api/v1/integrations/production-orders/webhook",
		},
	}
	c.JSON(http.StatusOK, out)
}

func (a *API) ProductionOrderWebhook(c *gin.Context) {
	var body store.ProductionOrderWebhookInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	reqB, _ := json.Marshal(body)
	ctx := c.Request.Context()
	po, err := a.Store.ApplyProductionOrderWebhook(ctx, body)
	if err != nil {
		_ = a.Store.LogIntegrationCall(ctx, "external_erp", "production_order.webhook", body.PONum, "error", reqB, nil, err.Error())
		writeStoreError(c, err)
		return
	}
	var respB json.RawMessage
	if po != nil {
		respB, _ = json.Marshal(po)
	} else {
		respB, _ = json.Marshal(gin.H{"po_num": body.PONum, "deleted": true})
	}
	_ = a.Store.LogIntegrationCall(ctx, "external_erp", "production_order.webhook", body.PONum, "ok", reqB, respB, "")
	if po == nil {
		c.JSON(http.StatusOK, gin.H{"po_num": body.PONum, "deleted": true})
		return
	}
	c.JSON(http.StatusAccepted, po)
}

func (a *API) ListIntegrationCalls(c *gin.Context) {
	items, err := a.Store.ListIntegrationCalls(c.Request.Context(), c.Query("target"), queryInt(c, "limit", 50))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetEmployeeByUserID(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}
	item, err := a.Store.GetEmployeeByUserID(c.Request.Context(), userID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) ListDirectReports(c *gin.Context) {
	items, err := a.Store.ListDirectReports(c.Request.Context(), c.Param("employee_no"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
