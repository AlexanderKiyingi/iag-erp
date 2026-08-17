package handlers

import (
	"errors"
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
	Cfg    *config.Config
	Store  *store.Store
	Audit  *auditlog.Store
	Bus    *events.Bus
	Notify *notify.Publisher
	Pool   *pgxpool.Pool
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
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, store.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "conflict"})
	case errors.Is(err, store.ErrForbidden):
		// 403 rather than 404: the caller holds the permission for this kind of
		// record, just not for this one. Hiding that distinction would leave an
		// employee unable to tell a typo from a boundary.
		c.JSON(http.StatusForbidden, gin.H{"error": "not permitted for this record", "code": "out_of_scope"})
	case errors.Is(err, store.ErrInsufficientLeave):
		c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient leave balance", "code": "insufficient_leave"})
	case errors.Is(err, store.ErrNoWorkingDays):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "the selected dates contain no working days",
			"code":  "no_working_days",
		})
	case errors.Is(err, store.ErrNoCompensation):
		c.JSON(http.StatusNotFound, gin.H{"error": "no effective compensation record", "code": "no_compensation"})
	case errors.Is(err, store.ErrBadInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
