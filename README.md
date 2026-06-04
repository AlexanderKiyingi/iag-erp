# iag-erp

Enterprise resource planning microservice for the IAG platform — planned home for cross-module operational planning (orders, capacity, master data sync).

| Field | Value |
|-------|-------|
| **Port** | `4001` |
| **Status** | Scaffold |
| **Remote** | [iag-erp](https://github.com/AlexanderKiyingi/iag-erp) |

## Planned role

Unified ERP views and workflows that span operations services (inventory, procurement, MES, finance). Will follow platform patterns: gateway JWT (`aud=iag.erp`), Postgres schema, Kafka on `iag.operations`, permission registration with `iag-authentication`.

## Quick start

```bash
cd services/operations/erp
# implementation pending — see sibling services (procurement, fleet) for patterns
```

Registry: [`subrepos.json`](../../../subrepos.json)
