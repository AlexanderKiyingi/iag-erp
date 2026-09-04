package handlers

import (
	"net/http"
	"strings"

	"github.com/alvor-technologies/iag-platform-go/middleware"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"iag-erp/backend/internal/auditlog"
	appmw "iag-erp/backend/internal/middleware"
)

type RouterDeps struct {
	API            *API
	Audit          *auditlog.Store
	PlatformAuth   *appmw.PlatformAuth
	CORSOrigins    []string
	StrictRBAC     bool
	PayrollEnabled bool
}

func NewRouter(deps RouterDeps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(otelgin.Middleware(deps.API.Cfg.ServiceName))
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(securityHeaders())
	r.Use(corsMiddleware(deps.CORSOrigins))

	api := deps.API
	if deps.PlatformAuth != nil {
		r.Use(deps.PlatformAuth.AttachPrincipal())
	}
	r.Use(appmw.RequestAudit(deps.Audit))

	r.GET("/health", api.Health)
	r.GET("/healthz", api.Health)
	r.GET("/ready", api.Ready)

	v1 := r.Group("/api/v1")
	if deps.PlatformAuth != nil {
		v1.Use(deps.PlatformAuth.RequireAuth())
	}
	if deps.StrictRBAC {
		v1.Use(appmw.StrictRBAC())
	}
	{
		v1.GET("/platform/status", appmw.RequireStaff(), api.PlatformStatus)
		v1.GET("/bootstrap", appmw.RequirePermission("erp.view_hr_overview"), api.Bootstrap)

		v1.GET("/departments", appmw.RequirePermission("erp.view_employee"), api.ListDepartments)
		v1.GET("/departments/:code", appmw.RequirePermission("erp.view_employee"), api.GetDepartment)
		v1.POST("/departments", appmw.RequirePermission("erp.change_employee"), api.CreateDepartment)
		v1.PATCH("/departments/:code", appmw.RequirePermission("erp.change_employee"), api.UpdateDepartment)

		v1.GET("/employees", appmw.RequirePermission("erp.view_employee"), api.ListEmployees)
		v1.GET("/employees/by-operator/:ref", appmw.RequirePermission("erp.view_employee"), api.GetEmployeeByOperatorRef)
		v1.GET("/employees/by-user/:user_id", appmw.RequirePermission("erp.view_employee"), api.GetEmployeeByUserID)
		v1.POST("/employees", appmw.RequirePermission("erp.change_employee"), api.CreateEmployee)
		v1.POST("/employees/import", appmw.RequirePermission("erp.change_employee"), api.ImportEmployees)
		v1.GET("/employees/:employee_no", appmw.RequirePermission("erp.view_employee"), api.GetEmployee)
		v1.GET("/employees/:employee_no/direct-reports", appmw.RequirePermission("erp.view_employee"), api.ListDirectReports)
		v1.GET("/employees/:employee_no/leave-balance", appmw.RequirePermission("erp.view_leave"), api.GetLeaveBalance)
		v1.PATCH("/employees/:employee_no", appmw.RequirePermission("erp.change_employee"), api.UpdateEmployee)

		// Compensation is read by the employee themselves or by HR, and written
		// only by HR — the handlers narrow further than the permission does.
		v1.GET("/employees/:employee_no/compensation", appmw.RequirePermission("erp.view_compensation"), api.GetCompensation)
		v1.GET("/employees/:employee_no/compensation/history", appmw.RequirePermission("erp.view_compensation"), api.ListCompensationHistory)
		v1.PUT("/employees/:employee_no/compensation", appmw.RequirePermission("erp.change_compensation"), api.SetCompensation)

		v1.GET("/employees/:employee_no/pay-components", appmw.RequirePermission("erp.view_compensation"), api.ListEmployeePayComponents)
		v1.POST("/employees/:employee_no/pay-components", appmw.RequirePermission("erp.change_compensation"), api.AssignPayComponent)
		v1.DELETE("/employees/:employee_no/pay-components/:component_id", appmw.RequirePermission("erp.change_compensation"), api.EndPayComponent)
		v1.GET("/employees/:employee_no/payslips", appmw.RequirePermission("erp.view_payslip"), api.ListEmployeePayslips)

		v1.GET("/leave-types", appmw.RequirePermission("erp.view_leave"), api.ListLeaveTypes)
		v1.GET("/leave-requests", appmw.RequirePermission("erp.view_leave"), api.ListLeaveRequests)
		v1.POST("/leave-requests", appmw.RequirePermission("erp.change_leave"), api.CreateLeaveRequest)
		v1.POST("/leave-requests/import", appmw.RequirePermission("erp.change_leave"), api.ImportLeaveRequests)
		v1.GET("/leave-requests/:id", appmw.RequirePermission("erp.view_leave"), api.GetLeaveRequest)
		v1.PATCH("/leave-requests/:id", appmw.RequirePermission("erp.change_leave"), api.UpdateLeaveRequest)
		v1.POST("/leave-requests/:id/decide", appmw.RequireAnyPermission("erp.approve_leave", "erp.admin.read"), api.DecideLeaveRequest)
		v1.POST("/leave-requests/:id/cancel", appmw.RequirePermission("erp.change_leave"), api.CancelLeaveRequest)

		v1.GET("/attendance", appmw.RequirePermission("erp.view_attendance"), api.ListAttendance)
		v1.POST("/attendance", appmw.RequirePermission("erp.change_attendance"), api.UpsertAttendance)
		v1.POST("/attendance/import", appmw.RequirePermission("erp.change_attendance"), api.ImportAttendance)
		v1.POST("/attendance/clock-in", appmw.RequirePermission("erp.change_attendance"), api.ClockIn)
		v1.POST("/attendance/clock-out", appmw.RequirePermission("erp.change_attendance"), api.ClockOut)
		v1.DELETE("/attendance/:id", appmw.RequirePermission("erp.change_attendance"), api.DeleteAttendance)

		// Recruitment. Not employee-scoped: a candidate is not on the roster,
		// so there is no reporting line to narrow by.
		v1.GET("/recruitment/requisitions", appmw.RequirePermission("erp.view_recruitment"), api.ListRequisitions)
		v1.GET("/recruitment/requisitions/:id", appmw.RequirePermission("erp.view_recruitment"), api.GetRequisition)
		v1.POST("/recruitment/requisitions", appmw.RequirePermission("erp.change_recruitment"), api.CreateRequisition)
		v1.POST("/recruitment/requisitions/:id/status", appmw.RequirePermission("erp.change_recruitment"), api.SetRequisitionStatus)
		v1.GET("/recruitment/applications", appmw.RequirePermission("erp.view_recruitment"), api.ListApplications)
		v1.GET("/recruitment/applications/:id", appmw.RequirePermission("erp.view_recruitment"), api.GetApplication)
		v1.POST("/recruitment/applications", appmw.RequirePermission("erp.change_recruitment"), api.CreateApplication)
		v1.POST("/recruitment/applications/:id/advance", appmw.RequirePermission("erp.change_recruitment"), api.AdvanceApplication)
		// Hiring writes to the roster, so it needs the employee grant too.
		v1.POST("/recruitment/applications/:id/hire", appmw.RequirePermission("erp.change_employee"), api.HireApplicant)

		// Onboarding and offboarding.
		v1.GET("/checklist-templates", appmw.RequirePermission("erp.view_lifecycle"), api.ListChecklistTemplates)
		v1.GET("/checklists", appmw.RequirePermission("erp.view_lifecycle"), api.ListChecklists)
		v1.GET("/checklists/:id", appmw.RequirePermission("erp.view_lifecycle"), api.GetChecklist)
		v1.POST("/checklists", appmw.RequirePermission("erp.change_lifecycle"), api.IssueChecklist)
		v1.POST("/checklists/:id/complete", appmw.RequirePermission("erp.change_lifecycle"), api.CompleteChecklist)
		v1.POST("/checklists/:id/cancel", appmw.RequirePermission("erp.change_lifecycle"), api.CancelChecklist)
		// Ticking an item is done by IT and line managers, not only HR, so it
		// carries the lighter grant.
		v1.POST("/checklist-items/:item_id/status", appmw.RequirePermission("erp.complete_checklist_item"), api.SetChecklistItemStatus)

		// Performance.
		v1.GET("/performance/cycles", appmw.RequirePermission("erp.view_performance"), api.ListReviewCycles)
		v1.POST("/performance/cycles", appmw.RequirePermission("erp.manage_performance"), api.CreateReviewCycle)
		v1.POST("/performance/cycles/:id/status", appmw.RequirePermission("erp.manage_performance"), api.SetReviewCycleStatus)
		v1.GET("/performance/reviews", appmw.RequirePermission("erp.view_performance"), api.ListReviews)
		v1.GET("/performance/reviews/:id", appmw.RequirePermission("erp.view_performance"), api.GetReview)
		v1.POST("/performance/reviews", appmw.RequirePermission("erp.change_performance"), api.OpenReview)
		v1.POST("/performance/reviews/:id/advance", appmw.RequirePermission("erp.change_performance"), api.AdvanceReview)
		v1.GET("/performance/goals", appmw.RequirePermission("erp.view_performance"), api.ListGoals)
		v1.POST("/performance/goals", appmw.RequirePermission("erp.change_performance"), api.CreateGoal)
		v1.PATCH("/performance/goals/:id", appmw.RequirePermission("erp.change_performance"), api.UpdateGoal)

		// Disciplinary. Deliberately its own permission pair rather than folded
		// into the general HR grants: these records are read on a need-to-know
		// basis, not by everyone who can see a roster.
		v1.GET("/disciplinary/cases", appmw.RequirePermission("erp.view_disciplinary"), api.ListCases)
		v1.GET("/disciplinary/cases/:id", appmw.RequirePermission("erp.view_disciplinary"), api.GetCase)
		v1.POST("/disciplinary/cases", appmw.RequirePermission("erp.change_disciplinary"), api.OpenCase)
		v1.POST("/disciplinary/cases/:id/advance", appmw.RequirePermission("erp.change_disciplinary"), api.AdvanceCase)

		// Training.
		v1.GET("/training/courses", appmw.RequirePermission("erp.view_training"), api.ListCourses)
		v1.POST("/training/courses", appmw.RequirePermission("erp.change_training"), api.CreateCourse)
		v1.GET("/training/enrolments", appmw.RequirePermission("erp.view_training"), api.ListEnrolments)
		v1.POST("/training/enrolments", appmw.RequirePermission("erp.change_training"), api.Enrol)
		v1.PATCH("/training/enrolments/:id", appmw.RequirePermission("erp.change_training"), api.UpdateEnrolment)

		// The generic JSONB module store. Still serves the modules that have
		// not been promoted to a schema (shifts, helpdesk, documents, assets,
		// settings) and stays in place for the promoted ones so no frontend
		// breaks on this change.
		hrModules := v1.Group("/hr/:module")
		hrModules.GET("", appmw.RequirePermission("erp.view_hr_records"), api.ListHRModuleRecords)
		hrModules.POST("", appmw.RequirePermission("erp.change_hr_records"), api.CreateHRModuleRecord)
		hrModules.POST("/import", appmw.RequirePermission("erp.change_hr_records"), api.ImportHRModuleRecords)
		hrModules.GET("/:id", appmw.RequirePermission("erp.view_hr_records"), api.GetHRModuleRecord)
		hrModules.PATCH("/:id", appmw.RequirePermission("erp.change_hr_records"), api.UpdateHRModuleRecord)
		hrModules.DELETE("/:id", appmw.RequirePermission("erp.change_hr_records"), api.DeleteHRModuleRecord)

		// Payroll is behind PAYROLL_ENABLED. It computes statutory deductions
		// against live pay data, and it should not be reachable anywhere the
		// seeded tax bands have not been reviewed against the current gazette.
		if deps.PayrollEnabled {
			v1.GET("/payroll/components", appmw.RequirePermission("erp.view_payroll"), api.ListPayComponentDefinitions)
			v1.GET("/payroll/runs", appmw.RequirePermission("erp.view_payroll"), api.ListPayrollRuns)
			v1.GET("/payroll/runs/:id", appmw.RequirePermission("erp.view_payroll"), api.GetPayrollRun)
			v1.GET("/payroll/runs/:id/payslips", appmw.RequirePermission("erp.view_payroll"), api.ListPayslips)
			v1.POST("/payroll/runs", appmw.RequirePermission("erp.run_payroll"), api.CreatePayrollRun)
		// Inside the PayrollEnabled group on purpose: the flag also gates listing,
		// approving and posting, so a run that could be created but never read,
		// approved or posted would be a write-only hole.
		v1.POST("/payroll/external-runs", appmw.RequirePermission("erp.run_payroll"), api.RecordExternalPayrollRun)
			v1.POST("/payroll/runs/:id/cancel", appmw.RequirePermission("erp.run_payroll"), api.CancelPayrollRun)
			// Approving and posting are separate permissions from running,
			// because separation of duties enforced only inside one grant is
			// separation anybody holding that grant can undo.
			v1.POST("/payroll/runs/:id/approve", appmw.RequirePermission("erp.approve_payroll"), api.ApprovePayrollRun)
			v1.POST("/payroll/runs/:id/post", appmw.RequirePermission("erp.post_payroll"), api.PostPayrollRun)
		}

		v1.GET("/reports", appmw.RequirePermission("erp.view_hr_overview"), api.HRReport)

		v1.GET("/worksites", appmw.RequirePermission("erp.view_hr_overview"), api.ListWorksites)
		v1.GET("/setup-items", appmw.RequirePermission("erp.view_hr_records"), api.ListSetupItems)
		v1.POST("/setup-items", appmw.RequirePermission("erp.change_hr_records"), api.CreateSetupItem)
		v1.PATCH("/setup-items/:id", appmw.RequirePermission("erp.change_hr_records"), api.UpdateSetupItem)
		v1.DELETE("/setup-items/:id", appmw.RequirePermission("erp.change_hr_records"), api.DeleteSetupItem)

		v1.GET("/integrations/status", appmw.RequirePermission("erp.view_hr_overview"), api.IntegrationStatus)
		v1.POST("/integrations/production-orders/webhook", appmw.RequirePermission("erp.change_production_order"), api.ProductionOrderWebhook)
		v1.GET("/production-orders", api.ListProductionOrders)
		v1.POST("/production-orders", appmw.RequirePermission("erp.change_production_order"), api.CreateProductionOrder)
		v1.PATCH("/production-orders/:po_num", appmw.RequirePermission("erp.change_production_order"), api.UpdateProductionOrder)
		v1.DELETE("/production-orders/:po_num", appmw.RequirePermission("erp.change_production_order"), api.DeleteProductionOrder)

		admin := v1.Group("/admin")
		adminRead := admin.Group("")
		adminRead.Use(appmw.RequirePermission("erp.admin.read"))
		{
			adminRead.GET("/audit-logs", api.ListAPIAuditLogs)
			adminRead.GET("/monitoring/summary", api.MonitoringSummary)
			adminRead.GET("/config", api.AdminConfig)
			adminRead.GET("/integrations/calls", api.ListIntegrationCalls)
			adminRead.POST("/jobs/leave-reconcile", api.ReconcileLeaveStatuses)
			adminRead.POST("/jobs/birthday-reminders", api.RunBirthdayReminders)
		}
	}

	return r
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

func corsMiddleware(origins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := allowed["*"]; ok {
				c.Header("Access-Control-Allow-Origin", "*")
			} else if _, ok := allowed[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Request-Id")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
