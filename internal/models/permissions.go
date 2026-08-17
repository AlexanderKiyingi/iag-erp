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
		{"erp.view_all_hr", "See every employee, not only your own reporting tree"},
		{"erp.view_compensation", "View pay rates and pay components"},
		{"erp.change_compensation", "Set pay rates and assign pay components"},
		{"erp.view_payslip", "View payslips (own unless combined with erp.view_all_hr)"},
		{"erp.view_payroll", "View payroll runs and payslips"},
		{"erp.run_payroll", "Compute and cancel payroll runs"},
		{"erp.approve_payroll", "Approve a payroll run prepared by someone else"},
		{"erp.post_payroll", "Release an approved payroll run to the ledger"},
		{"erp.view_recruitment", "View job requisitions, candidates and applications"},
		{"erp.change_recruitment", "Raise requisitions and move applicants through the pipeline"},
		{"erp.view_lifecycle", "View onboarding and offboarding checklists"},
		{"erp.change_lifecycle", "Issue, complete and cancel checklists"},
		{"erp.complete_checklist_item", "Tick off checklist items assigned to you or your team"},
		{"erp.view_performance", "View review cycles, reviews and goals"},
		{"erp.change_performance", "Write reviews and goals"},
		{"erp.manage_performance", "Open and close review cycles"},
		{"erp.view_disciplinary", "View disciplinary cases (need-to-know)"},
		{"erp.change_disciplinary", "Open and progress disciplinary cases"},
		{"erp.view_training", "View training courses and enrolments"},
		{"erp.change_training", "Manage courses and enrolments"},
		{"erp.view_hr_records", "View extended HR module records (shifts, helpdesk, assets, etc.)"},
		{"erp.change_hr_records", "Create or update extended HR module records"},
		{"erp.view_production_order", "View ERP production orders"},
		{"erp.change_production_order", "Create or update production orders"},
		{"erp.admin.read", "View admin audit logs and monitoring"},
		{"audit.view_api_log", "Read HTTP audit entries"},
	}
}
