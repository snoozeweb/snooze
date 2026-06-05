---
sidebar_position: 11
---

# Tenant management

This page covers the operator workflow for creating, inspecting, updating,
and deleting tenants via the REST API and the `snooze tenant` CLI. All
operations require the `platform_admin` role (which holds `rw_tenant` /
`ro_tenant`). The root user has this role by default.

See [Multi-tenancy](./multitenancy.md) for the conceptual overview.

## Prerequisites

Obtain a token for a user holding the `platform_admin` role:

```bash
TOKEN=$(curl -s -X POST \
  -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"<password>"}' \
  http://localhost:5200/api/v1/login/local | jq -r .token)
```

## List tenants

```bash
curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:5200/api/v1/tenant | jq .
```

Response:
```json
{
  "data": [
    {
      "id": "default",
      "display_name": "Default",
      "status": "active",
      "ingest_token": "a3f8b2c1...",
      "created_at": 1717545600
    }
  ],
  "meta": {"count": 1, "total": 1}
}
```

Note: the list returns the full tenant documents, including `ingest_token`.
The response is unpaginated — `meta` reports `count` (items returned) and
`total` (items matched), and every tenant is returned in a single call.

CLI equivalent:
```bash
snooze tenant list
```

## Create a tenant

```bash
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"id":"acme","display_name":"ACME Corp"}' \
  http://localhost:5200/api/v1/tenant | jq .
```

Response (excerpt):
```json
{
  "data": {
    "id": "acme",
    "display_name": "ACME Corp",
    "status": "active",
    "ingest_token": "a3f8b2c1...",
    "created_at": 1717545700
  }
}
```

Store the `ingest_token` value securely — it is only returned in full on the
create response and on a subsequent `GET /api/v1/tenant/{id}`.

CLI equivalent:
```bash
snooze tenant create --id acme --display-name "ACME Corp"
```

### Slug rules

The `id` slug must:

- Contain only lowercase letters, digits, and hyphens (`-`).
- Start and end with a letter or digit.

A single-character slug (e.g. `a`) is valid. The reserved `default` tenant
already exists, so attempting to create it again is rejected as a duplicate.

### Seeded roles

On creation, three default roles are seeded inside the new tenant:

| Role | Permissions |
|---|---|
| `admin` | `rw_all` |
| `viewer` | `ro_all` |
| `notifications` | `rw_notification`, `ro_all` |

Add users via `POST /api/v1/user` with a bearer token scoped to that tenant
(log in with `"org": "acme"` first).

## Get a single tenant

```bash
curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:5200/api/v1/tenant/acme | jq .
```

There is no `snooze tenant get` CLI subcommand — the `GET /api/v1/tenant/{id}`
REST call above is the only way to fetch a single tenant (including its
`ingest_token`).

## Update a tenant

Only the fields supplied are updated. `id` cannot be changed.

### Rename

```bash
curl -s -X PATCH \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"display_name":"ACME Corporation"}' \
  http://localhost:5200/api/v1/tenant/acme | jq .
```

### Rotate the ingest token

```bash
NEW_TOKEN=$(openssl rand -hex 32)
curl -s -X PATCH \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"ingest_token\":\"$NEW_TOKEN\"}" \
  http://localhost:5200/api/v1/tenant/acme | jq .
```

Update every alert sender to use the new token. The old token is
immediately invalidated — in-flight requests that arrived before the
PATCH may still use the old token within the current request lifecycle.

### Suspend a tenant

```bash
curl -s -X PATCH \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"status":"suspended"}' \
  http://localhost:5200/api/v1/tenant/acme
```

After suspension:
- New logins with `"org":"acme"` return 401.
- Alert ingestion for tenant `acme`'s ingest token returns 401.
- Existing valid JWTs continue to work until they expire.
- All data is retained and visible to platform admins.

Unsuspend with `"status":"active"`.

There is no `snooze tenant update` CLI subcommand — all updates (rename,
token rotation, suspend/unsuspend) go through `PATCH /api/v1/tenant/{id}`
as shown above.

## Delete a tenant

```bash
curl -s -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:5200/api/v1/tenant/acme
```

This removes only the tenant registry document. Tenant-scoped data (`record`,
`rule`, `snooze`, `user`, etc.) is **not** cascade-deleted. Run a separate
data-purge pass before or after deleting the tenant document if you need to
reclaim storage:

```bash
# Example: purge all records for tenant acme (MongoDB)
# Run this BEFORE deleting the tenant if you want a scoped query.
# You will need platform scope on the session or direct DB access.
db.record.deleteMany({tenant_id: "acme"})
```

The `default` tenant is protected. A DELETE request for it returns 409.

CLI equivalent:
```bash
snooze tenant delete acme
```

## Logging in as a tenant user

Users are tenant-scoped. To log in as a user in a non-default tenant,
supply the `org` field:

```bash
TOKEN=$(curl -s -X POST \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"hunter2","org":"acme"}' \
  http://localhost:5200/api/v1/login/local | jq -r .token)
```

All subsequent API calls with this token operate within tenant `acme`.
