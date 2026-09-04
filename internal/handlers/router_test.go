package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"iag-erp/backend/internal/config"
)

// Gin builds its routing tree at registration and panics on a conflict between
// a static segment and a wildcard. Several routes added for compensation and
// payroll sit under /employees/:employee_no and /payroll/..., so constructing
// the router is itself the assertion.
func newTestRouter(t *testing.T, payrollEnabled bool) *gin.Engine {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("router construction panicked: %v", r)
		}
	}()
	return NewRouter(RouterDeps{
		API:            &API{Cfg: &config.Config{ServiceName: "erp-test"}},
		CORSOrigins:    []string{"http://localhost:3000"},
		PayrollEnabled: payrollEnabled,
	})
}

// hasRoute reports whether a method/path pair is registered. Asserting on the
// route table rather than on a response keeps the test about routing: serving
// the request would exercise handlers that need a database.
func hasRoute(r *gin.Engine, method, path string) bool {
	for _, route := range r.Routes() {
		if route.Method == method && route.Path == path {
			return true
		}
	}
	return false
}

func TestRouterBuilds(t *testing.T) {
	for _, payroll := range []bool{false, true} {
		if r := newTestRouter(t, payroll); r == nil {
			t.Fatal("nil router")
		}
	}
}

// Payroll is behind a flag. With it off the routes must not be registered at
// all: an endpoint that answers "unauthorised" is an endpoint that is switched
// on, and payroll should not be reachable where the tax tables are unreviewed.
func TestPayrollRoutesAreGatedByFlag(t *testing.T) {
	payrollRoutes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/payroll/runs"},
		{http.MethodGet, "/api/v1/payroll/runs/:id"},
		{http.MethodGet, "/api/v1/payroll/runs/:id/payslips"},
		{http.MethodPost, "/api/v1/payroll/runs"},
		{http.MethodPost, "/api/v1/payroll/runs/:id/approve"},
		{http.MethodPost, "/api/v1/payroll/runs/:id/post"},
		// Externally-computed payroll is gated by the same flag. The flag also
		// gates listing, approving and posting, so registering this outside the
		// group would let a run be created that could never be read or paid.
		{http.MethodPost, "/api/v1/payroll/external-runs"},
	}

	off := newTestRouter(t, false)
	for _, r := range payrollRoutes {
		if hasRoute(off, r.method, r.path) {
			t.Fatalf("%s %s is registered with PAYROLL_ENABLED off", r.method, r.path)
		}
	}

	on := newTestRouter(t, true)
	for _, r := range payrollRoutes {
		if !hasRoute(on, r.method, r.path) {
			t.Fatalf("%s %s is missing with PAYROLL_ENABLED on", r.method, r.path)
		}
	}
}

// Compensation was built and never routed; these are the routes that fixed it.
// They are not behind the payroll flag — a pay rate is needed to value leave
// whether or not payroll is being run.
func TestCompensationRoutesAlwaysRegistered(t *testing.T) {
	r := newTestRouter(t, false)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/employees/:employee_no/compensation"},
		{http.MethodGet, "/api/v1/employees/:employee_no/compensation/history"},
		{http.MethodPut, "/api/v1/employees/:employee_no/compensation"},
	} {
		if !hasRoute(r, route.method, route.path) {
			t.Fatalf("%s %s is not registered", route.method, route.path)
		}
	}
}

func TestHealthIsUnauthenticated(t *testing.T) {
	r := newTestRouter(t, false)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health returned %d, want 200", rec.Code)
	}
}
