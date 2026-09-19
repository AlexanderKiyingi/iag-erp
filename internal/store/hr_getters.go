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
	var attrs []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, name, plant_code, active, attrs, created_at
		FROM erp_departments WHERE code = $1`, code).Scan(
		&d.ID, &d.Code, &d.Name, &d.PlantCode, &d.Active, &attrs, &d.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Attrs = scanAttrs(attrs)
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
	return scanLeaveRequest(s.pool.QueryRow(ctx,
		`SELECT `+leaveRequestColumns+` `+leaveRequestFrom+` WHERE lr.id = $1`, id))
}

// leaveRequestColumns and scanLeaveRequest are shared with ListLeaveRequests.
// The department join is LEFT: an inner join used to hide the leave of every
// employee who had not been assigned a department, in the list and in the
// approval queue alike.
const leaveRequestColumns = `lr.id, lr.employee_id, e.employee_no, e.first_name || ' ' || e.last_name,
	lr.leave_type_id, lt.code, lt.name, lr.starts_on, lr.ends_on, lr.days,
	lr.reason, lr.status, lr.approver_ref, lr.decision_note, lr.decided_at,
	lr.chain_stage, lr.created_at`

const leaveRequestFrom = `FROM erp_leave_requests lr
	JOIN erp_employees e ON e.id = lr.employee_id
	LEFT JOIN erp_departments d ON d.id = e.department_id
	JOIN erp_leave_types lt ON lt.id = lr.leave_type_id`

func scanLeaveRequest(row pgx.Row) (*LeaveRequest, error) {
	var lr LeaveRequest
	err := row.Scan(&lr.ID, &lr.EmployeeID, &lr.EmployeeNo, &lr.EmployeeName,
		&lr.LeaveTypeID, &lr.LeaveTypeCode, &lr.LeaveTypeName, &lr.StartsOn, &lr.EndsOn,
		&lr.Days, &lr.Reason, &lr.Status, &lr.ApproverRef, &lr.DecisionNote,
		&lr.DecidedAt, &lr.ChainStage, &lr.CreatedAt)
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

// OperatorRosterRow is what iag-production needs of an employee to keep its
// operator registry in step with HR: no contact details, no dates, no pay.
type OperatorRosterRow struct {
	EmployeeNo  string `json:"employee_no"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	JobTitle    string `json:"job_title"`
	Status      string `json:"status"`
	PlantCode   string `json:"plant_code"`
	OperatorRef string `json:"operator_ref"`
}

// ListOperatorRoster returns every employee with a production operator ref
// — active or not, so a consumer can deactivate the ones who have left.
// Paged by employee_no for a stable walk.
func (s *Store) ListOperatorRoster(ctx context.Context, plantCode string, limit, offset int) ([]OperatorRosterRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT e.employee_no, e.first_name, e.last_name, e.job_title, e.status,
	             COALESCE(e.plant_code, ''), e.operator_ref
	      FROM erp_employees e
	      WHERE e.operator_ref IS NOT NULL AND e.operator_ref <> ''`
	args := []any{}
	if plantCode = strings.TrimSpace(plantCode); plantCode != "" {
		args = append(args, plantCode)
		q += ` AND e.plant_code = $1`
	}
	args = append(args, limit, offset)
	q += ` ORDER BY e.employee_no LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OperatorRosterRow{}
	for rows.Next() {
		var r OperatorRosterRow
		if err := rows.Scan(&r.EmployeeNo, &r.FirstName, &r.LastName, &r.JobTitle, &r.Status, &r.PlantCode, &r.OperatorRef); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
