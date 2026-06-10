package store

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const employeeColumns = `e.id, e.employee_no, e.first_name, e.last_name, e.email, e.phone,
	e.department_id, d.code, d.name, e.job_title, e.employment_type, e.status,
	e.hire_date, e.birth_date, e.plant_code, e.operator_ref, e.user_id, e.manager_id, m.employee_no,
	e.attrs, e.created_at, e.updated_at`

const employeeFrom = `FROM erp_employees e
	LEFT JOIN erp_departments d ON d.id = e.department_id
	LEFT JOIN erp_employees m ON m.id = e.manager_id`

func (s *Store) GetDepartment(ctx context.Context, code string) (*Department, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	var d Department
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, name, plant_code, active, created_at
		FROM erp_departments WHERE code = $1`, code).Scan(
		&d.ID, &d.Code, &d.Name, &d.PlantCode, &d.Active, &d.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (s *Store) GetEmployeeByUserID(ctx context.Context, userID uuid.UUID) (*Employee, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+employeeColumns+` `+employeeFrom+` WHERE e.user_id = $1`, userID)
	return scanEmployeeRow(row)
}

func (s *Store) ListDirectReports(ctx context.Context, employeeNo string) ([]Employee, error) {
	employeeNo = strings.TrimSpace(employeeNo)
	if employeeNo == "" {
		return nil, ErrBadInput
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+employeeColumns+` `+employeeFrom+`
		WHERE e.manager_id = (SELECT id FROM erp_employees WHERE employee_no = $1)
		ORDER BY e.last_name, e.first_name`, employeeNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEmployees(rows)
}

func (s *Store) getLeaveRequestByID(ctx context.Context, id uuid.UUID) (*LeaveRequest, error) {
	var lr LeaveRequest
	err := s.pool.QueryRow(ctx, `
		SELECT lr.id, lr.employee_id, e.employee_no, e.first_name || ' ' || e.last_name,
		       lr.leave_type_id, lt.code, lt.name, lr.starts_on, lr.ends_on, lr.days,
		       lr.reason, lr.status, lr.approver_ref, lr.decided_at, lr.created_at
		FROM erp_leave_requests lr
		JOIN erp_employees e ON e.id = lr.employee_id
		JOIN erp_leave_types lt ON lt.id = lr.leave_type_id
		WHERE lr.id = $1`, id).Scan(
		&lr.ID, &lr.EmployeeID, &lr.EmployeeNo, &lr.EmployeeName,
		&lr.LeaveTypeID, &lr.LeaveTypeCode, &lr.LeaveTypeName, &lr.StartsOn, &lr.EndsOn,
		&lr.Days, &lr.Reason, &lr.Status, &lr.ApproverRef, &lr.DecidedAt, &lr.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &lr, nil
}

func (s *Store) GetLeaveRequest(ctx context.Context, id uuid.UUID) (*LeaveRequest, error) {
	return s.getLeaveRequestByID(ctx, id)
}

func (s *Store) GetEmployeeByOperatorRef(ctx context.Context, operatorRef string) (*Employee, error) {
	operatorRef = strings.TrimSpace(operatorRef)
	if operatorRef == "" {
		return nil, ErrBadInput
	}
	row := s.pool.QueryRow(ctx, `
		SELECT `+employeeColumns+` `+employeeFrom+` WHERE e.operator_ref = $1`, operatorRef)
	return scanEmployeeRow(row)
}
