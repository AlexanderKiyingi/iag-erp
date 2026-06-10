# iag-erp

Enterprise resource planning microservice for the IAG platform — HR (employees, leave, attendance) and operational master data (production orders for MES/production sync).

| Field | Value |
|-------|-------|
| **Port** | `4001` |
| **Gateway prefix** | `/api/v1/erp` |
| **Audience** | `iag.erp` |
| **DB schema** | `erp` |
| **Remote** | [iag-erp](https://github.com/AlexanderKiyingi/iag-erp) |

## Modules

### HR (Phase 1)

| Method | Path | Permission | Description |
|--------|------|------------|-------------|
| GET | `/bootstrap` | `erp.view_hr_overview` | HR dashboard counts + pending leave |
| GET/POST | `/departments` | view/change employee | Department catalogue |
| GET | `/departments/:code` | `erp.view_employee` | Department detail |
| PATCH | `/departments/:code` | `erp.change_employee` | Update department |
| GET/POST | `/employees` | view/change employee | Employee roster (`?search`, `?limit`, `?offset`) |
| GET | `/employees/by-operator/:ref` | `erp.view_employee` | Resolve HR record from production operator ref |
| GET/PATCH | `/employees/:employee_no` | view/change employee | Employee detail |
| GET | `/employees/:employee_no/leave-balance` | `erp.view_leave` | Leave balance by type/year |
| GET | `/leave-types` | `erp.view_leave` | Leave type catalogue |
| GET/POST | `/leave-requests` | view/change leave | Leave workflow (`?status`, pagination) |
| GET | `/leave-requests/:id` | `erp.view_leave` | Leave request detail |
| POST | `/leave-requests/:id/decide` | `erp.approve_leave` | Approve/reject |
| POST | `/leave-requests/:id/cancel` | `erp.change_leave` | Employee self-cancel (pending) |
| GET/POST | `/attendance` | view/change attendance | Daily attendance upsert |
| POST | `/attendance/clock-in` | `erp.change_attendance` | Clock in now (auto `leave` if on approved leave) |
| POST | `/attendance/clock-out` | `erp.change_attendance` | Clock out now |
| GET | `/integrations/status` | `erp.view_hr_overview` | Module/integration summary |

Payroll journal posting remains in **iag-finance**; ERP owns workforce master data and leave/attendance workflow.

### Production orders

| Method | Path | Description |
|--------|------|-------------|
| GET | `/production-orders` | Pulled by **iag-production** / **iag-mes** ERP sync (`?status`, `?since`, pagination) |
| POST/PATCH | `/production-orders` | Manage orders (`erp.change_production_order`) |
| DELETE | `/production-orders/:po_num` | Remove stale orders |

### Admin / jobs

| Method | Path | Permission | Description |
|--------|------|------------|-------------|
| POST | `/admin/jobs/leave-reconcile` | `erp.admin.read` | Reconcile `active` / `on_leave` from approved leave dates |
| — | `erp-jobs -leave-reconcile` | — | Same job for Compose profile `jobs` |

### Phase C — integrations & events

| Method | Path | Permission | Description |
|--------|------|------------|-------------|
| POST | `/integrations/production-orders/webhook` | `erp.change_production_order` | External ERP push (upsert/delete) |
| GET | `/admin/integrations/calls` | `erp.admin.read` | Integration call audit log |
| GET | `/employees/by-user/:user_id` | `erp.view_employee` | Link to **iag-users** profile |
| GET | `/employees/:employee_no/direct-reports` | `erp.view_employee` | Manager hierarchy |

**Kafka** (`iag.operations`): `erp.employee.*`, `erp.leave.*`, `erp.production_order.*` — see [`docs/FRONTEND_INTEGRATION.md`](docs/FRONTEND_INTEGRATION.md).

Payroll remains in **iag-finance** (consume ERP events; no payroll API here). See [`shared/services/finance/docs/PAYROLL_ERP_BOUNDARY.md`](../../../shared/services/finance/docs/PAYROLL_ERP_BOUNDARY.md).

## Docs

- [`docs/FRONTEND_INTEGRATION.md`](docs/FRONTEND_INTEGRATION.md)
- [`docs/openapi.yaml`](docs/openapi.yaml)

## Quick start

```bash
cd services/operations/erp
cp config/.env.example .env
go run ./cmd/server
curl http://localhost:4001/health
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/erp/api/v1/bootstrap
```

Registry: [`subrepos.json`](../../../subrepos.json)
