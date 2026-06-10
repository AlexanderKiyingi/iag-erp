package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"iag-erp/backend/internal/events"
)

type Department struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	PlantCode *string   `json:"plant_code,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type Employee struct {
	ID             uuid.UUID      `json:"id"`
	EmployeeNo     string         `json:"employee_no"`
	FirstName      string         `json:"first_name"`
	LastName       string         `json:"last_name"`
	Email          *string        `json:"email,omitempty"`
	Phone          *string        `json:"phone,omitempty"`
	DepartmentID   *uuid.UUID     `json:"department_id,omitempty"`
	DepartmentCode *string        `json:"department_code,omitempty"`
	DepartmentName *string        `json:"department_name,omitempty"`
	JobTitle       string         `json:"job_title"`
	EmploymentType string         `json:"employment_type"`
	Status         string         `json:"status"`
	HireDate          *time.Time     `json:"hire_date,omitempty"`
	BirthDate         *time.Time     `json:"birth_date,omitempty"`
	PlantCode         *string        `json:"plant_code,omitempty"`
	OperatorRef       *string        `json:"operator_ref,omitempty"`
	UserID            *uuid.UUID     `json:"user_id,omitempty"`
	ManagerID         *uuid.UUID     `json:"manager_id,omitempty"`
	ManagerEmployeeNo *string        `json:"manager_employee_no,omitempty"`
	Attrs             map[string]any `json:"attrs"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type LeaveType struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Paid        bool      `json:"paid"`
	DaysPerYear float64   `json:"days_per_year"`
}

type LeaveRequest struct {
	ID             uuid.UUID  `json:"id"`
	EmployeeID     uuid.UUID  `json:"employee_id"`
	EmployeeNo     string     `json:"employee_no"`
	EmployeeName   string     `json:"employee_name"`
	LeaveTypeID    uuid.UUID  `json:"leave_type_id"`
	LeaveTypeCode  string     `json:"leave_type_code"`
	LeaveTypeName  string     `json:"leave_type_name"`
	StartsOn       time.Time  `json:"starts_on"`
	EndsOn         time.Time  `json:"ends_on"`
	Days           float64    `json:"days"`
	Reason         string     `json:"reason"`
	Status         string     `json:"status"`
	ApproverRef    *string    `json:"approver_ref,omitempty"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type AttendanceRecord struct {
	ID         uuid.UUID  `json:"id"`
	EmployeeID uuid.UUID  `json:"employee_id"`
	EmployeeNo string     `json:"employee_no"`
	EmployeeName string   `json:"employee_name"`
	WorkDate   time.Time  `json:"work_date"`
	ClockIn    *time.Time `json:"clock_in,omitempty"`
	ClockOut   *time.Time `json:"clock_out,omitempty"`
	Status     string     `json:"status"`
	Notes      string     `json:"notes"`
	CreatedAt  time.Time  `json:"created_at"`
}

type HRCounts struct {
	ActiveEmployees   int `json:"active_employees"`
	OnLeaveEmployees  int `json:"on_leave_employees"`
	PendingLeave      int `json:"pending_leave"`
	Departments       int `json:"departments"`
}

func (s *Store) HRCounts(ctx context.Context) (HRCounts, error) {
	var out HRCounts
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status = 'active')::int,
		  COUNT(*) FILTER (WHERE status = 'on_leave')::int,
		  (SELECT COUNT(*)::int FROM erp_leave_requests WHERE status = 'pending'),
		  (SELECT COUNT(*)::int FROM erp_departments WHERE active = true)
		FROM erp_employees`).Scan(&out.ActiveEmployees, &out.OnLeaveEmployees, &out.PendingLeave, &out.Departments)
	return out, err
}

