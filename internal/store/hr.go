package store

import (
	"context"
	"encoding/json"
	"math"
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
	// Attrs is HR's own record of the department — head of department, cost
	// centre, planned headcount, attachments, notes. The service does not
	// reason about any of it (it rolls up no headcount and posts to no cost
	// centre), which is exactly why it is a free map rather than five columns
	// the schema would be promising to understand. Same shape as Employee.Attrs.
	Attrs     map[string]any `json:"attrs"`
	CreatedAt time.Time      `json:"created_at"`
}

type Employee struct {
	ID                uuid.UUID      `json:"id"`
	EmployeeNo        string         `json:"employee_no"`
	FirstName         string         `json:"first_name"`
	LastName          string         `json:"last_name"`
	Email             *string        `json:"email,omitempty"`
	Phone             *string        `json:"phone,omitempty"`
	DepartmentID      *uuid.UUID     `json:"department_id,omitempty"`
	DepartmentCode    *string        `json:"department_code,omitempty"`
	DepartmentName    *string        `json:"department_name,omitempty"`
	JobTitle          string         `json:"job_title"`
	EmploymentType    string         `json:"employment_type"`
	Status            string         `json:"status"`
	HireDate          *time.Time     `json:"hire_date,omitempty"`
	BirthDate         *time.Time     `json:"birth_date,omitempty"`
	PlantCode         *string        `json:"plant_code,omitempty"`
	OperatorRef       *string        `json:"operator_ref,omitempty"`
	UserID            *uuid.UUID     `json:"user_id,omitempty"`
	ManagerID         *uuid.UUID     `json:"manager_id,omitempty"`
	ManagerEmployeeNo *string        `json:"manager_employee_no,omitempty"`
	Attrs             map[string]any `json:"attrs"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type LeaveType struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Paid        bool      `json:"paid"`
	DaysPerYear float64   `json:"days_per_year"`
}

type LeaveRequest struct {
	ID            uuid.UUID  `json:"id"`
	EmployeeID    uuid.UUID  `json:"employee_id"`
	EmployeeNo    string     `json:"employee_no"`
	EmployeeName  string     `json:"employee_name"`
	LeaveTypeID   uuid.UUID  `json:"leave_type_id"`
	LeaveTypeCode string     `json:"leave_type_code"`
	LeaveTypeName string     `json:"leave_type_name"`
	StartsOn      time.Time  `json:"starts_on"`
	EndsOn        time.Time  `json:"ends_on"`
	Days          float64    `json:"days"`
	Reason        string     `json:"reason"`
	Status        string     `json:"status"`
	ApproverRef   *string    `json:"approver_ref,omitempty"`
	// DecisionNote is why the request was decided. Every approval desk collects
	// a comment, and a rejection without one is the case where it matters most:
	// the employee is told no and cannot be told why.
	DecisionNote  string     `json:"decision_note"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
	// ChainStage is which approval desk the request is sitting at while it is
	// still pending. Empty means it never went through them.
	ChainStage    string     `json:"chain_stage"`
	CreatedAt     time.Time  `json:"created_at"`
}

type AttendanceRecord struct {
	ID           uuid.UUID  `json:"id"`
	EmployeeID   uuid.UUID  `json:"employee_id"`
	EmployeeNo   string     `json:"employee_no"`
	EmployeeName string     `json:"employee_name"`
	WorkDate     time.Time  `json:"work_date"`
	ClockIn      *time.Time `json:"clock_in,omitempty"`
	ClockOut     *time.Time `json:"clock_out,omitempty"`
	Status       string     `json:"status"`
	Location     string     `json:"location"`
	Method       string     `json:"method"`
	Notes        string     `json:"notes"`
	// Which department and block the day was worked for. A casual moved between
	// departments is exactly the row payroll has to allocate, so it belongs here
	// rather than being inferred from the employee's current department.
	DepartmentCode string   `json:"department_code"`
	Block          string   `json:"block"`
	// The geofenced check-in evidence, as columns rather than packed into notes.
	// Nullable: a punch entered by an HR officer from a paper register has no
	// GPS fix, and 0,0 is a real place rather than a way of saying "unknown".
	Latitude         *float64 `json:"latitude,omitempty"`
	Longitude        *float64 `json:"longitude,omitempty"`
	AccuracyM        *float64 `json:"accuracy_m,omitempty"`
	WifiBSSID        string   `json:"wifi_bssid"`
	VerificationNote string   `json:"verification_note"`
	// Hours is computed from the two clocks on read, never stored: a copy
	// disagrees with them the first time either is corrected.
	Hours        float64    `json:"hours"`
	CreatedAt    time.Time  `json:"created_at"`
}

