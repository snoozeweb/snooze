---
sidebar_position: 12
---

# Tenant-aware login

The Snooze login page is always multi-tenant aware. Every login request is
scoped to a tenant, even on a single-tenant installation — the tenant is just
`default` by default and the selector stays hidden.

## The `listed` flag

Each tenant carries a boolean `listed` field (default `true`). The public
login endpoint `GET /api/v1/login` returns only **active + listed** tenants;
it never exposes the tenant list to unauthenticated visitors if every tenant
is unlisted.

The `listed` flag drives the login page behaviour:

| Active listed tenants | Login page behaviour |
|---|---|
| 0 | No organization selector — the tenant must be supplied via a login link (see below) or the `org` field in the API request body; absent `org` defaults to `default` |
| 1 | Tenant used implicitly — no selector shown |
| 2 or more | An **Organization** dropdown is shown; the user picks their organization before signing in |

### Same-org deployments (internal use)

For a company or team running Snooze for their own use, keep every tenant's
`listed` flag set to `true`. When there is more than one active tenant a
dropdown appears automatically — no extra configuration required.

### SaaS deployments (multiple unrelated customers)

If you host Snooze as a service for separate organizations, you typically do
not want one customer to see another's tenant name. Set `listed` to `false`
on every tenant and share a **per-tenant login link** (see below) with each
customer instead.

Toggle `listed` on the tenant page in the admin UI, or via the API:

```bash
curl -s -X PATCH \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"listed":false}' \
  http://localhost:5200/api/v1/tenant/acme
```

## Per-tenant login links

Every tenant has an opaque `login_key` generated automatically at creation
time. The key is a 128-bit, URL-safe random string. A user who visits:

```
https://<your-host>/web/login?key=<login_key>
```

is silently routed to that tenant's login page — the organization name is
resolved from the key and displayed as "Sign in to _\<display\_name\>_" without
ever revealing the full tenant list.

The key is a **discovery secret**: it controls which tenant's login page is
shown, but it does not grant any permissions. The user still needs valid
credentials (username/password, LDAP, or anonymous) to sign in.

:::note Security model
The login key is not an authentication token. An attacker who learns a login
key can reach the login page for that tenant, but they cannot sign in without
valid credentials. If you want to limit access to a tenant's login page,
rotate the key (see below) and distribute the new link only to authorized
users.
:::

The login link is displayed on the tenant page in the admin UI alongside a
**Copy** button.

## Rotating a login key

Rotating a key invalidates the old link immediately. Any bookmark or shared
URL containing the old key will return a generic "not found" response — it
will not reveal that the key is no longer valid, to avoid enumeration.

Rotate via the admin UI (tenant page → **Rotate** button, confirm dialog) or
via the API:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:5200/api/v1/tenant/acme/rotate-login-key | jq .
```

Response:

```json
{
  "id": "acme",
  "login_key": "BkRqv7Xt9mWpLn2AsDcF3g"
}
```

Update any login links you have distributed with the new key.

## API reference

| Endpoint | Description |
|---|---|
| `GET /api/v1/login` | Returns `{ backends, tenants }`. The `tenants` array contains only active + listed tenants; `login_key` is never included. |
| `GET /api/v1/login/tenant?key=<login_key>` | Resolves an opaque key to `{ id, display_name }`. Returns a generic 404 for unknown, empty, or suspended tenants — it never resolves by slug. |
| `POST /api/v1/tenant/{id}/rotate-login-key` | Generates a new `login_key`, invalidates the previous one, and returns `{ id, login_key }`. Requires the `rw_tenant` permission (platform admin). |

## Related pages

- [Multi-tenancy](./multitenancy.md) — conceptual overview of tenants, scoping, and permissions.
- [Tenant management](./tenant_management.md) — create, update, suspend, and delete tenants via the REST API.
