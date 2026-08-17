package store

import (
	"context"
	"strings"
)

type ReportRow map[string]string

type ReportResult struct {
	Type    string      `json:"type"`
	Title   string      `json:"title"`
	Summary []ReportRow `json:"summary"`
	Rows    []ReportRow `json:"rows"`
}

func (s *Store) HRReport(ctx context.Context, reportType, department, from, to string) (ReportResult, error) {
	reportType = strings.ToLower(strings.TrimSpace(reportType))
	deptCode, err := s.ResolveDepartmentFilter(ctx, department)
	if err != nil {
		return ReportResult{}, err
	}
	switch reportType {
	case "headcount":
		return s.reportHeadcount(ctx, deptCode)
	case "attendance":
		return s.reportAttendance(ctx, deptCode, from)
	case "leave":
		return s.reportLeave(ctx, deptCode, from, to)
	case "payroll":
		return s.reportPayrollModule(ctx, deptCode)
	case "recruitment":
		return s.reportHRModule(ctx, "recruitment", deptCode, "stage")
	case "performance":
		return s.reportHRModule(ctx, "performance", deptCode, "rating")
	case "training":
		return s.reportHRModule(ctx, "training", deptCode, "status")
	case "assets":
		return s.reportHRModule(ctx, "assets", deptCode, "condition")
	case "helpdesk":
		return s.reportHRModule(ctx, "helpdesk", deptCode, "status")
	default:
		return ReportResult{}, ErrBadInput
	}
}

func (s *Store) reportHeadcount(ctx context.Context, department string) (ReportResult, error) {
	f := ListEmployeesFilter{Limit: 500}
	if department != "" && !strings.EqualFold(department, "all departments") {
		f.DepartmentCode = department
	}
	emps, err := s.ListEmployees(ctx, f)
	if err != nil {
		return ReportResult{}, err
	}
	rows := make([]ReportRow, 0, len(emps))
	active, probation, onLeave := 0, 0, 0
	for _, e := range emps {
		dept := ""
		if e.DepartmentName != nil {
			dept = *e.DepartmentName
		}
		rows = append(rows, ReportRow{
			"employee_no": e.EmployeeNo,
			"name":        e.FirstName + " " + e.LastName,
			"department":  dept,
			"status":      e.Status,
			"job_title":   e.JobTitle,
		})
		switch e.Status {
		case "active":
			active++
		case "probation":
			probation++
		case "on_leave":
			onLeave++
		}
	}
	return ReportResult{
		Type:  "headcount",
		Title: "Headcount by Department",
		Summary: []ReportRow{
			{"label": "Active", "value": itoa(active)},
			{"label": "Probation", "value": itoa(probation)},
			{"label": "On leave", "value": itoa(onLeave)},
			{"label": "Total", "value": itoa(len(emps))},
		},
		Rows: rows,
	}, nil
}

func (s *Store) reportAttendance(ctx context.Context, department, workDate string) (ReportResult, error) {
	items, err := s.ListAttendance(ctx, workDate, "", department, 500, 0)
	if err != nil {
		return ReportResult{}, err
	}
	rows := make([]ReportRow, 0, len(items))
	counts := map[string]int{}
	for _, a := range items {
		rows = append(rows, ReportRow{
			"employee": a.EmployeeName,
			"date":     a.WorkDate.Format("2006-01-02"),
			"status":   a.Status,
			"location": a.Location,
			"method":   a.Method,
		})
		counts[a.Status]++
	}
	return ReportResult{
		Type:  "attendance",
		Title: "Attendance Summary",
		Summary: []ReportRow{
			{"label": "Present", "value": itoa(counts["present"])},
			{"label": "Late", "value": itoa(counts["late"])},
			{"label": "Absent", "value": itoa(counts["absent"])},
			{"label": "Total", "value": itoa(len(items))},
		},
		Rows: rows,
	}, nil
}

func (s *Store) reportLeave(ctx context.Context, department, from, to string) (ReportResult, error) {
	f := ListLeaveRequestsFilter{Limit: 500}
	if department != "" {
		f.DepartmentCode = department
	}
	if from != "" {
		f.FromDate = from
	}
	if to != "" {
		f.ToDate = to
	}
	items, err := s.ListLeaveRequests(ctx, f)
	if err != nil {
		return ReportResult{}, err
	}
	rows := make([]ReportRow, 0, len(items))
	pending, approved := 0, 0
	var totalDays float64
	for _, lr := range items {
		rows = append(rows, ReportRow{
			"employee":   lr.EmployeeName,
			"leave_type": lr.LeaveTypeName,
			"status":     lr.Status,
			"starts_on":  lr.StartsOn.Format("2006-01-02"),
			"ends_on":    lr.EndsOn.Format("2006-01-02"),
			"days":       stringifyHRField(lr.Days),
		})
		if lr.Status == "pending" {
			pending++
		}
		if lr.Status == "approved" {
			approved++
		}
		totalDays += lr.Days
	}
	return ReportResult{
		Type:  "leave",
		Title: "Leave Liability & Requests",
		Summary: []ReportRow{
			{"label": "Pending", "value": itoa(pending)},
			{"label": "Approved", "value": itoa(approved)},
			{"label": "Total days", "value": stringifyHRField(totalDays)},
			{"label": "Requests", "value": itoa(len(items))},
		},
		Rows: rows,
	}, nil
}

func (s *Store) reportPayrollModule(ctx context.Context, department string) (ReportResult, error) {
	f := ListHRModuleFilter{Module: "payroll", Limit: 500}
	if department != "" && !strings.EqualFold(department, "all departments") {
		f.Department = department
	}
	items, err := s.ListHRModuleRecords(ctx, f)
	if err != nil {
		return ReportResult{}, err
	}
	rows := make([]ReportRow, 0, len(items))
	for _, item := range items {
		rows = append(rows, item.FrontendShape())
	}
	return ReportResult{
		Type:  "payroll",
		Title: "Payroll Preparation",
		Summary: []ReportRow{
			{"label": "Payroll lines", "value": itoa(len(items))},
		},
		Rows: rows,
	}, nil
}

func (s *Store) reportHRModule(ctx context.Context, module, department, groupField string) (ReportResult, error) {
	f := ListHRModuleFilter{Module: module, Limit: 500}
	if department != "" && !strings.EqualFold(department, "all departments") {
		f.Department = department
	}
	items, err := s.ListHRModuleRecords(ctx, f)
	if err != nil {
		return ReportResult{}, err
	}
	groups := map[string]int{}
	rows := make([]ReportRow, 0, len(items))
	for _, item := range items {
		shape := item.FrontendShape()
		rows = append(rows, shape)
		if v := shape[groupField]; v != "" {
			groups[v]++
		}
	}
	summary := make([]ReportRow, 0, len(groups))
	for k, v := range groups {
		summary = append(summary, ReportRow{"label": k, "value": itoa(v)})
	}
	return ReportResult{
		Type:    module,
		Title:   module + " report",
		Summary: summary,
		Rows:    rows,
	}, nil
}
