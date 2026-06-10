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
	API          *API
	Audit        *auditlog.Store
	PlatformAuth *appmw.PlatformAuth
	CORSOrigins  []string
	StrictRBAC   bool
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
		v1.GET("/employees/:employee_no", appmw.RequirePermission("erp.view_employee"), api.GetEmployee)
		v1.GET("/employees/:employee_no/direct-reports", appmw.RequirePermission("erp.view_employee"), api.ListDirectReports)
		v1.GET("/employees/:employee_no/leave-balance", appmw.RequirePermission("erp.view_leave"), api.GetLeaveBalance)
		v1.PATCH("/employees/:employee_no", appmw.RequirePermission("erp.change_employee"), api.UpdateEmployee)

		v1.GET("/leave-types", appmw.RequirePermission("erp.view_leave"), api.ListLeaveTypes)
		v1.GET("/leave-requests", appmw.RequirePermission("erp.view_leave"), api.ListLeaveRequests)
		v1.GET("/leave-requests/:id", appmw.RequirePermission("erp.view_leave"), api.GetLeaveRequest)
		v1.POST("/leave-requests", appmw.RequirePermission("erp.change_leave"), api.CreateLeaveRequest)
		v1.POST("/leave-requests/:id/decide", appmw.RequireAnyPermission("erp.approve_leave", "erp.admin.read"), api.DecideLeaveRequest)
		v1.POST("/leave-requests/:id/cancel", appmw.RequirePermission("erp.change_leave"), api.CancelLeaveRequest)

		v1.GET("/attendance", appmw.RequirePermission("erp.view_attendance"), api.ListAttendance)
		v1.POST("/attendance", appmw.RequirePermission("erp.change_attendance"), api.UpsertAttendance)
		v1.POST("/attendance/clock-in", appmw.RequirePermission("erp.change_attendance"), api.ClockIn)
		v1.POST("/attendance/clock-out", appmw.RequirePermission("erp.change_attendance"), api.ClockOut)

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
