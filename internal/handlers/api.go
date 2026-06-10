package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-erp/backend/internal/auditlog"
	"iag-erp/backend/internal/config"
	"iag-erp/backend/internal/db"
	"iag-erp/backend/internal/events"
	"iag-erp/backend/internal/notify"
	"iag-erp/backend/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

type API struct {
	Cfg   *config.Config
	Store *store.Store
	Audit *auditlog.Store
	Bus    *events.Bus
	Notify *notify.Publisher
	Pool  *pgxpool.Pool
}

func (a *API) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": a.Cfg.ServiceName})
}

func (a *API) Ready(c *gin.Context) {
	if err := db.Ping(c.Request.Context(), a.Pool); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "database": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "database": true})
}

func writeStoreError(c *gin.Context, err error) {
	if err == store.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err == store.ErrConflict {
		c.JSON(http.StatusConflict, gin.H{"error": "conflict"})
		return
	}
	if err == store.ErrBadInput {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
