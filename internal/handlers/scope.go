package handlers

import (
	"github.com/gin-gonic/gin"

	appmw "iag-erp/backend/internal/middleware"
	"iag-erp/backend/internal/store"
)

// Record-level access for HR endpoints.
//
// Permissions gate the endpoint; this gates the row. A caller is unrestricted
// when they are HR or admin, and otherwise sees themselves and their reporting
// tree — the "self / team / all" split every comparable HR product enforces.
//
// The whole layer is behind HR_SCOPE_ENFORCED and off by default. Turning it on
// narrows what existing tokens can reach, so it is a deliberate rollout step
// once erp.view_all_hr has been granted to the HR groups, not a silent upgrade
// that locks an HR officer out of their own roster.

// PermViewAllHR is the grant that means "all employees, not just your own tree".
const PermViewAllHR = "erp.view_all_hr"

// scopeContextKey caches the resolved scope for the life of one request: it
// costs a recursive query, and several handlers ask for it more than once.
const scopeContextKey = "erp.access_scope"

// scope resolves the caller's record-level access.
//
// Errors resolving the scope are returned rather than defaulted, because the
// safe default and the useful default point opposite ways: defaulting open
// would hand out the roster on a database blip, and defaulting closed would
// silently empty an HR officer's screen.
func (a *API) scope(c *gin.Context) (store.AccessScope, error) {
	if cached, ok := c.Get(scopeContextKey); ok {
		if sc, ok := cached.(store.AccessScope); ok {
			return sc, nil
		}
	}

	sc, err := a.resolveScope(c)
	if err != nil {
		return store.AccessScope{}, err
	}
	c.Set(scopeContextKey, sc)
	return sc, nil
}

func (a *API) resolveScope(c *gin.Context) (store.AccessScope, error) {
	if a.Cfg == nil || !a.Cfg.HRScopeEnforced {
		return store.UnrestrictedScope(), nil
	}

	claims, ok := appmw.PlatformClaims(c)
	if !ok {
		// No verified claims reach the store layer only in non-strict modes
		// where the permission middleware already waved the request through.
		// Narrowing here would contradict that decision.
		return store.UnrestrictedScope(), nil
	}
	if claims.IsSuperuser || claims.IsStaff || claims.HasPermission(PermViewAllHR) {
		return store.UnrestrictedScope(), nil
	}

	userID, ok := appmw.UserID(c)
	if !ok {
		return store.AccessScope{}, nil
	}
	return a.Store.ResolveAccessScope(c.Request.Context(), userID)
}

// requireScope resolves the scope and writes the error response itself,
// reporting whether the handler should continue.
func (a *API) requireScope(c *gin.Context) (store.AccessScope, bool) {
	sc, err := a.scope(c)
	if err != nil {
		writeStoreError(c, err)
		return sc, false
	}
	return sc, true
}

// requireEmployeeInScope is the guard for a handler addressing one employee.
func (a *API) requireEmployeeInScope(c *gin.Context, employeeNo string) bool {
	sc, ok := a.requireScope(c)
	if !ok {
		return false
	}
	if !sc.Allows(employeeNo) {
		writeStoreError(c, store.ErrForbidden)
		return false
	}
	return true
}

// requireSelfOrUnrestricted is the guard for acts only the employee themselves
// can honestly perform — clocking in and out — where a manager's authority over
// a report is not authority to punch their card.
func (a *API) requireSelfOrUnrestricted(c *gin.Context, employeeNo string) bool {
	sc, ok := a.requireScope(c)
	if !ok {
		return false
	}
	if sc.Unrestricted || sc.IsSelf(employeeNo) {
		return true
	}
	writeStoreError(c, store.ErrForbidden)
	return false
}

// requireUnrestricted is the guard for handlers that only HR may use at all —
// creating employees, editing pay — where "your own record" is not a lesser
// version of the operation but a different one.
func (a *API) requireUnrestricted(c *gin.Context) bool {
	sc, ok := a.requireScope(c)
	if !ok {
		return false
	}
	if !sc.Unrestricted {
		writeStoreError(c, store.ErrForbidden)
		return false
	}
	return true
}
