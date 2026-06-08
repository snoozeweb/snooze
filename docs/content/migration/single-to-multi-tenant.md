---
sidebar_position: 2
---

# Upgrading to multi-tenancy (single-tenant → multi-tenant)

If you are running Snooze 2.x in single-tenant mode (the default before
2.2.0) and want to add additional organizations, this guide walks you
through the transition.

:::danger A one-shot data migration is required
From 2.2.0 the storage layer is **fail-closed**: every read and write is
scoped to a `tenant_id`, and a query for the `default` tenant matches only
documents whose `tenant_id` field equals `"default"`. Your existing
(pre-2.2.0) documents have **no** `tenant_id` field, so they will be
**invisible** until they are backfilled.

You **must** run `snooze-server migrate multitenancy` against the existing
database **before** starting the upgraded server. If you start the new
server first, it will treat the database as empty: it re-seeds a fresh root
user (with a new password) and the default roles, while all your historical
records, rules, snoozes, notifications, and users remain orphaned. Run the
migration first.
:::

## What changes in 2.2.0

| Area | Before | After |
|---|---|---|
| Database documents | No `tenant_id` field | Every scoped document carries a `tenant_id`; the migration stamps existing documents with `default` |
| JWT claims | No `tenant_id` claim | New tokens carry `tenant_id: "default"` (or the chosen org). Legacy tokens without the claim are treated as `default`. |
| Login API | `{"username":…,"password":…}` | Optionally `{"username":…,"password":…,"org":"acme"}` — `org` is optional, defaults to `"default"` |
| Alert ingestion | Always `default` | Routed by per-tenant ingest token; falls back to `default` when absent |
| Permissions | `rw_all`, `ro_all`, etc. | Adds platform-tier `rw_tenant` / `ro_tenant`; `platform_admin` seeded role |

## Upgrade procedure

### 1. Back up the database

The migration rewrites the primary keys of every user and role document and
stamps `tenant_id` on every scoped document. Take a backup first
(`mongodump`, `pg_dump`, or a copy of the SQLite file) so you can roll back.

### 2. Install the new binaries, but do not start the server yet

Follow the [Installation](../getting_started/installation.md) guide for your
deployment method (binary, Docker, Helm) to put the 2.2.0 binaries in place,
but **stop the running `snooze-server` (and pause alert ingestion) before the
new server boots**. The migration must run against the database while the new
server is **not** serving, so a half-started server does not re-seed over your
data.

### 3. Run the backfill migration

```bash
# Uses the same config directory as the daemon (default /etc/snooze/server-go),
# so it resolves the same database DSN/credentials the server uses.
snooze-server migrate multitenancy --config /etc/snooze/server-go
```

The command opens the configured database directly (no running server needed)
and:

1. Creates the `default` tenant registry document.
2. Creates the `platform_admin` role (`rw_tenant` + `ro_tenant`).
3. Stamps `tenant_id="default"` on every document in tenant-scoped collections.
4. Rewrites user and role primary keys to include `tenant_id`.
5. Grants the `root` user the `platform_admin` role.

It is **idempotent**: a completion sentinel is written to the `general`
collection, so re-running it is a safe no-op. On success it prints
`multitenancy migration complete`.

### 4. Start the upgraded server

Start `snooze-server` normally. Because the migration already stamped the
`init_db` marker and the root/admin users with `tenant_id="default"`, boot
finds them under the default tenant and **skips** re-seeding — your existing
root password and data are preserved.

### 5. Verify the default tenant exists

```bash
TOKEN=$(curl -s -X POST \
  -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"<password>"}' \
  http://localhost:5200/api/v1/login/local | jq -r .token)

curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:5200/api/v1/tenant/default | jq .
```

Expected: a `200` response with `"id": "default"`, `"status": "active"`.

### 6. Verify existing data is accessible

```bash
curl -H "Authorization: Bearer $TOKEN" \
     'http://localhost:5200/api/v1/record?limit=5' | jq .meta.total
```

Your existing records, rules, snoozes and notifications are accessible again
because the migration stamped them with `tenant_id="default"`. (If this count
is `0` and you expected data, the migration did not run — stop the server and
run `snooze-server migrate multitenancy` as described in step 3.)

### 7. No changes needed for existing alert senders

Existing alert senders (cURL scripts, Alertmanager webhooks, Prometheus
remote-write, etc.) continue to work. All traffic without an ingest token
is automatically routed to `default`. You do not need to configure per-tenant
tokens unless you add a second tenant.

### 8. Verify existing API tokens still work

Legacy JWTs (issued before 2.2.0, no `tenant_id` claim) are accepted and
treated as `default`. Users do not need to log out and back in. When tokens
naturally expire and users log in again, the new tokens will carry
`tenant_id: "default"`.

### 9. Add tenants (optional)

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

The migration only adds a `tenant_id` field and re-writes each document in
place under the same logical identity (user/role names are unchanged); it
deletes nothing. A rollback to the pre-2.2.0 binary is therefore safe — the
extra `tenant_id` field is simply ignored by older code. If you want a
byte-for-byte pre-migration state, restore the backup you took in step 1.

## Indexing `tenant_id` (large deployments)

The migration does **not** create a dedicated `tenant_id` index. For typical
deployments this is fine — tenant predicates ride along with the existing
search indexes. For very large collections (millions of rows) you may add one
manually after the migration:

```sql
-- Postgres
CREATE INDEX IF NOT EXISTS idx_record_tenant_id
    ON record ((data->>'tenant_id'));
```

```sql
-- SQLite
CREATE INDEX IF NOT EXISTS idx_record_tenant_id
    ON record (json_extract(data, '$.tenant_id'));
```

On MongoDB, add the field to the relevant collection index with
`db.<collection>.createIndex({ tenant_id: 1 })`.

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
