---
sidebar_position: 2
---

# Upgrading to multi-tenancy (single-tenant → multi-tenant)

If you are running Snooze 2.x in single-tenant mode (the default before
2.2.0) and want to add additional organizations, this guide walks you
through the transition. No data migration is required — existing data is
already in the `default` tenant.

## What changes in 2.2.0

| Area | Before | After |
|---|---|---|
| Database documents | No `tenant_id` field | Every scoped document gains a `tenant_id` column/field stamped `default` on first write after upgrade |
| JWT claims | No `tenant_id` claim | New tokens carry `tenant_id: "default"` (or the chosen org). Legacy tokens without the claim are treated as `default`. |
| Login API | `{"username":…,"password":…}` | Optionally `{"username":…,"password":…,"org":"acme"}` — `org` is optional, defaults to `"default"` |
| Alert ingestion | Always `default` | Routed by per-tenant ingest token; falls back to `default` when absent |
| Permissions | `rw_all`, `ro_all`, etc. | Adds platform-tier `rw_tenant` / `ro_tenant`; `platform_admin` seeded role |

## Upgrade procedure

### 1. Run the standard upgrade

Follow the [Installation](../getting_started/installation.md) guide for
your deployment method (binary, Docker, Helm). The server performs an
automatic schema migration on startup — this adds the `tenant_id`
index/column to existing tenant-scoped collections and seeds the `default`
tenant document if it does not already exist.

### 2. Verify the default tenant exists

```bash
TOKEN=$(curl -s -X POST \
  -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"<password>"}' \
  http://localhost:5200/api/v1/login/local | jq -r .token)

curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:5200/api/v1/tenant/default | jq .
```

Expected: a `200` response with `"id": "default"`, `"status": "active"`.

### 3. Verify existing data is accessible

```bash
curl -H "Authorization: Bearer $TOKEN" \
     'http://localhost:5200/api/v1/record?limit=5' | jq .meta.total
```

All existing records remain accessible. The `tenant_id: "default"` field is
backfilled lazily on the first write after upgrade (reads still work without
the field while Postgres/Mongo/SQLite enforce no NOT-NULL constraint on it
during the transition window).

### 4. No changes needed for existing alert senders

Existing alert senders (cURL scripts, Alertmanager webhooks, Prometheus
remote-write, etc.) continue to work. All traffic without an ingest token
is automatically routed to `default`. You do not need to configure per-tenant
tokens unless you add a second tenant.

### 5. Verify existing API tokens still work

Legacy JWTs (issued before 2.2.0, no `tenant_id` claim) are accepted and
treated as `default`. Users do not need to log out and back in. When tokens
naturally expire and users log in again, the new tokens will carry
`tenant_id: "default"`.

### 6. Add tenants (optional)

If you want to onboard additional organizations, follow the
[Tenant management](../general/tenant_management.md) guide. No changes to
existing tenants or data are needed before creating the first additional
tenant.

## Rollback

If you need to roll back to a pre-2.2.0 binary:

1. The `tenant_id` column/field on existing collections is harmless to
   older code — it is an unknown field and is ignored by the 2.1.x read path.
2. The `tenant` collection is unknown to older code and is also ignored.
3. Platform permissions (`rw_tenant`/`ro_tenant`) and the `platform_admin`
   role are unknown to older code; the RBAC check falls through to the normal
   permission table and the routes are simply not mounted.

There is no destructive migration step, so a rollback is safe. Data written
after the upgrade (with `tenant_id` stamped) will be visible to older code
but without tenant isolation — treat older code as having platform-scope
access semantics.

## Data isolation for existing collections (Postgres / SQLite)

A background index is created at boot on each tenant-scoped collection:

```sql
-- Postgres / SQLite (created automatically)
CREATE INDEX IF NOT EXISTS idx_<collection>_tenant_id
    ON <collection>((data->>'tenant_id'));
```

For large collections (millions of rows), index creation may take a few
minutes. The server remains available during this time; queries are
unaffected (the index is not required for correctness, only performance).

## Frequently asked questions

**Do I need to restart after adding a tenant?**  
No. The tenant registry is a live collection backed by the syncer. A new
tenant becomes effective on all replicas within the syncer's propagation
interval (default: next heartbeat cycle, typically under a second).

**Can I rename the `default` tenant?**  
The slug `default` is immutable. The `display_name` can be changed with
`PATCH /api/v1/tenant/default` by a `platform_admin` user.

**What happens to existing LDAP configuration after the upgrade?**  
LDAP settings are stored in the `settings` collection, which is
tenant-scoped. After the upgrade all existing LDAP settings belong to
`default`. To configure per-tenant LDAP for a new tenant, log in as an
admin of that tenant and configure it in Settings → LDAP.
