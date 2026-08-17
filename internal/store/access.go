package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Record-level access.
//
// Holding a permission said which *kind* of record a caller could touch and
// never *which* records: anyone with erp.view_employee read the whole roster,
// anyone with erp.change_leave filed and cancelled leave in anyone's name, and
// anyone with erp.approve_leave approved their own request. Every HR product
// this service is measured against treats "employee sees self, manager sees
// team, HR sees all" as the floor.
//
// The permission stays the gate on the endpoint. This is the gate on the row.

// AccessScope is the set of employees one caller may act on.
type AccessScope struct {
	// Unrestricted is HR, an admin, or a superuser: every employee.
	Unrestricted bool
	// EmployeeNo is the caller's own employee record, empty if their platform
	// user was never linked to one.
	EmployeeNo string
	// TeamNos is the caller's reporting tree including themselves, ordered
	// self-first. Empty when Unrestricted.
	TeamNos []string
}

// UnrestrictedScope is the scope in force when scoping is switched off, and the
// one HR and admin callers get.
func UnrestrictedScope() AccessScope { return AccessScope{Unrestricted: true} }

// Allows reports whether this scope covers an employee.
func (a AccessScope) Allows(employeeNo string) bool {
	if a.Unrestricted {
		return true
	}
	if employeeNo == "" {
		return false
	}
	for _, no := range a.TeamNos {
		if no == employeeNo {
			return true
		}
	}
	return false
}

// IsSelf reports whether an employee is the caller themselves. Used where being
// in scope is not enough — nobody approves their own leave.
func (a AccessScope) IsSelf(employeeNo string) bool {
	return a.EmployeeNo != "" && a.EmployeeNo == employeeNo
}

// Manages reports whether an employee is someone else in the caller's tree,
// which is the authority an approval needs.
func (a AccessScope) Manages(employeeNo string) bool {
	return a.Allows(employeeNo) && !a.IsSelf(employeeNo)
}

// Filter is the employee-number allowlist to apply to a list query, or nil when
// the caller may see everything.
func (a AccessScope) Filter() []string {
	if a.Unrestricted {
		return nil
	}
	// A caller with no linked employee record sees nothing rather than
	// everything: an empty allowlist must never read as "no filter".
	if len(a.TeamNos) == 0 {
		return []string{""}
	}
	return a.TeamNos
}

// ResolveAccessScope builds the scope for a platform user: their own employee
// record plus everyone beneath them in the manager chain.
//
// The chain is walked to full depth rather than one level, because a department
// head who cannot see a report's report cannot approve the leave that escalates
// to them.
func (s *Store) ResolveAccessScope(ctx context.Context, userID uuid.UUID) (AccessScope, error) {
	var scope AccessScope

	var selfID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id, employee_no FROM erp_employees WHERE user_id = $1`, userID).
		Scan(&selfID, &scope.EmployeeNo)
	if err != nil {
		if err == pgx.ErrNoRows {
			// A platform user with no employee record is not an employee. They
			// get an empty scope, not an open one.
			return scope, nil
		}
		return scope, err
	}

	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE tree AS (
		    SELECT id, employee_no, 0 AS depth
		    FROM erp_employees WHERE id = $1
		  UNION ALL
		    SELECT e.id, e.employee_no, t.depth + 1
		    FROM erp_employees e
		    JOIN tree t ON e.manager_id = t.id
		    -- A cycle in the manager chain (A reports to B reports to A) is a
		    -- data error, not a reason to hang the request.
		    WHERE t.depth < 20
		)
		SELECT employee_no FROM tree ORDER BY depth`, selfID)
	if err != nil {
		return scope, err
	}
	defer rows.Close()
	for rows.Next() {
		var no string
		if err := rows.Scan(&no); err != nil {
			return scope, err
		}
		scope.TeamNos = append(scope.TeamNos, no)
	}
	return scope, rows.Err()
}
