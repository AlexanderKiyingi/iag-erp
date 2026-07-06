# Frontend integration — iag-erp

Guide for wiring HR and production-order master data via **iag-api-gateway**.

Payroll journals and payslips remain in **iag-finance** — see [`shared/services/finance/docs/PAYROLL_ERP_BOUNDARY.md`](../../../../shared/services/finance/docs/PAYROLL_ERP_BOUNDARY.md). Finance consumes the events below on **`iag.operations`** (consumer group `iag.finance.erp`).

## Base URL

| Environment | Base |
|-------------|------|
| Local gateway | `http://localhost:8080/api/v1/erp/api/v1` |
| Direct (dev) | `http://localhost:4001/api/v1` |

All requests require `Authorization: Bearer <JWT>` except `/health` and `/ready`.

## Boot sequence

1. `GET /bootstrap` — HR counts, departments, pending leave (runs leave reconcile)
2. `GET /integrations/status` — modules, Kafka topic, webhook paths, event types

## HR pane mapping

| UI concern | Endpoints |
|------------|-----------|
| Org chart / roster | `GET /employees?search=&limit=&offset=` |
| Link platform user | `PATCH /employees/:employee_no` with `user_id` (UUID from **iag-users**) |
| Resolve by login | `GET /employees/by-user/:user_id` |
| Production operator link | `GET /employees/by-operator/:ref` (`OP-001` ↔ `EMP-001`) |
| Manager hierarchy | `manager_employee_no` on create/update; `GET /employees/:employee_no/direct-reports` |
| Leave | `GET /leave-requests`, `POST /leave-requests`, `PATCH /leave-requests/:id`, `POST /leave-requests/import`, `GET .../leave-balance`, `POST .../cancel` |
| Approve leave | `POST /leave-requests/:id/decide` (`erp.approve_leave`) |
| Attendance | `GET /attendance`, `POST /attendance`, `POST /attendance/clock-in`, `POST /attendance/clock-out`, `DELETE /attendance/:id` |
| Extended HR modules | `GET|POST /hr/:module`, `GET|PATCH|DELETE /hr/:module/:id`, `POST /hr/:module/import` |
| Reports | `GET /reports?type=&department=&from=&to=` |
| Bulk import | `POST /employees/import`, `POST /leave-requests/import`, `POST /attendance/import`, `POST /hr/:module/import` |

### HR roles (iag-authentication)

| Group | Permissions |
|-------|-------------|
| `hr-officer` | `platform.access_erp` + all `erp.view_*` / `erp.change_*` / `erp.view_hr_records` / `erp.change_hr_records` |
| `hr-manager` | officer grant + `erp.approve_leave` + `erp.admin.read` |

Assign users to `hr-officer` or `hr-manager` in auth admin. Users need a token refresh after grant changes.

### HR module keys (`/hr/:module`)

`shifts`, `recruitment`, `onboarding`, `performance`, `training`, `helpdesk`, `documents`, `disciplinary`, `assets`, `offboarding`, `payroll`, `settings`

Records are stored as JSON documents shaped for the **HRMIAG** frontend (`camelCase` fields in `data`).

### HRMIAG frontend env

Copy from the HRMIAG repo `.env.example`:

```env
NEXT_PUBLIC_DATA_SOURCE=api
NEXT_PUBLIC_GATEWAY_ORIGIN=http://localhost:8080
NEXT_PUBLIC_AUTH_API_URL=http://localhost:8080/api/v1/authentication
NEXT_PUBLIC_ERP_API_URL=http://localhost:8080/api/v1/erp/api/v1
```

## Production orders

| Concern | Endpoint |
|---------|----------|
| Pull sync (production/MES jobs) | `GET /production-orders?status=&since=&limit=` |
| Admin CRUD | `POST|PATCH|DELETE /production-orders` |
| External ERP push | `POST /integrations/production-orders/webhook` |

Webhook body (upsert):

```json
{
  "action": "upsert",
  "po_num": "PO-2026-001",
  "customer": "Export Co",
  "product": "Arabica AA",
  "qty_kg": 12000,
  "status": "queued",
  "due_at": "2026-06-15T00:00:00Z"
}
```

Delete: `{ "action": "delete", "po_num": "PO-2026-001" }`

## Kafka (`iag.operations`)

| Event type | When |
|------------|------|
| `erp.employee.created` | Employee created |
| `erp.employee.updated` | Employee updated |
| `erp.employee.terminated` | Status set to `terminated` |
| `erp.leave.approved` | Leave approved |
| `erp.leave.rejected` | Leave rejected |
| `erp.leave.cancelled` | Leave cancelled |
| `erp.production_order.created` | PO created |
| `erp.production_order.updated` | PO updated |
| `erp.production_order.deleted` | PO deleted |

## Permissions

Gateway: `platform.access_erp` plus route codenames (`erp.view_employee`, `erp.change_leave`, …). See [`docs/RBAC.md`](../../../../docs/RBAC.md).

## Admin / ops

- Integration audit: `GET /admin/integrations/calls?target=external_erp`
- Leave status job: `POST /admin/jobs/leave-reconcile` or `erp-jobs -leave-reconcile`

OpenAPI: [`docs/openapi.yaml`](openapi.yaml)