func (s *Store) ListDepartments(ctx context.Context, includeInactive bool) ([]Department, error) {
	q := `SELECT id, code, name, plant_code, active, created_at FROM erp_departments`
	if !includeInactive {
		q += ` WHERE active = true`
	}
	q += ` ORDER BY name`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Department
	for rows.Next() {
		var d Department
		if err := rows.Scan(&d.ID, &d.Code, &d.Name, &d.PlantCode, &d.Active, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type CreateDepartmentInput struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	PlantCode string `json:"plant_code"`
}

func (s *Store) CreateDepartment(ctx context.Context, in CreateDepartmentInput) (*Department, error) {
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if code == "" || strings.TrimSpace(in.Name) == "" {
		return nil, ErrBadInput
	}
	var d Department
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_departments (code, name, plant_code)
		VALUES ($1, $2, NULLIF($3,''))
		RETURNING id, code, name, plant_code, active, created_at`,
		code, in.Name, in.PlantCode).Scan(&d.ID, &d.Code, &d.Name, &d.PlantCode, &d.Active, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

type UpdateDepartmentInput struct {
	Name      string `json:"name"`
	PlantCode string `json:"plant_code"`
	Active    *bool  `json:"active"`
}

func (s *Store) UpdateDepartment(ctx context.Context, code string, in UpdateDepartmentInput) (*Department, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrBadInput
	}
	var d Department
	err := s.pool.QueryRow(ctx, `
		UPDATE erp_departments SET
		  name = COALESCE(NULLIF($2,''), name),
		  plant_code = CASE WHEN $3 = '' THEN plant_code ELSE NULLIF($3,'') END,
		  active = COALESCE($4, active)
		WHERE code = $1
		RETURNING id, code, name, plant_code, active, created_at`,
		code, in.Name, in.PlantCode, in.Active).Scan(&d.ID, &d.Code, &d.Name, &d.PlantCode, &d.Active, &d.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

type ListEmployeesFilter struct {
	Status         string
	DepartmentCode string
	PlantCode      string
	Search         string
	Limit          int
	Offset         int
}

func (s *Store) ListEmployees(ctx context.Context, f ListEmployeesFilter) ([]Employee, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	q := `SELECT ` + employeeColumns + ` ` + employeeFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if f.Status != "" {
		q += ` AND e.status = $` + itoa(n)
		args = append(args, f.Status)
		n++
	}
	if f.DepartmentCode != "" {
		q += ` AND d.code = $` + itoa(n)
		args = append(args, strings.ToUpper(f.DepartmentCode))
		n++
	}
	if f.PlantCode != "" {
		q += ` AND e.plant_code = $` + itoa(n)
		args = append(args, f.PlantCode)
		n++
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		q += ` AND (e.employee_no ILIKE $` + itoa(n) +
			` OR e.first_name ILIKE $` + itoa(n) +
			` OR e.last_name ILIKE $` + itoa(n) +
			` OR e.operator_ref ILIKE $` + itoa(n) + `)`
		args = append(args, "%"+search+"%")
		n++
	}
	_ = n
	q += ` ORDER BY e.last_name, e.first_name LIMIT ` + itoa(f.Limit) + ` OFFSET ` + itoa(f.Offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEmployees(rows)
}

func (s *Store) GetEmployee(ctx context.Context, employeeNo string) (*Employee, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+employeeColumns+` `+employeeFrom+` WHERE e.employee_no = $1`, employeeNo)
	return scanEmployeeRow(row)
}

type CreateEmployeeInput struct {
	EmployeeNo        string         `json:"employee_no"`
	FirstName         string         `json:"first_name"`
	LastName          string         `json:"last_name"`
	Email             string         `json:"email"`
	Phone             string         `json:"phone"`
	DepartmentCode    string         `json:"department_code"`
	JobTitle          string         `json:"job_title"`
	EmploymentType    string         `json:"employment_type"`
	HireDate          string         `json:"hire_date"`
	BirthDate         string         `json:"birth_date"`
	PlantCode         string         `json:"plant_code"`
	OperatorRef       string         `json:"operator_ref"`
	UserID            string         `json:"user_id"`
	ManagerEmployeeNo string         `json:"manager_employee_no"`
	Attrs             map[string]any `json:"attrs"`
}

func (s *Store) resolveManagerID(ctx context.Context, managerEmployeeNo string) (*uuid.UUID, error) {
	managerEmployeeNo = strings.TrimSpace(managerEmployeeNo)
	if managerEmployeeNo == "" {
		return nil, nil
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM erp_employees WHERE employee_no = $1`, managerEmployeeNo).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrBadInput
		}
		return nil, err
	}
	return &id, nil
}

func parseOptionalUserID(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, ErrBadInput
	}
	return &id, nil
}