type HRCounts struct {
	ActiveEmployees  int `json:"active_employees"`
	OnLeaveEmployees int `json:"on_leave_employees"`
	PendingLeave     int `json:"pending_leave"`
	Departments      int `json:"departments"`
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
	q := `SELECT id, code, name, plant_code, active, attrs, created_at FROM erp_departments`
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
		var attrs []byte
		if err := rows.Scan(&d.ID, &d.Code, &d.Name, &d.PlantCode, &d.Active, &attrs, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Attrs = scanAttrs(attrs)
		out = append(out, d)
	}
	return out, rows.Err()
}

type CreateDepartmentInput struct {
	Code      string         `json:"code"`
	Name      string         `json:"name"`
	PlantCode string         `json:"plant_code"`
	Attrs     map[string]any `json:"attrs"`
}

func (s *Store) CreateDepartment(ctx context.Context, in CreateDepartmentInput) (*Department, error) {
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if code == "" || strings.TrimSpace(in.Name) == "" {
		return nil, ErrBadInput
	}
	attrs := []byte("{}")
	if in.Attrs != nil {
		attrs, _ = json.Marshal(in.Attrs)
	}
	var d Department
	var out []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_departments (code, name, plant_code, attrs)
		VALUES ($1, $2, NULLIF($3,''), $4)
		RETURNING id, code, name, plant_code, active, attrs, created_at`,
		code, in.Name, in.PlantCode, attrs).Scan(
		&d.ID, &d.Code, &d.Name, &d.PlantCode, &d.Active, &out, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	d.Attrs = scanAttrs(out)
	return &d, nil
}

type UpdateDepartmentInput struct {
	Name      string         `json:"name"`
	PlantCode string         `json:"plant_code"`
	Active    *bool          `json:"active"`
	Attrs     map[string]any `json:"attrs"`
}

func (s *Store) UpdateDepartment(ctx context.Context, code string, in UpdateDepartmentInput) (*Department, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrBadInput
	}
	// A nil Attrs means "leave it alone", matching how every other field on this
	// update behaves. Sending an empty map would erase what HR recorded about
	// the department because a caller happened not to send the key.
	var attrs []byte
	if in.Attrs != nil {
		attrs, _ = json.Marshal(in.Attrs)
	}
	var d Department
	var out []byte
	err := s.pool.QueryRow(ctx, `
		UPDATE erp_departments SET
		  name = COALESCE(NULLIF($2,''), name),
		  plant_code = CASE WHEN $3 = '' THEN plant_code ELSE NULLIF($3,'') END,
		  active = COALESCE($4, active),
		  attrs = COALESCE($5, attrs)
		WHERE code = $1
		RETURNING id, code, name, plant_code, active, attrs, created_at`,
		code, in.Name, in.PlantCode, in.Active, attrs).Scan(
		&d.ID, &d.Code, &d.Name, &d.PlantCode, &d.Active, &out, &d.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Attrs = scanAttrs(out)
	return &d, nil
}

type ListEmployeesFilter struct {
	Status         string
	DepartmentCode string
	PlantCode      string
	Search         string
	Limit          int
	Offset         int
	// RestrictToEmployeeNos is the caller's access scope. Nil means unrestricted;
	// it is never set from a query parameter, only from AccessScope.Filter().
	RestrictToEmployeeNos []string
}

func (s *Store) ListEmployees(ctx context.Context, f ListEmployeesFilter) ([]Employee, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	q := `SELECT ` + employeeColumns + ` ` + employeeFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if len(f.RestrictToEmployeeNos) > 0 {
		q += ` AND e.employee_no = ANY($` + itoa(n) + `)`
		args = append(args, f.RestrictToEmployeeNos)
		n++
	}
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
	empType, ok := NormaliseEmploymentType(in.EmploymentType)
	if !ok {
		return nil, ErrBadInput
	}
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
	// Both columns are COALESCE(NULLIF($n,''), col), so an empty string means
	// "leave it alone" and normalising to "" is the correct no-op. An unknown
	// value is rejected here instead of violating the CHECK three frames down.
	empType, ok := NormaliseEmploymentType(in.EmploymentType)
	if !ok {
		return nil, ErrBadInput
	}
	status, ok := NormaliseEmployeeStatus(in.Status)
	if !ok {
		return nil, ErrBadInput
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
		in.JobTitle, empType, status, birthDate, in.PlantCode, in.OperatorRef, userID, in.ClearUserID,
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
	Status         string
	DepartmentCode string
	PlantCode      string
	FromDate       string
	ToDate         string
	Limit          int
	Offset         int
	// RestrictToEmployeeNos is the caller's access scope — see ListEmployeesFilter.
	RestrictToEmployeeNos []string
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
	// Same columns, joins and reader as getLeaveRequestByID — see the note on
	// leaveRequestColumns for why the department join is LEFT.
	q := `SELECT ` + leaveRequestColumns + ` ` + leaveRequestFrom + ` WHERE 1=1`
	args := []any{}
	n := 1
	if len(f.RestrictToEmployeeNos) > 0 {
		q += ` AND e.employee_no = ANY($` + itoa(n) + `)`
		args = append(args, f.RestrictToEmployeeNos)
		n++
	}
	if f.Status != "" {
		q += ` AND lr.status = $` + itoa(n)
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
	if f.FromDate != "" {
		q += ` AND lr.ends_on >= $` + itoa(n) + `::date`
		args = append(args, f.FromDate)
		n++
	}
	if f.ToDate != "" {
		q += ` AND lr.starts_on <= $` + itoa(n) + `::date`
		args = append(args, f.ToDate)
		n++
	}
	_ = n
	q += ` ORDER BY lr.created_at DESC LIMIT ` + itoa(limit) + ` OFFSET ` + itoa(offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaveRequest
	for rows.Next() {
		lr, err := scanLeaveRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *lr)
	}
	return out, rows.Err()
}

type CreateLeaveRequestInput struct {
	EmployeeNo    string  `json:"employee_no"`
	LeaveTypeCode string  `json:"leave_type_code"`
	StartsOn      string  `json:"starts_on"`
	EndsOn        string  `json:"ends_on"`
	Days          float64 `json:"days"`
	Reason        string  `json:"reason"`
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
	var paid bool
	if err := s.pool.QueryRow(ctx, `SELECT id, paid FROM erp_leave_types WHERE code = $1`,
		strings.ToUpper(in.LeaveTypeCode)).Scan(&ltID, &paid); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrBadInput
		}
		return nil, err
	}
	days, calendarDays, err := s.chargeableDays(ctx, start, end, in.Days)
	if err != nil {
		return nil, err
	}
	overlap, err := s.HasOverlappingLeave(ctx, empID, start, end, nil)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, ErrConflict
	}
	// Unpaid leave has no entitlement to exhaust, so there is no balance to
	// check against. Checking anyway compared the request to days_per_year = 0
	// and rejected every unpaid request ever made — which also made the unpaid
	// absence that payroll pro-rates pay from impossible to record.
	if paid {
		// A balance that cannot be read is not a balance of zero. Letting the
		// request through on a failed lookup is how an employee ends up owed
		// leave nobody ever accrued, so the error is returned not swallowed.
		balance, err := s.GetLeaveBalance(ctx, in.EmployeeNo, strings.ToUpper(in.LeaveTypeCode), start.Year())
		if err != nil {
			return nil, err
		}
		if balance.BookableDays(s.LeaveCheckBasis()) < days {
			return nil, ErrInsufficientLeave
		}
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO erp_leave_requests (employee_id, leave_type_id, starts_on, ends_on, days, calendar_days, reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id`, empID, ltID, start, end, days, calendarDays, in.Reason).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.getLeaveRequestByID(ctx, id)
}

