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

## Automatic organization routing

Operators can spare users from ever knowing or typing their organization slug
by defining a global **`tenant_match`** registry. Each rule maps one identity
attribute to a target tenant. When an SSO (OIDC/SAML) or LDAP user authenticates
**without an explicit, non-default `org`**, the login flow consults the registry
and routes the user to the matched tenant automatically.

A rule document:

```json
{
  "match_type": "group",   // "group" | "domain" | "login"
  "match":      "ops-team", // the literal value compared (case-insensitive)
  "tenant_id":  "acme",     // slug of the target tenant (must already exist)
  "priority":   0           // lower = evaluated first; ties broken by uid
}
```

The three match types:

| `match_type` | Fires when | Source |
|---|---|---|
| `group` | any of the user's IdP/LDAP groups equals `match` (case-insensitive) | OIDC `groups`/`roles` claims, LDAP `memberOf`, SAML group attribute |
| `domain` | the user's email ends with `@<match>` (case-insensitive) | OIDC `email` claim, LDAP email attribute |
| `login` | the username equals `match` (case-insensitive) | the authenticated username |

Rules are evaluated in `(priority ASC, uid ASC)` order; the **first hit wins**.
To express the conventional group → domain → login precedence, give group rules
a lower `priority` than domain rules, and domain rules a lower priority than
login rules.

`match_type` + `match` is the effective unique key: two rules for the same pair
are rejected. `tenant_id` must name an existing tenant (referential integrity is
checked on write). The collection is **global** — only platform operators with
the `rw_tenant` permission may manage it (`GET|POST /api/v1/tenant_match`,
`GET|PATCH|DELETE /api/v1/tenant_match/{uid}`).

### The `tenant_match.fail_closed` setting

A platform-wide runtime setting (`tenant_match.fail_closed`, boolean, default
**`false`**) controls what happens to a user who matches **no** rule and did not
supply an explicit org:

| `tenant_match.fail_closed` | Unmatched user with no explicit org |
|---|---|
| `false` (default) | Lands in the `default` tenant — the safe migration default; existing deployments are unaffected. |
| `true` | **Denied** with `403 "no organization matched your account"` — no session is issued. |

An explicit, non-default `org` in the login request always wins and is never
overridden, even under `fail_closed`.

:::note SAML email matching
SAML logins do not currently surface an email address, so **`domain` rules do
not fire for SAML**. Use `group` or `login` rules for SAML, or OIDC/LDAP for
domain-based routing. (This is a documented limitation; OIDC and LDAP populate
the email used for domain matching.)
:::

When at least one rule exists, `GET /api/v1/login` includes
`"tenant_match_enabled": true` so the login UI can tell the user that
organization routing is automatic.

## API reference

| Endpoint | Description |
|---|---|
| `GET /api/v1/login` | Returns `{ backends, tenants, tenant_match_enabled? }`. The `tenants` array contains only active + listed tenants; `login_key` is never included. `tenant_match_enabled` is `true` when ≥1 routing rule exists. |
| `GET\|POST /api/v1/tenant_match`, `GET\|PATCH\|DELETE /api/v1/tenant_match/{uid}` | CRUD over the global attribute→tenant routing registry. Requires the `rw_tenant` permission (platform admin). |
| `GET /api/v1/login/tenant?key=<login_key>` | Resolves an opaque key to `{ id, display_name }`. Returns a generic 404 for unknown, empty, or suspended tenants — it never resolves by slug. |
| `POST /api/v1/tenant/{id}/rotate-login-key` | Generates a new `login_key`, invalidates the previous one, and returns `{ id, login_key }`. Requires the `rw_tenant` permission (platform admin). |

## Related pages

- [Multi-tenancy](./multitenancy.md) — conceptual overview of tenants, scoping, and permissions.
- [Tenant management](./tenant_management.md) — create, update, suspend, and delete tenants via the REST API.
