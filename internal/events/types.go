package events

const (
	SpecVersion = "1.0"
	Source      = "iag-erp"

	TopicOperations = "iag.operations"

	TypeEmployeeCreated = "erp.employee.created"
	TypeEmployeeUpdated = "erp.employee.updated"
	TypeEmployeeTerminated = "erp.employee.terminated"

	TypeLeaveApproved  = "erp.leave.approved"
	TypeLeaveRejected  = "erp.leave.rejected"
	TypeLeaveCancelled = "erp.leave.cancelled"

	TypeProductionOrderCreated = "erp.production_order.created"
	TypeProductionOrderUpdated = "erp.production_order.updated"
	TypeProductionOrderDeleted = "erp.production_order.deleted"
)

func TopicForEvent(eventType string) string {
	return TopicOperations
}
