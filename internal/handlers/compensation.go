package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Compensation.
//
// The store, the table and the effective dating were built and then never
// routed: SetCompensation and CurrentCompensation had no callers, so the pay
// rate the leave liability is valued at could only be set by writing SQL. These
// are the endpoints that make it reachable.
//
// Access is deliberately narrower than the rest of HR. Reading is self or HR;
// writing is HR only, whatever the reader's place in the reporting tree — a
// manager who can see a report's leave has no business setting their salary.

func (a *API) GetCompensation(c *gin.Context) {
	employeeNo := c.Param("employee_no")
	if !a.requireSelfOrUnrestricted(c, employeeNo) {
		return
	}
	asOf := time.Now().UTC()
	if raw := c.Query("as_of"); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "as_of must be YYYY-MM-DD"})
			return
		}
		asOf = parsed
	}
	item, err := a.Store.CurrentCompensation(c.Request.Context(), employeeNo, asOf)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"employee_no":            item.EmployeeNo,
		"monthly_gross":          item.MonthlyGross,
		"currency":               item.Currency,
		"working_days_per_month": item.WorkingDaysPerMonth,
		"daily_rate":             item.DailyRate(),
		"effective_from":         item.EffectiveFrom.Format("2006-01-02"),
	})
}

func (a *API) ListCompensationHistory(c *gin.Context) {
	employeeNo := c.Param("employee_no")
	if !a.requireSelfOrUnrestricted(c, employeeNo) {
		return
	}
	items, err := a.Store.CompensationHistory(c.Request.Context(), employeeNo)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) SetCompensation(c *gin.Context) {
	if !a.requireUnrestricted(c) {
		return
	}
	employeeNo := c.Param("employee_no")
	var body struct {
		MonthlyGross        float64 `json:"monthly_gross"`
		Currency            string  `json:"currency"`
		WorkingDaysPerMonth float64 `json:"working_days_per_month"`
		EffectiveFrom       string  `json:"effective_from"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Effective from today unless stated. Back-dating is allowed — a rise agreed
	// in March and entered in May applies from March — and it re-measures the
	// leave liability from that date, which is why the date is explicit rather
	// than taken from the clock.
	effectiveFrom := time.Now().UTC()
	if body.EffectiveFrom != "" {
		parsed, err := time.Parse("2006-01-02", body.EffectiveFrom)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "effective_from must be YYYY-MM-DD"})
			return
		}
		effectiveFrom = parsed
	}

	// 22 is the five-day-week convention the compensation table defaults to;
	// repeating it here keeps a caller that omits the field from writing a zero
	// divisor into the daily rate.
	workingDays := body.WorkingDaysPerMonth
	if workingDays <= 0 {
		workingDays = 22
	}

	item, err := a.Store.SetCompensation(c.Request.Context(), employeeNo,
		body.MonthlyGross, body.Currency, workingDays, effectiveFrom)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"employee_no":            item.EmployeeNo,
		"monthly_gross":          item.MonthlyGross,
		"currency":               item.Currency,
		"working_days_per_month": item.WorkingDaysPerMonth,
		"daily_rate":             item.DailyRate(),
		"effective_from":         item.EffectiveFrom.Format("2006-01-02"),
	})
}
