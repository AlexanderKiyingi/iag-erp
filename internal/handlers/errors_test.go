package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"iag-erp/backend/internal/store"
)

// writeStoreError's default branch used to return err.Error() verbatim, and it
// is reached from 91 call sites — every store error nobody classified. What
// came back was never useful to the caller and sometimes told them things they
// had no business knowing.
func TestWriteStoreErrorDoesNotLeakDatabaseDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name    string
		err     error
		secrets []string
	}{
		{
			name: "constraint violation",
			// Names the table, the constraint and the SQLSTATE.
			err: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "erp_employees_employee_no_key",
				TableName:      "erp_employees",
				Message:        `duplicate key value violates unique constraint "erp_employees_employee_no_key"`,
			},
			secrets: []string{"erp_employees", "23505", "constraint"},
		},
		{
			name: "connection failure",
			// pgx renders this as: failed to connect to `user=… database=…`.
			// A database outage published the credentials' username.
			err:     errors.New("failed to connect to `user=iag_erp_prod database=railway`: dial tcp 10.0.0.4:5432: connect: connection refused"),
			secrets: []string{"iag_erp_prod", "railway", "10.0.0.4", "5432"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/employees", nil)

			writeStoreError(c, tc.err)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d", rec.Code)
			}
			body := rec.Body.String()
			for _, secret := range tc.secrets {
				if strings.Contains(body, secret) {
					t.Errorf("response leaks %q: %s", secret, body)
				}
			}

			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("response is not JSON: %v", err)
			}
			if payload["code"] != "internal_error" {
				t.Errorf(`want code "internal_error", got %v`, payload["code"])
			}
			// The caller needs something to quote in a bug report; the id is on
			// the response header too.
			if _, ok := payload["request_id"]; !ok {
				t.Error("no request_id to correlate with the logged cause")
			}
		})
	}
}

// The classified branches carry information the caller asked for and can act
// on. Silencing those along with the leak would be a regression of its own.
func TestWriteStoreErrorStillExplainsClassifiedFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name   string
		err    error
		status int
		expect string
	}{
		{"not found", store.ErrNotFound, http.StatusNotFound, "not found"},
		{"bad input", store.ErrBadInput, http.StatusBadRequest, "invalid input"},
		{"out of scope", store.ErrForbidden, http.StatusForbidden, "out_of_scope"},
		{"insufficient leave", store.ErrInsufficientLeave, http.StatusBadRequest, "insufficient_leave"},
		{
			"schema behind",
			// This one deliberately keeps its detail: it names the migration to
			// apply, and it is about the deployment rather than the caller.
			store.ErrSchemaBehind,
			http.StatusServiceUnavailable,
			"schema_behind",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/employees", nil)

			writeStoreError(c, tc.err)

			if rec.Code != tc.status {
				t.Fatalf("want %d, got %d", tc.status, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.expect) {
				t.Errorf("want %q in %s", tc.expect, rec.Body.String())
			}
		})
	}
}
