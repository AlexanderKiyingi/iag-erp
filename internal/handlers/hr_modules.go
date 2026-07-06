package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"iag-erp/backend/internal/store"
)

func hrModuleItems(records []store.HRModuleRecord) []map[string]string {
	out := make([]map[string]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.FrontendShape())
	}
	return out
}

func (a *API) ListHRModuleRecords(c *gin.Context) {
	module := c.Param("module")
	items, err := a.Store.ListHRModuleRecords(c.Request.Context(), store.ListHRModuleFilter{
		Module:     module,
		Department: c.Query("department"),
		Status:     c.Query("status"),
		Search:     c.Query("search"),
		Limit:      queryInt(c, "limit", 100),
		Offset:     queryInt(c, "offset", 0),
	})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": hrModuleItems(items)})
}

func (a *API) GetHRModuleRecord(c *gin.Context) {
	module := c.Param("module")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := a.Store.GetHRModuleRecord(c.Request.Context(), module, id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item.FrontendShape())
}

func (a *API) CreateHRModuleRecord(c *gin.Context) {
	module := c.Param("module")
	var flat map[string]any
	if err := c.ShouldBindJSON(&flat); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in := store.UpsertHRModuleInput{Data: flat}
	if v, ok := flat["department"].(string); ok {
		in.Department = v
	}
	if v, ok := flat["status"].(string); ok {
		in.Status = v
	}
	delete(in.Data, "id")
	item, err := a.Store.CreateHRModuleRecord(c.Request.Context(), module, in)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item.FrontendShape())
}

func (a *API) UpdateHRModuleRecord(c *gin.Context) {
	module := c.Param("module")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var flat map[string]any
	if err := c.ShouldBindJSON(&flat); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in := store.UpsertHRModuleInput{Data: flat}
	if v, ok := flat["department"].(string); ok {
		in.Department = v
	}
	if v, ok := flat["status"].(string); ok {
		in.Status = v
	}
	delete(in.Data, "id")
	item, err := a.Store.UpdateHRModuleRecord(c.Request.Context(), module, id, in)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, item.FrontendShape())
}

func (a *API) DeleteHRModuleRecord(c *gin.Context) {
	module := c.Param("module")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := a.Store.DeleteHRModuleRecord(c.Request.Context(), module, id); err != nil {
		writeStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *API) ImportHRModuleRecords(c *gin.Context) {
	module := c.Param("module")
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n, err := a.Store.ImportHRModuleRecords(c.Request.Context(), module, body.Items)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"imported": n})
}
