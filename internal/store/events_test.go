package store

import "testing"

func TestEmployeeEventData_forFinanceConsumer(t *testing.T) {
	e := &Employee{EmployeeNo: "EMP-001", FirstName: "Jane", LastName: "Doe", Status: "active"}
	data := employeeEventData(e)
	for _, key := range []string{"employee_no", "first_name", "last_name", "status"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("missing key %q in employee event payload", key)
		}
	}
}

func TestLeaveEventPayloadKeys(t *testing.T) {
	lr := &LeaveRequest{
		EmployeeNo:    "EMP-001",
		LeaveTypeCode: "ANNUAL",
		Days:          3,
		Status:        "approved",
	}
	data := map[string]any{
		"leave_request_id": "550e8400-e29b-41d4-a716-446655440000",
		"employee_no":        lr.EmployeeNo,
		"leave_type_code":    lr.LeaveTypeCode,
		"starts_on":          "2026-06-01",
		"ends_on":            "2026-06-03",
		"days":               lr.Days,
		"status":             lr.Status,
	}
	for _, key := range []string{"leave_request_id", "employee_no", "leave_type_code", "starts_on", "ends_on", "days", "status"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("missing key %q", key)
		}
	}
}
