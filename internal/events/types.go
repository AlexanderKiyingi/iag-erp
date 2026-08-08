package events

const (
	SpecVersion = "1.0"
	Source      = "iag-erp"

	TopicOperations = "iag.operations"

	TypeEmployeeCreated    = "erp.employee.created"
	TypeEmployeeUpdated    = "erp.employee.updated"
	TypeEmployeeTerminated = "erp.employee.terminated"

	TypeLeaveApproved  = "erp.leave.approved"
	TypeLeaveRejected  = "erp.leave.rejected"
	TypeLeaveCancelled = "erp.leave.cancelled"
	// TypeLeaveBalanceChanged carries the obligation side: how much leave an
	// employee has earned and not taken. The approved/rejected/cancelled events
	// above report consumption, which is the wrong side to accrue a liability
	// from.
	TypeLeaveBalanceChanged = "erp.leave.balance_changed"

	// TypeEmployeeRateChanged carries only the derived daily rate, never gross
	// or benefits: finance needs to value an obligation, not to know what
	// anyone earns, and this topic has several readers.
	TypeEmployeeRateChanged = "erp.employee.rate_changed"

	TypeProductionOrderCreated = "erp.production_order.created"
	TypeProductionOrderUpdated = "erp.production_order.updated"
	TypeProductionOrderDeleted = "erp.production_order.deleted"
)

func TopicForEvent(eventType string) string {
	return TopicOperations
}