func (s *Store) CreateEmployee(ctx context.Context, in CreateEmployeeInput) (*Employee, error) {
	if strings.TrimSpace(in.EmployeeNo) == "" || strings.TrimSpace(in.FirstName) == "" || strings.TrimSpace(in.LastName) == "" {
		return nil, ErrBadInput
	}
	var deptID *uuid.UUID
	if code := strings.TrimSpace(in.DepartmentCode); code != "" {
		var id uuid.UUID
		err := s.pool.QueryRow(ctx, `SELECT id FROM erp_departments WHERE code = $1`, strings.ToUpper(code)).Scan(&id)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, ErrBadInput
			}
			return nil, err
		}
		deptID = &id
	}
	var attrs []byte
	if in.Attrs != nil {
		attrs, _ = json.Marshal(in.Attrs)
	}
	empType := strings.ToLower(strings.TrimSpace(in.EmploymentType))
	if empType == "" {
		empType = "permanent"
	}
	var hireDate *time.Time
	if in.HireDate != "" {
		if t, err := time.Parse("2006-01-02", in.HireDate); err == nil {
			hireDate = &t
		}
	}
	var birthDate *time.Time
	if in.BirthDate != "" {
		if t, err := time.Parse("2006-01-02", in.BirthDate); err == nil {
			birthDate = &t
		}
	}
	userID, err := parseOptionalUserID(in.UserID)
	if err != nil {
		return nil, err
	}
	managerID, err := s.resolveManagerID(ctx, in.ManagerEmployeeNo)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO erp_employees (employee_no, first_name, last_name, email, phone, department_id,
		  job_title, employment_type, hire_date, birth_date, plant_code, operator_ref, user_id, manager_id, attrs)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,
		  COALESCE(NULLIF($7,''),''),$8,$9,$10,NULLIF($11,''),NULLIF($12,''),$13,$14,COALESCE($15::jsonb,'{}'))
		RETURNING id`, in.EmployeeNo, in.FirstName, in.LastName, in.Email, in.Phone, deptID,
		in.JobTitle, empType, hireDate, birthDate, in.PlantCode, in.OperatorRef, userID, managerID, attrs).Scan(&id)
	if err != nil {
		return nil, err
	}
	emp, err := s.GetEmployee(ctx, in.EmployeeNo)
	if err != nil {
		return nil, err
	}
	s.emitEmployeeCreated(ctx, emp)
	return emp, nil
}

type UpdateEmployeeInput struct {
	FirstName         string         `json:"first_name"`
	LastName          string         `json:"last_name"`
	Email             string         `json:"email"`
	Phone             string         `json:"phone"`
	DepartmentCode    string         `json:"department_code"`
	JobTitle          string         `json:"job_title"`
	EmploymentType    string         `json:"employment_type"`
	Status            string         `json:"status"`
	BirthDate         string         `json:"birth_date"`
	PlantCode         string         `json:"plant_code"`
	OperatorRef       string         `json:"operator_ref"`
	UserID            string         `json:"user_id"`
	ManagerEmployeeNo string         `json:"manager_employee_no"`
	ClearManager      bool           `json:"clear_manager"`
	ClearUserID       bool           `json:"clear_user_id"`
	Attrs             map[string]any `json:"attrs"`
}

func (s *Store) UpdateEmployee(ctx context.Context, employeeNo string, in UpdateEmployeeInput) (*Employee, error) {
	employeeNo = strings.TrimSpace(employeeNo)
	if employeeNo == "" {
		return nil, ErrBadInput
	}
	var deptID *uuid.UUID
	if code := strings.TrimSpace(in.DepartmentCode); code != "" {
		var id uuid.UUID
		err := s.pool.QueryRow(ctx, `SELECT id FROM erp_departments WHERE code = $1`, strings.ToUpper(code)).Scan(&id)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, ErrBadInput
			}
			return nil, err
		}
		deptID = &id
	}
	var attrs []byte
	if in.Attrs != nil {
		attrs, _ = json.Marshal(in.Attrs)
	}
	var birthDate *time.Time
	if in.BirthDate != "" {
		if t, err := time.Parse("2006-01-02", in.BirthDate); err == nil {
			birthDate = &t
		}
	}
	userID, err := parseOptionalUserID(in.UserID)
	if err != nil {
		return nil, err
	}
	managerID, err := s.resolveManagerID(ctx, in.ManagerEmployeeNo)
	if err != nil {
		return nil, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE erp_employees SET
		  first_name = COALESCE(NULLIF($2,''), first_name),
		  last_name = COALESCE(NULLIF($3,''), last_name),
		  email = CASE WHEN $4 = '' THEN email ELSE NULLIF($4,'') END,
		  phone = CASE WHEN $5 = '' THEN phone ELSE NULLIF($5,'') END,
		  department_id = COALESCE($6, department_id),
		  job_title = COALESCE(NULLIF($7,''), job_title),
		  employment_type = COALESCE(NULLIF($8,''), employment_type),
		  status = COALESCE(NULLIF($9,''), status),
		  birth_date = COALESCE($10, birth_date),
		  plant_code = CASE WHEN $11 = '' THEN plant_code ELSE NULLIF($11,'') END,
		  operator_ref = CASE WHEN $12 = '' THEN operator_ref ELSE NULLIF($12,'') END,
		  user_id = CASE WHEN $14 THEN NULL WHEN $13 IS NOT NULL THEN $13 ELSE user_id END,
		  manager_id = CASE WHEN $16 THEN NULL WHEN $15 IS NOT NULL THEN $15 ELSE manager_id END,
		  attrs = COALESCE($17::jsonb, attrs),
		  updated_at = NOW()
		WHERE employee_no = $1`,
		employeeNo, in.FirstName, in.LastName, in.Email, in.Phone, deptID,
		in.JobTitle, in.EmploymentType, in.Status, birthDate, in.PlantCode, in.OperatorRef, userID, in.ClearUserID,
		managerID, in.ClearManager, attrs)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	emp, err := s.GetEmployee(ctx, employeeNo)
	if err != nil {
		return nil, err
	}
	s.emitEmployeeUpdated(ctx, emp)
	return emp, nil
}

