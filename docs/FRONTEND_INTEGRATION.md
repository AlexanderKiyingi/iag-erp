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
| Compensation | `GET|PUT /employees/:employee_no/compensation`, `GET .../compensation/history` |
| Pay components | `GET|POST /employees/:employee_no/pay-components`, `DELETE .../pay-components/:component_id` |
| Payslips (self-service) | `GET /employees/:employee_no/payslips` |
| Payroll | `GET|POST /payroll/runs`, `GET /payroll/runs/:id`, `GET /payroll/runs/:id/payslips`, `POST /payroll/runs/:id/{approve,post,cancel}`, `GET /payroll/components` |
| Recruitment | `GET|POST /recruitment/requisitions`, `POST /recruitment/requisitions/:id/status`, `GET|POST /recruitment/applications`, `POST /recruitment/applications/:id/{advance,hire}` |
| Onboarding / offboarding | `GET /checklist-templates`, `GET|POST /checklists`, `GET /checklists/:id`, `POST /checklists/:id/{complete,cancel}`, `POST /checklist-items/:item_id/status` |
| Performance | `GET|POST /performance/cycles`, `POST /performance/cycles/:id/status`, `GET|POST /performance/reviews`, `POST /performance/reviews/:id/advance`, `GET|POST /performance/goals`, `PATCH /performance/goals/:id` |
| Disciplinary | `GET|POST /disciplinary/cases`, `GET /disciplinary/cases/:id`, `POST /disciplinary/cases/:id/advance` |
| Training | `GET|POST /training/courses`, `GET|POST /training/enrolments`, `PATCH /training/enrolments/:id` |

### HR roles (iag-authentication)

| Group | Permissions |
|-------|-------------|
| `hr-officer` | `platform.access_erp` + all `erp.view_*` / `erp.change_*` / `erp.view_hr_records` / `erp.change_hr_records` + `erp.view_all_hr` |
| `hr-manager` | officer grant + `erp.approve_leave` + `erp.admin.read` |
| `payroll-officer` | `erp.view_all_hr` + `erp.view_compensation` + `erp.change_compensation` + `erp.view_payroll` + `erp.run_payroll` |
| `payroll-approver` | `erp.view_all_hr` + `erp.view_payroll` + `erp.approve_payroll` + `erp.post_payroll` |
| *(every employee)* | `erp.view_payslip` — reads only their own payslips |

Assign users to these groups in auth admin. Users need a token refresh after grant changes.

**`payroll-officer` and `payroll-approver` must be different people.** The service rejects an approval by the employee who prepared the run (`403 self_approval`), so granting both to one person does not defeat it — it just makes payroll unapprovable by that person.

### Record-level access (`HR_SCOPE_ENFORCED`)

A permission says which *kind* of record you may touch. Scoping says *which* records.

| Caller | Sees |
|--------|------|
| Holds `erp.view_all_hr`, or is staff/superuser | Every employee |
| Linked to an employee with reports | Themselves and their whole reporting tree |
| Linked to an employee, no reports | Themselves only |
| Not linked to any employee | Nothing (403 `out_of_scope`) |

Also enforced when the flag is on:

- **Nobody decides their own leave request** → `403 self_approval`. A manager decides for their tree; HR decides anything.
- Clock-in/clock-out are self-only. A manager corrects a report's day through `POST /attendance`.
- Creating, updating and bulk-importing employees is HR-only regardless of reporting line.
- Compensation is readable by the employee or HR, writable by HR only.

**Rollout order.** The flag defaults to `false` and existing behaviour is unchanged. Before turning it on: grant `erp.view_all_hr` to `hr-officer`/`hr-manager`, confirm HR users' `user_id` is linked on their employee record (`PATCH /employees/:employee_no`), and have them refresh their token. Turning it on first leaves HR looking at an empty roster.

New error codes the frontend should surface:

| HTTP | `code` | Meaning |
|------|--------|---------|
| 403 | `out_of_scope` | Right permission, wrong record |
| 403 | `self_approval` | You cannot approve your own leave or payroll run |
| 403 | `employee_link_required` | Your login is not linked to an employee record |
| 400 | `insufficient_leave` | The request costs more days than the balance holds |
| 400 | `no_working_days` | The dates selected are all weekend/holiday |
| 409 | `period_posted` | Payroll for that month already reached the ledger |

### Leave is charged in working days

`POST /leave-requests` and `PATCH /leave-requests/:id` now compute `days` as **working days** — weekends per `HR_WORK_WEEK` and active `holiday` setup items are excluded. The response carries both `days` (charged) and `calendar_days` (span).

- Omit `days` in the request body and the server computes it. This is the recommended call.
- Send `days` to request *fewer* (a half day: `days: 0.5`). Sending more than the range contains is `400`.
- A range containing no working day is `400 no_working_days`.

Maintain public holidays as setup items: `POST /setup-items` with `item_type: "holiday"` and `effective_date`. Uganda's 2026 holidays are seeded; the two lunar dates are marked *(confirm)* and must be checked against the gazette.

### Leave balance: two figures, not two answers

`GET /employees/:employee_no/leave-balance` returns both bases, computed from the same inputs:

