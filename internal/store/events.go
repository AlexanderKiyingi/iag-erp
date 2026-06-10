package store

import (
	"context"

	"iag-erp/backend/internal/events"
)

type EventPublisher interface {
	Publish(ctx context.Context, eventType string, data map[string]any, key string)
}

func (s *Store) SetEventBus(bus EventPublisher) {
	s.events = bus
}

func (s *Store) emit(ctx context.Context, eventType string, data map[string]any, key string) {
	if s == nil || s.events == nil {
		return
	}
	s.events.Publish(ctx, eventType, data, key)
}

func employeeEventData(e *Employee) map[string]any {
	if e == nil {
		return map[string]any{}
	}
	data := map[string]any{
		"employee_no":      e.EmployeeNo,
		"first_name":       e.FirstName,
		"last_name":        e.LastName,
		"status":           e.Status,
		"employment_type":  e.EmploymentType,
		"job_title":        e.JobTitle,
	}
	if e.DepartmentCode != nil {
		data["department_code"] = *e.DepartmentCode
	}
	if e.PlantCode != nil {
		data["plant_code"] = *e.PlantCode
	}
	if e.OperatorRef != nil {
		data["operator_ref"] = *e.OperatorRef
	}
	if e.UserID != nil {
		data["user_id"] = e.UserID.String()
	}
	if e.ManagerEmployeeNo != nil {
		data["manager_employee_no"] = *e.ManagerEmployeeNo
	}
	return data
}

func (s *Store) emitEmployeeCreated(ctx context.Context, e *Employee) {
	s.emit(ctx, events.TypeEmployeeCreated, employeeEventData(e), e.EmployeeNo)
}

func (s *Store) emitEmployeeUpdated(ctx context.Context, e *Employee) {
	eventType := events.TypeEmployeeUpdated
	if e.Status == "terminated" {
		eventType = events.TypeEmployeeTerminated
	}
	s.emit(ctx, eventType, employeeEventData(e), e.EmployeeNo)
}

func (s *Store) emitLeaveEvent(ctx context.Context, eventType string, lr *LeaveRequest) {
	if lr == nil {
		return
	}
	s.emit(ctx, eventType, map[string]any{
		"leave_request_id": lr.ID.String(),
		"employee_no":        lr.EmployeeNo,
		"leave_type_code":    lr.LeaveTypeCode,
		"starts_on":          lr.StartsOn.Format("2006-01-02"),
		"ends_on":            lr.EndsOn.Format("2006-01-02"),
		"days":               lr.Days,
		"status":             lr.Status,
	}, lr.EmployeeNo)
}

func productionOrderEventData(po *ProductionOrder) map[string]any {
	if po == nil {
		return map[string]any{}
	}
	data := map[string]any{
		"po_num":   po.PONum,
		"customer": po.Customer,
		"product":  po.Product,
		"qty_kg":   po.QtyKg,
		"status":   po.Status,
	}
	if po.OriginLot != nil {
		data["origin_lot"] = *po.OriginLot
	}
	if po.AssetTag != nil {
		data["asset_tag"] = *po.AssetTag
	}
	if po.DueAt != nil {
		data["due_at"] = po.DueAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if po.ERPRef != nil {
		data["erp_ref"] = *po.ERPRef
	}
	return data
}

func (s *Store) emitProductionOrder(ctx context.Context, eventType string, po *ProductionOrder) {
	s.emit(ctx, eventType, productionOrderEventData(po), po.PONum)
}
