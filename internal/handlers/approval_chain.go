/*
The chain endpoints: one hop along the approval desks.

Which permission a hop needs depends on where it is going -- an intermediate
review is erp.run_payroll, CEO sign-off is erp.approve_payroll, the Finance
release is erp.post_payroll -- and route middleware runs before anything has
read the body. So the routes carry the permissive guard and these handlers gate
the terminal hop themselves, through middleware.AllowsPermission, which is the
same rule RequirePermission applies.

Everything else is delegated. The store decides whether a hop is legal for the
chain's shape, and the existing verbs decide whether the resulting transition is
legal for the record: self-approval, the posted-period uniqueness index, the
manager's own reporting tree.
*/
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	appmw "iag-erp/backend/internal/middleware"
	"iag-erp/backend/internal/store"
)

// bindChainHop reads and normalises the body every chain endpoint takes.
func bindChainHop(c *gin.Context) (store.ChainInput, bool) {
	var in store.ChainInput
	if err := bindJSONCoerced(c, &in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return in, false
	}
	if in.Action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action must be advance, reject or amend"})
		return in, false
	}
	return in, true
}

/*
gateChainHop refuses a hop the caller does not hold the permission for.

The refusal names the codename it wanted, because the whole point of splitting
these is that a holder of one is deliberately not a holder of the next -- and a
403 that does not say which grant is missing sends somebody to the wrong
administrator.
*/
func gateChainHop(c *gin.Context, subject store.ApprovalSubject, in store.ChainInput) bool {
	code := store.ChainPermission(subject, in.Action, in.ToStage)
	if code == "" || appmw.AllowsPermission(c, code) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{
		"error":               "permission denied: " + code,
		"required_permission": code,
	})
	return false
}

// PayrollRunChain moves a run one desk along, or rejects it, or sends it back.
func (a *API) PayrollRunChain(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	in, ok := bindChainHop(c)
	if !ok {
		return
	}
	if !gateChainHop(c, store.SubjectPayrollRun, in) {
		return
	}
	// Who acted is recorded from the session, never from the body: a trail an
	// approver can sign with somebody else's name is not a trail. ApprovePayroll
	// also refuses the preparer's own approval on this value, so accepting a
	// caller-supplied one would hand them a way around it.
	actor, ok := a.callerEmployeeNo(c)
	if !ok {
		return
	}
	in.ActorEmployeeNo = actor

	run, err := a.Store.MovePayrollRunChain(c.Request.Context(), id, in)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, run)
}

// LeaveRequestChain is the same hop for a leave request.
func (a *API) LeaveRequestChain(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	in, ok := bindChainHop(c)
	if !ok {
		return
	}
	if !gateChainHop(c, store.SubjectLeaveRequest, in) {
		return
	}

	existing, err := a.Store.GetLeaveRequest(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	sc, ok := a.requireScope(c)
	if !ok {
		return
	}
	// The same authority rule DecideLeaveRequest enforces, applied to every hop
	// rather than only the last one. A chain that let somebody advance their own
	// request to the desk before approval and only stopped them at the final
	// step would be checking the wrong thing late.
	if !sc.Unrestricted {
		if sc.IsSelf(existing.EmployeeNo) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "you cannot decide your own leave request",
				"code":  "self_approval",
			})
			return
		}
		if !sc.Manages(existing.EmployeeNo) {
			writeStoreError(c, store.ErrForbidden)
			return
		}
	}
	in.ActorEmployeeNo = a.resolveCallerEmployeeNo(c)

	item, err := a.Store.MoveLeaveRequestChain(c.Request.Context(), id, in)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	// A decided request still emails the employee. Reaching the decision through
	// the chain rather than through /decide must not change what they are told.
	if item != nil && (item.Status == "approved" || item.Status == "rejected") {
		a.notifyLeaveDecision(c, item)
	}
	c.JSON(http.StatusOK, item)
}

// ListPayrollRunApprovals returns one run's approval trail.
func (a *API) ListPayrollRunApprovals(c *gin.Context) {
	a.listApprovals(c, store.SubjectPayrollRun)
}

// ListLeaveRequestApprovals returns one leave request's approval trail.
func (a *API) ListLeaveRequestApprovals(c *gin.Context) {
	a.listApprovals(c, store.SubjectLeaveRequest)
}

func (a *API) listApprovals(c *gin.Context, subject store.ApprovalSubject) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	items, err := a.Store.ListApprovalEvents(c.Request.Context(), subject, id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "stages": store.ChainStages(subject)})
}