func (s *Store) ListLeaveTypes(ctx context.Context) ([]LeaveType, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, name, paid, days_per_year FROM erp_leave_types ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaveType
	for rows.Next() {
		var lt LeaveType
		if err := rows.Scan(&lt.ID, &lt.Code, &lt.Name, &lt.Paid, &lt.DaysPerYear); err != nil {
			return nil, err
		}
		out = append(out, lt)
	}
	return out, rows.Err()
}

type ListLeaveRequestsFilter struct {
	Status string
	Limit  int
	Offset int
}

func (s *Store) ListLeaveRequests(ctx context.Context, f ListLeaveRequestsFilter) ([]LeaveRequest, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	q := `
		SELECT lr.id, lr.employee_id, e.employee_no, e.first_name || ' ' || e.last_name,
		       lr.leave_type_id, lt.code, lt.name, lr.starts_on, lr.ends_on, lr.days,
		       lr.reason, lr.status, lr.approver_ref, lr.decided_at, lr.created_at
		FROM erp_leave_requests lr
		JOIN erp_employees e ON e.id = lr.employee_id
		JOIN erp_leave_types lt ON lt.id = lr.leave_type_id
		WHERE 1=1`
	args := []any{}
	if f.Status != "" {
		q += ` AND lr.status = $1`
		args = append(args, f.Status)
	}
	q += ` ORDER BY lr.created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaveRequest
	for rows.Next() {
		var lr LeaveRequest
		if err := rows.Scan(&lr.ID, &lr.EmployeeID, &lr.EmployeeNo, &lr.EmployeeName,
			&lr.LeaveTypeID, &lr.LeaveTypeCode, &lr.LeaveTypeName, &lr.StartsOn, &lr.EndsOn,
			&lr.Days, &lr.Reason, &lr.Status, &lr.ApproverRef, &lr.DecidedAt, &lr.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, rows.Err()
}

type CreateLeaveRequestInput struct {
	EmployeeNo     string  `json:"employee_no"`
	LeaveTypeCode  string  `json:"leave_type_code"`
	StartsOn       string  `json:"starts_on"`
	EndsOn         string  `json:"ends_on"`
	Days           float64 `json:"days"`
	Reason         string  `json:"reason"`
}

func (s *Store) CreateLeaveRequest(ctx context.Context, in CreateLeaveRequestInput) (*LeaveRequest, error) {
	start, err1 := time.Parse("2006-01-02", in.StartsOn)
	end, err2 := time.Parse("2006-01-02", in.EndsOn)
	if err1 != nil || err2 != nil || in.EmployeeNo == "" || in.LeaveTypeCode == "" {
		return nil, ErrBadInput
	}
	if end.Before(start) {
		return nil, ErrBadInput
	}
	var empID, ltID uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM erp_employees WHERE employee_no = $1`, in.EmployeeNo).Scan(&empID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrBadInput
		}
		return nil, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT id FROM erp_leave_types WHERE code = $1`, strings.ToUpper(in.LeaveTypeCode)).Scan(&ltID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrBadInput
		}
		return nil, err
	}
	days := in.Days
	if days <= 0 {
		days = float64(end.Sub(start).Hours()/24) + 1
	}
	overlap, err := s.HasOverlappingLeave(ctx, empID, start, end, nil)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, ErrConflict
	}
	balance, err := s.GetLeaveBalance(ctx, in.EmployeeNo, strings.ToUpper(in.LeaveTypeCode), start.Year())
	if err == nil && balance.RemainingDays < days {
		return nil, ErrBadInput
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO erp_leave_requests (employee_id, leave_type_id, starts_on, ends_on, days, reason)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id`, empID, ltID, start, end, days, in.Reason).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.getLeaveRequestByID(ctx, id)
}

