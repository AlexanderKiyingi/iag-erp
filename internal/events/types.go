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

	// TypePayrollRunPosted announces a payroll run that has been approved and
	// released. It carries period totals only — gross, PAYE, NSSF, other
	// deductions and net — which is exactly what finance needs to raise the
	// journal, and never a per-employee figure: what any one person is paid
	// does not belong on a topic several services read.
	TypePayrollRunPosted = "erp.payroll.run_posted"

	TypeProductionOrderCreated = "erp.production_order.created"
	TypeProductionOrderUpdated = "erp.production_order.updated"
	TypeProductionOrderDeleted = "erp.production_order.deleted"
)

func TopicForEvent(eventType string) string {
	return TopicOperations
}