| Field | Meaning |
|-------|---------|
| `entitled_days` | The leave type's full annual entitlement |
| `earned_days` | Entitlement accrued to date, pro-rated for service and probation |
| `opening_days` | Carried over from last year, capped by policy |
| `taken_days` | Approved days falling in the year |
| `remaining_days` | **Entitlement basis** — `opening + entitled − taken`. What may still be booked |
| `balance_days` | **Accrual basis** — `opening + earned − taken`. What is owed today, and the figure published to finance |

They answer different questions and will differ mid-year; that is correct, not a bug. `HR_LEAVE_CHECK_BASIS` decides which one gates a new request (`entitlement` by default). Unpaid leave accrues nothing, so its derived figures stay zero — and unpaid requests are no longer balance-checked at all, which previously made them impossible to file.

### Payroll lifecycle

`POST /payroll/runs {period: "2026-07"}` → **draft** → `POST /payroll/runs/:id/approve` → **approved** → `POST /payroll/runs/:id/post` → **posted**.

- Recomputing a period replaces its draft. A posted period is refused (`409 period_posted`).
- The draft response carries `skipped[]` — employees with no compensation record, who were left out. Show it; a run that quietly covered fewer people looks identical to a correct one.
- Posting publishes `erp.payroll.run_posted` on `iag.operations`; **iag-finance** consumes it and books the journal. Employees only ever see their own payslips, and only from posted runs.

The journal finance raises has two parts:

```
Dr  Salary & Wages Expense (5200)        gross
  Cr  PAYE Payable (2200)                      paye
  Cr  NSSF Payable (2210)                      nssf_employee
  Cr  Other Payroll Deductions (2230)          other_deductions
  Cr  Net Salaries Payable (2220)              net
Dr  Employer NSSF Contribution (5210)    nssf_employer
  Cr  NSSF Payable (2210)                      nssf_employer
```

The employer's contribution is deliberately outside the `gross = deductions + net` identity: it is a cost on top of pay, not withheld from it, so it is booked as its own balanced pair. Both NSSF halves credit the same payable because both are remitted to the same fund.

### HR module keys (`/hr/:module`) — partially superseded

`shifts`, `recruitment`, `onboarding`, `performance`, `training`, `helpdesk`, `documents`, `disciplinary`, `assets`, `offboarding`, `payroll`, `settings`

Records are stored as untyped JSON documents shaped for the **HRMIAG** frontend (`camelCase` fields in `data`). **This endpoint still works for every key and is not being removed** — but seven of the twelve now have real schemas, workflows and validation behind dedicated endpoints, and new work should use those:

| Module key | Use instead | What you gain |
|------------|-------------|---------------|
| `recruitment` | `/recruitment/*` | Requisition approval, a pipeline that cannot be skipped, candidate de-duplication by email, and `POST /applications/:id/hire` which creates the employee record |
| `onboarding`, `offboarding` | `/checklists`, `/checklist-templates` | Templated items with due dates, per-item ownership, and a completion that **refuses while required items are outstanding** |
| `performance` | `/performance/*` | Cycles, reviewers, weighted competency ratings, and a review that ends at *acknowledged by the employee* rather than *written by the manager* |
| `disciplinary` | `/disciplinary/*` | Due-process states, a full event log of who moved the case and when, and a dismissal that is refused without a recorded hearing |
| `training` | `/training/*` | Courses with validity periods, and certificate expiry derived from them — `?expiring_within_days=60` is the compliance query |
| `payroll` | `/payroll/*` | The real engine (see above) |

Still JSONB, and fine as such for now: `shifts`, `helpdesk`, `documents`, `assets`, `settings`.

Data already in `erp_hr_module_records` is left where it is. There is no automatic backfill — the shapes were free-form, so any migration would be guesswork. Move records over as the frontend adopts the typed endpoints.

### Workflow errors

The typed domains reject moves their state machine does not allow, with `409` and the vocabulary to fix it:

```json
{ "error": "application: cannot move from \"applied\" to \"offer\" (allowed: screening, rejected, withdrawn)",
  "code": "invalid_transition", "from": "applied", "to": "offer",
  "allowed": ["screening", "rejected", "withdrawn"] }
```

Other domain-specific refusals:

| HTTP | `code` | Meaning |
|------|--------|---------|
| 409 | `invalid_transition` | That move is not permitted from the current state |
| 409 | `checklist_incomplete` | Required items are still outstanding |
| 409 | `hearing_required` | A dismissal needs a recorded hearing |
| 403 | `self_case` | You cannot act on a disciplinary case about yourself |
| 403 | `acknowledge_self_only` | Only the employee acknowledges their own review |

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
| `erp.leave.balance_changed` | Entitlement earned/taken changed — the side finance accrues the liability from |
| `erp.employee.rate_changed` | Pay rate set. Carries the **derived daily rate only** — never gross or benefits |
| `erp.payroll.run_posted` | Payroll released. Period totals only; finance books the journal from it |
| `erp.production_order.created` | PO created |
| `erp.production_order.updated` | PO updated |
| `erp.production_order.deleted` | PO deleted |

## Permissions

Gateway: `platform.access_erp` plus route codenames (`erp.view_employee`, `erp.change_leave`, …). See [`docs/RBAC.md`](../../../../docs/RBAC.md).

## Admin / ops

- Integration audit: `GET /admin/integrations/calls?target=external_erp`
- Leave status job: `POST /admin/jobs/leave-reconcile` or `erp-jobs -leave-reconcile`

OpenAPI: [`docs/openapi.yaml`](openapi.yaml)
