package handlers

import (
	"errors"
	"log"
	"net/http"

	"github.com/alvor-technologies/iag-platform-go/middleware"
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
	case errors.Is(err, store.ErrSchemaBehind):
		// Not the caller's fault and not retryable by them: the deploy is half
		// applied. 503 says "this service, later" rather than 400's "you, now".
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": err.Error(),
			"code":  "schema_behind",
		})
	default:
		// Everything above is a decision this service made and can explain. This
		// is the opposite: a driver or database error nobody classified, and
		// handing it back verbatim tells the caller things they should not learn
		// and nothing they can use.
		//
		// What escaped: table, column and constraint names plus the SQLSTATE on
		// any constraint or type error, and — worst of it — pgx renders a
		// connection failure as "failed to connect to `user=… database=…`", so a
		// database outage published the credentials' username and the database
		// name to anyone who could make a request fail.
		//
		// The detail belongs in the log, where it is actually useful. The caller
		// gets the request id, which is already on the response header, so a
		// report can be tied back to the logged cause.
		id := middleware.RequestIDFrom(c)
		log.Printf("unhandled store error [%s] %s %s: %v",
			id, c.Request.Method, c.FullPath(), err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":      "internal error",
			"code":       "internal_error",
			"request_id": id,
		})
	}
}
