---
sidebar_position: 10
---

# Multi-tenancy

Snooze supports multiple isolated **tenants** (organizations) on a single
server. Every alert, rule, snooze filter, user, role, and notification is
scoped to exactly one tenant. Data from different tenants is never mixed at
query time.

## Key concepts

### Tenant

A tenant is identified by an immutable URL/login-safe **slug** (the `id`
field, e.g. `acme` or `ops-team`). The slug is stamped on every
tenant-scoped document as the `tenant_id` field in the database. Slugs are
lowercase alphanumeric with hyphens and underscores; they cannot be
changed after creation.

Each tenant also carries a mutable `display_name`, a `status`
(`active` or `suspended`), and a per-tenant `ingest_token` for
unauthenticated alert ingestion.

### The `default` tenant

A reserved tenant with slug `default` is created automatically at first
boot. It cannot be deleted or have its slug changed. When a login request
omits the `org` field, or when alert ingestion arrives without a recognized
ingest token, the request is served by the `default` tenant. This means
**existing single-tenant deployments continue to work without any change** —
all data is already in the `default` tenant.

### Platform scope vs tenant scope

Most permissions (`rw_record`, `ro_rule`, …) are evaluated within a tenant.
Two special **platform-tier** permissions gate the tenant registry itself:

| Permission | Grants |
|---|---|
| `ro_tenant` | Read the tenant list and individual tenant documents |
| `rw_tenant` | Full CRUD on the tenant registry |

These permissions are independent of any tenant. The seeded `platform_admin`
role holds both. A user must belong to the `platform_admin` role (in any
tenant) to manage tenants.

### Global vs tenant-scoped collections

A small set of collections are **global** (not scoped to any tenant):

| Collection | Contents |
|---|---|
| `tenant` | The tenant registry itself |
| `secrets` | Cluster JWT signing key and other secrets |
| `nodes` | Cluster node heartbeats |
| `heartbeat` | Dead-man's-switch heartbeat records |

Everything else — `record`, `rule`, `aggregaterule`, `snooze`,
`notification`, `user`, `role`, `settings`, `audit`, `comment`, `stats`,
`aggregate`, etc. — is tenant-scoped.

## Authentication and tenant selection

### Login

All login endpoints (`POST /api/v1/login/local`, `/ldap`, `/anonymous`)
accept an optional `org` field in the request body:

```json
{
  "username": "alice",
  "password": "hunter2",
  "org": "acme"
}
```

If `org` is omitted or blank, the token is scoped to `default`. An unknown
`org` value returns 401 (indistinguishable from bad credentials to prevent
tenant enumeration).

The issued JWT carries a `tenant_id` claim matching the chosen tenant slug.
Every subsequent API call is automatically scoped to that tenant by the
server-side auth middleware.

### Token refresh

`POST /api/v1/login/refresh` re-scopes the new token to the same tenant as
the original token. Refresh tokens are isolated per-tenant: a token issued
for tenant `acme` cannot be used to refresh a session in tenant `default`.

## Alert ingestion routing

Unauthenticated alert ingestion — `POST /api/v1/alerts` and all
`POST /api/v1/webhook/*` receivers — is routed to a tenant by the
per-tenant **ingest token**:

```
Authorization: Bearer <ingest_token>
```

or as a query parameter `?token=<ingest_token>`.

When no token is supplied, or the token does not match any tenant, the
request falls back to the `default` tenant. This preserves backward
compatibility for single-tenant deployments that have no ingest token
configured.

A tenant's `ingest_token` is generated at create time (or can be set
explicitly). It is returned in the `POST /api/v1/tenant` and
`GET /api/v1/tenant/{id}` responses. Rotate it with
`PATCH /api/v1/tenant/{id}` — see [Tenant management](./tenant_management.md).

## Plugin caches

Processing plugins (rule, snooze, aggregate-rule, notification) are
**process-wide singletons** with per-tenant in-memory caches. A cache
reload triggered by a change to the `rule` collection in tenant `acme`
only invalidates tenant `acme`'s rule cache — other tenants are
unaffected. This means rule/snooze changes take effect with the usual
syncer latency independently per tenant.

## Tenant lifecycle states

| Status | Logins | Ingest | Data |
|---|---|---|---|
| `active` | Allowed | Allowed | Intact |
| `suspended` | Blocked (401) | Blocked (401) | Retained |

Suspending a tenant does not evict existing sessions — tokens already
issued remain valid until they expire. Unsuspend by PATCH-ing `status`
back to `active`.

## LDAP per-tenant

LDAP configuration (bind DN, server, base DN, …) is stored in the `settings`
collection and is therefore tenant-scoped. Each tenant can have its own
LDAP server or directory. See [LDAP Authentication](../configuration/ldap_auth.md).

## Migration from single-tenant

If you are upgrading an existing single-tenant deployment, all existing data
is already in the `default` tenant and no migration is needed. Existing
clients, alert senders, and webhooks continue to work unchanged. When you
are ready to add additional tenants, see [Tenant management](./tenant_management.md).
