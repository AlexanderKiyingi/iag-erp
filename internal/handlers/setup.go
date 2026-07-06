package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"iag-erp/backend/internal/store"
)

func (a *API) ListWorksites(c *gin.Context) {
	items, err := a.Store.ListWorksites(c.Request.Context())
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) ListSetupItems(c *gin.Context) {
	items, err := a.Store.ListSetupItems(c.Request.Context(), store.ListSetupFilter{
		ItemType: c.Query("type"),
		Status:   c.Query("status"),
		Limit:    queryInt(c, "limit", 100),
		Offset:   queryInt(c, "offset", 0),
	})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	out := make([]map[string]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.FrontendShape())
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (a *API) CreateSetupItem(c *gin.Context) {
	var body store.UpsertSetupInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.CreateSetupItem(c.Request.Context(), body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item.FrontendShape())
}

func (a *API) UpdateSetupItem(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body store.UpsertSetupInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.Store.UpdateSetupItem(c.Request.Context(), id, body)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item.FrontendShape())
}

func (a *API) DeleteSetupItem(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := a.Store.DeleteSetupItem(c.Request.Context(), id); err != nil {
		writeStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