func (s *Store) DecideLeaveRequest(ctx context.Context, id uuid.UUID, action, approverRef string) (*LeaveRequest, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	var status string
	switch action {
	case "approve", "approved":
		status = "approved"
	case "reject", "rejected":
		status = "rejected"
	case "cancel", "cancelled":
		status = "cancelled"
	default:
		return nil, ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var employeeID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE erp_leave_requests
		SET status = $2, approver_ref = NULLIF($3,''), decided_at = NOW()
		WHERE id = $1 AND status = 'pending'
		RETURNING employee_id`, id, status, approverRef).Scan(&employeeID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	_ = s.ReconcileEmployeeLeaveStatus(ctx, employeeID)
	lr, err := s.getLeaveRequestByID(ctx, id)
	if err != nil {
		return nil, err
	}
	switch status {
	case "approved":
		s.emitLeaveEvent(ctx, events.TypeLeaveApproved, lr)
	case "rejected":
		s.emitLeaveEvent(ctx, events.TypeLeaveRejected, lr)
	case "cancelled":
		s.emitLeaveEvent(ctx, events.TypeLeaveCancelled, lr)
	}
	return lr, nil
}

func (s *Store) CancelLeaveRequest(ctx context.Context, id uuid.UUID) (*LeaveRequest, error) {
	var employeeID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		UPDATE erp_leave_requests
		SET status = 'cancelled', decided_at = NOW()
		WHERE id = $1 AND status = 'pending'
		RETURNING employee_id`, id).Scan(&employeeID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = s.ReconcileEmployeeLeaveStatus(ctx, employeeID)
	lr, err := s.getLeaveRequestByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.emitLeaveEvent(ctx, events.TypeLeaveCancelled, lr)
	return lr, nil
}

func (s *Store) ListAttendance(ctx context.Context, workDate, plantCode string, limit, offset int) ([]AttendanceRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT a.id, a.employee_id, e.employee_no, e.first_name || ' ' || e.last_name,
		       a.work_date, a.clock_in, a.clock_out, a.status, a.notes, a.created_at
		FROM erp_attendance_records a
		JOIN erp_employees e ON e.id = a.employee_id
		WHERE 1=1`
	args := []any{}
	n := 1
	if workDate != "" {
		q += ` AND a.work_date = $` + itoa(n)
		args = append(args, workDate)
		n++
	}
	if plantCode != "" {
		q += ` AND e.plant_code = $` + itoa(n)
		args = append(args, plantCode)
		n++
	}
	_ = n
	q += ` ORDER BY a.work_date DESC, e.last_name LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttendanceRecord
	for rows.Next() {
		var a AttendanceRecord
		if err := rows.Scan(&a.ID, &a.EmployeeID, &a.EmployeeNo, &a.EmployeeName,
			&a.WorkDate, &a.ClockIn, &a.ClockOut, &a.Status, &a.Notes, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type CreateAttendanceInput struct {
	EmployeeNo string `json:"employee_no"`
	WorkDate   string `json:"work_date"`
	ClockIn    string `json:"clock_in"`
	ClockOut   string `json:"clock_out"`
	Status     string `json:"status"`
	Notes      string `json:"notes"`
}

func (s *Store) UpsertAttendance(ctx context.Context, in CreateAttendanceInput) (*AttendanceRecord, error) {
	if in.EmployeeNo == "" || in.WorkDate == "" {
		return nil, ErrBadInput
	}
	workDate, err := time.Parse("2006-01-02", in.WorkDate)
	if err != nil {
		return nil, ErrBadInput
	}
	var empID uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM erp_employees WHERE employee_no = $1`, in.EmployeeNo).Scan(&empID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrBadInput
		}
		return nil, err
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if status == "" {
		status = "present"
	}
	var clockIn, clockOut *time.Time
	if in.ClockIn != "" {
		if t, err := time.Parse(time.RFC3339, in.ClockIn); err == nil {
			clockIn = &t
		}
	}
	if in.ClockOut != "" {
		if t, err := time.Parse(time.RFC3339, in.ClockOut); err == nil {
			clockOut = &t
		}
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO erp_attendance_records (employee_id, work_date, clock_in, clock_out, status, notes)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (employee_id, work_date) DO UPDATE SET
		  clock_in = COALESCE(EXCLUDED.clock_in, erp_attendance_records.clock_in),
		  clock_out = COALESCE(EXCLUDED.clock_out, erp_attendance_records.clock_out),
		  status = EXCLUDED.status,
		  notes = EXCLUDED.notes
		RETURNING id`, empID, workDate, clockIn, clockOut, status, in.Notes).Scan(&id)
	if err != nil {
		return nil, err
	}
	items, err := s.ListAttendance(ctx, in.WorkDate, "", 200, 0)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ID == id {
			return &item, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) ClockIn(ctx context.Context, employeeNo string) (*AttendanceRecord, error) {
	now := time.Now().UTC()
	workDate := now.Truncate(24 * time.Hour)
	status := "present"
	if onLeave, _ := s.isEmployeeOnLeaveToday(ctx, employeeNo, now); onLeave {
		status = "leave"
	}
	return s.UpsertAttendance(ctx, CreateAttendanceInput{
		EmployeeNo: employeeNo,
		WorkDate:   workDate.Format("2006-01-02"),
		ClockIn:    now.Format(time.RFC3339),
		Status:     status,
	})
}

func (s *Store) ClockOut(ctx context.Context, employeeNo string) (*AttendanceRecord, error) {
	now := time.Now().UTC()
	workDate := now.Truncate(24 * time.Hour)
	return s.UpsertAttendance(ctx, CreateAttendanceInput{
		EmployeeNo: employeeNo,
		WorkDate:   workDate.Format("2006-01-02"),
		ClockOut:   now.Format(time.RFC3339),
	})
}

func (s *Store) isEmployeeOnLeaveToday(ctx context.Context, employeeNo string, day time.Time) (bool, error) {
	var onLeave bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM erp_leave_requests lr
		  JOIN erp_employees e ON e.id = lr.employee_id
		  WHERE e.employee_no = $1 AND lr.status = 'approved'
		    AND lr.starts_on <= $2::date AND lr.ends_on >= $2::date
		)`, employeeNo, day).Scan(&onLeave)
	return onLeave, err
}

func scanEmployees(rows pgx.Rows) ([]Employee, error) {
	var out []Employee
	for rows.Next() {
		var e Employee
		var attrs []byte
		if err := rows.Scan(&e.ID, &e.EmployeeNo, &e.FirstName, &e.LastName, &e.Email, &e.Phone,
			&e.DepartmentID, &e.DepartmentCode, &e.DepartmentName, &e.JobTitle, &e.EmploymentType,
			&e.Status, &e.HireDate, &e.BirthDate, &e.PlantCode, &e.OperatorRef, &e.UserID, &e.ManagerID, &e.ManagerEmployeeNo,
			&attrs, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Attrs = scanAttrs(attrs)
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanEmployeeRow(row pgx.Row) (*Employee, error) {
	var e Employee
	var attrs []byte
	err := row.Scan(&e.ID, &e.EmployeeNo, &e.FirstName, &e.LastName, &e.Email, &e.Phone,
		&e.DepartmentID, &e.DepartmentCode, &e.DepartmentName, &e.JobTitle, &e.EmploymentType,
		&e.Status, &e.HireDate, &e.BirthDate, &e.PlantCode, &e.OperatorRef, &e.UserID, &e.ManagerID, &e.ManagerEmployeeNo,
		&attrs, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.Attrs = scanAttrs(attrs)
	return &e, nil
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