// chargeableDays turns a date range into the days of entitlement it costs.
//
// The default is the working days in the range: weekends and public holidays
// are not leave. A caller may request fewer — a half day, or a partial return —
// but never more, because a request that charges days the calendar does not
// contain is how entitlement and the liability derived from it drift apart.
func (s *Store) chargeableDays(ctx context.Context, start, end time.Time, requested float64) (days, calendar float64, err error) {
	working, calendar, err := s.WorkingDaysBetween(ctx, start, end)
	if err != nil {
		return 0, 0, err
	}
	// A range made entirely of weekends and holidays costs nothing and means
	// nothing. Recording it would put a zero-day request in the approval queue
	// and in the leave report.
	if working <= 0 {
		return 0, 0, ErrNoWorkingDays
	}
	if requested <= 0 {
		return working, calendar, nil
	}
	if requested > working {
		return 0, 0, ErrBadInput
	}
	return requested, calendar, nil
}

type UpdateLeaveRequestInput struct {
	LeaveTypeCode string  `json:"leave_type_code"`
	StartsOn      string  `json:"starts_on"`
	EndsOn        string  `json:"ends_on"`
	Days          float64 `json:"days"`
	Reason        string  `json:"reason"`
}

func (s *Store) UpdateLeaveRequest(ctx context.Context, id uuid.UUID, in UpdateLeaveRequestInput) (*LeaveRequest, error) {
	existing, err := s.getLeaveRequestByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status != "pending" {
		return nil, ErrBadInput
	}
	start, err1 := time.Parse("2006-01-02", in.StartsOn)
	end, err2 := time.Parse("2006-01-02", in.EndsOn)
	if err1 != nil || err2 != nil {
		return nil, ErrBadInput
	}
	if end.Before(start) {
		return nil, ErrBadInput
	}
	ltCode := strings.ToUpper(strings.TrimSpace(in.LeaveTypeCode))
	if ltCode == "" {
		ltCode = existing.LeaveTypeCode
	}
	var ltID uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM erp_leave_types WHERE code = $1`, ltCode).Scan(&ltID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrBadInput
		}
		return nil, err
	}
	days, calendarDays, err := s.chargeableDays(ctx, start, end, in.Days)
	if err != nil {
		return nil, err
	}
	overlap, err := s.HasOverlappingLeave(ctx, existing.EmployeeID, start, end, &id)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, ErrConflict
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = existing.Reason
	}
	err = s.pool.QueryRow(ctx, `
		UPDATE erp_leave_requests SET
		  leave_type_id = $2, starts_on = $3, ends_on = $4, days = $5, calendar_days = $7, reason = $6
		WHERE id = $1 AND status = 'pending'
		RETURNING id`, id, ltID, start, end, days, reason, calendarDays).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.getLeaveRequestByID(ctx, id)
}

// DecideLeaveRequest approves, rejects or cancels a pending request.
//
// decidedByEmployeeNo is the approver as an employee, recorded alongside the
// free-text approver_ref the frontend sends. The authority check itself belongs
// to the caller, which knows the requester's place in the approver's tree; what
// is recorded here is who it was, so a decision can be answered for later.
func (s *Store) DecideLeaveRequest(ctx context.Context, id uuid.UUID, action, approverRef, note, decidedByEmployeeNo string) (*LeaveRequest, error) {
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

	var employeeID, leaveTypeID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE erp_leave_requests
		SET status = $2, approver_ref = NULLIF($3,''), decision_note = $4, decided_at = NOW(),
		    decided_by_employee_id = (
		        SELECT id FROM erp_employees WHERE employee_no = NULLIF($5,'')
		    )
		WHERE id = $1 AND status = 'pending'
		RETURNING employee_id, leave_type_id`,
		id, status, approverRef, strings.TrimSpace(note), decidedByEmployeeNo).
		Scan(&employeeID, &leaveTypeID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// The balance and the decision that moved it commit together. Finance
	// accrues its leave liability from this figure, so a decision that landed
	// without its balance would leave the obligation understated with nothing
	// to reconcile from.
	bal, err := s.RecomputeLeaveBalanceTx(ctx, tx, employeeID, leaveTypeID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := s.PublishLeaveBalanceTx(ctx, tx, bal); err != nil {
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

func (s *Store) ListAttendance(ctx context.Context, workDate, plantCode, departmentCode string, limit, offset int) ([]AttendanceRecord, error) {
	return s.ListAttendanceScoped(ctx, workDate, plantCode, departmentCode, limit, offset, nil)
}

// ListAttendanceScoped is ListAttendance narrowed to an access scope. A nil
// restriction means every employee.
func (s *Store) ListAttendanceScoped(ctx context.Context, workDate, plantCode, departmentCode string,
	limit, offset int, restrictToEmployeeNos []string) ([]AttendanceRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT a.id, a.employee_id, e.employee_no, e.first_name || ' ' || e.last_name,
		       a.work_date, a.clock_in, a.clock_out, a.status, a.location, a.method, a.notes,
		       a.department_code, a.block, a.latitude, a.longitude, a.accuracy_m,
		       a.wifi_bssid, a.verification_note, a.created_at
		FROM erp_attendance_records a
		JOIN erp_employees e ON e.id = a.employee_id
		LEFT JOIN erp_departments d ON d.id = e.department_id
		WHERE 1=1`
	args := []any{}
	n := 1
	if len(restrictToEmployeeNos) > 0 {
		q += ` AND e.employee_no = ANY($` + itoa(n) + `)`
		args = append(args, restrictToEmployeeNos)
		n++
	}
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
	if departmentCode != "" {
		q += ` AND d.code = $` + itoa(n)
		args = append(args, strings.ToUpper(departmentCode))
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
			&a.WorkDate, &a.ClockIn, &a.ClockOut, &a.Status, &a.Location, &a.Method, &a.Notes,
			&a.DepartmentCode, &a.Block, &a.Latitude, &a.Longitude, &a.AccuracyM,
			&a.WifiBSSID, &a.VerificationNote, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Hours = attendanceHours(a.ClockIn, a.ClockOut)
		out = append(out, a)
	}
	return out, rows.Err()
}

/*
attendanceHours is clock_out minus clock_in, in hours, to two decimals.

Computed on read rather than stored. A stored copy is a third number that has to
agree with the two it came from, and it stops agreeing the first time either
clock is corrected -- at which point the day's pay and the day's hours disagree
and nothing says which is right.

Zero when either clock is missing: a day with no check-out has no duration yet,
and guessing one would put hours against a shift still running.
*/
func attendanceHours(in, out *time.Time) float64 {
	if in == nil || out == nil || !out.After(*in) {
		return 0
	}
	return math.Round(out.Sub(*in).Hours()*100) / 100
}

type CreateAttendanceInput struct {
	EmployeeNo string `json:"employee_no"`
	WorkDate   string `json:"work_date"`
	ClockIn    string `json:"clock_in"`
	ClockOut   string `json:"clock_out"`
	Status     string `json:"status"`
	Location   string `json:"location"`
	Method     string `json:"method"`
	Notes      string `json:"notes"`

	DepartmentCode   string   `json:"department_code"`
	Block            string   `json:"block"`
	Latitude         *float64 `json:"latitude"`
	Longitude        *float64 `json:"longitude"`
	AccuracyM        *float64 `json:"accuracy_m"`
	WifiBSSID        string   `json:"wifi_bssid"`
	VerificationNote string   `json:"verification_note"`
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
	status, ok := NormaliseAttendanceStatus(in.Status)
	if !ok {
		return nil, ErrBadInput
	}
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
		INSERT INTO erp_attendance_records
			(employee_id, work_date, clock_in, clock_out, status, location, method, notes,
			 department_code, block, latitude, longitude, accuracy_m, wifi_bssid, verification_note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (employee_id, work_date) DO UPDATE SET
		  clock_in = COALESCE(EXCLUDED.clock_in, erp_attendance_records.clock_in),
		  clock_out = COALESCE(EXCLUDED.clock_out, erp_attendance_records.clock_out),
		  status = EXCLUDED.status,
		  location = COALESCE(NULLIF(EXCLUDED.location, ''), erp_attendance_records.location),
		  method = COALESCE(NULLIF(EXCLUDED.method, ''), erp_attendance_records.method),
		  notes = EXCLUDED.notes,
		  department_code = COALESCE(NULLIF(EXCLUDED.department_code, ''), erp_attendance_records.department_code),
		  block = COALESCE(NULLIF(EXCLUDED.block, ''), erp_attendance_records.block),
		  -- The check-out punch carries its own fix, and it is the later of the
		  -- two, so it replaces. A row upserted without one keeps what it had
		  -- rather than losing the check-in's evidence.
		  latitude = COALESCE(EXCLUDED.latitude, erp_attendance_records.latitude),
		  longitude = COALESCE(EXCLUDED.longitude, erp_attendance_records.longitude),
		  accuracy_m = COALESCE(EXCLUDED.accuracy_m, erp_attendance_records.accuracy_m),
		  wifi_bssid = COALESCE(NULLIF(EXCLUDED.wifi_bssid, ''), erp_attendance_records.wifi_bssid),
		  verification_note = COALESCE(NULLIF(EXCLUDED.verification_note, ''), erp_attendance_records.verification_note)
		RETURNING id`,
		empID, workDate, clockIn, clockOut, status, in.Location, in.Method, in.Notes,
		strings.ToUpper(strings.TrimSpace(in.DepartmentCode)), strings.TrimSpace(in.Block),
		in.Latitude, in.Longitude, in.AccuracyM,
		strings.TrimSpace(in.WifiBSSID), strings.TrimSpace(in.VerificationNote)).Scan(&id)
	if err != nil {
		return nil, err
	}
	items, err := s.ListAttendance(ctx, in.WorkDate, "", "", 200, 0)
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

func (s *Store) DeleteAttendance(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM erp_attendance_records WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanEmployeeRow is the single reader for employeeColumns.
//
// It used to be written twice — once here and once inside scanEmployees — so
// the twenty-two destinations behind a six-call-site column list had to be kept
// in step by hand. Scan is variadic, so a SELECT that gained a column and only
// one reader that was updated is a defect no compiler and no type check sees:
// the fields simply fill from the wrong columns, or the scan fails at runtime.
// pgx.Rows satisfies pgx.Row, so the list read goes through this too.
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

func scanEmployees(rows pgx.Rows) ([]Employee, error) {
	var out []Employee
	for rows.Next() {
		e, err := scanEmployeeRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
