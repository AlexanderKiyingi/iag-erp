package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-erp/backend/internal/store"
)

func (a *API) ListProductionOrders(c *gin.Context) {
	items, err := a.Store.ListProductionOrders(c.Request.Context(), store.ListProductionOrdersFilter{
		Status: c.Query("status"),
		Since:  c.Query("since"),
		Limit:  queryInt(c, "limit", 200),
		Offset: queryInt(c, "offset", 0),
	})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateProductionOrder(c *gin.Context) {
	var body store.CreateProductionOrderInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.CreateProductionOrder(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) UpdateProductionOrder(c *gin.Context) {
	var body store.UpdateProductionOrderInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.UpdateProductionOrder(c.Request.Context(), c.Param("po_num"), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *API) DeleteProductionOrder(c *gin.Context) {
	if err := a.Store.DeleteProductionOrder(c.Request.Context(), c.Param("po_num")); err != nil {
		writeStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
