package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *API) HRReport(c *gin.Context) {
	result, err := a.Store.HRReport(c.Request.Context(),
		c.Query("type"),
		c.Query("department"),
		c.Query("from"),
		c.Query("to"),
	)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
