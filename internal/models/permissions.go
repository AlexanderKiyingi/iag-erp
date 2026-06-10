package models

type PermissionDescriptor struct {
	Name        string
	Description string
}

func PermissionDescriptors() []PermissionDescriptor {
	return []PermissionDescriptor{
		{"erp.view_hr_overview", "HR dashboard and bootstrap"},
		{"erp.view_employee", "View employees and departments"},
		{"erp.change_employee", "Create or update employees and departments"},
		{"erp.view_leave", "View leave types and requests"},
		{"erp.change_leave", "Submit or cancel leave requests"},
		{"erp.approve_leave", "Approve or reject leave requests"},
		{"erp.view_attendance", "View attendance records"},
		{"erp.change_attendance", "Record or update attendance"},
		{"erp.view_production_order", "View ERP production orders"},
		{"erp.change_production_order", "Create or update production orders"},
		{"erp.admin.read", "View admin audit logs and monitoring"},
		{"audit.view_api_log", "Read HTTP audit entries"},
	}
}
