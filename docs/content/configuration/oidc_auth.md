---
sidebar_position: 4
---

# OIDC authentication (Microsoft 365 / Entra ID)

> Package location  
> `/etc/snooze/server-go/oidc.yaml` (Go canonical)
>
> Loader  
> `internal/config` (koanf) for the file baseline; DB-backed runtime overrides
> via the **Settings → OIDC / SSO** tab
>
> Live reload  
> **Partial.** `issuer`, `client_id`, `redirect_url`, `scopes`, `roles_claim`,
> `groups_claim`, and `enabled` are editable in the web UI and take effect on the
> next sign-in (no restart). `client_secret` and `method` are file-config only.

Snooze ships a generic OpenID Connect authentication backend that works with any
compliant identity provider and is pre-configured for **Microsoft 365 / Entra ID**
by default.

The backend always registers two public routes (it is gated by `enabled`, not by
whether it is mounted):

- `GET /api/v1/login/{method}/start` — redirects the browser to the identity
  provider's authorize endpoint.
- `GET /api/v1/login/{method}/callback` — receives the authorization-code
  callback, validates the ID token, and issues a Snooze session JWT.

**Two-tier configuration.** Like LDAP, OIDC follows the file-baseline +
DB-override model. The connection and claim fields can be edited live from
**Settings → OIDC / SSO** in the web UI (stored per-tenant in the DB). Two fields
are deliberately kept in file-config only:

- **`client_secret`** — a secret, never stored in the database. Supply it via the
  `SNOOZE_SERVER_OIDC_CLIENT_SECRET` environment variable (or `oidc.yaml`).
- **`method`** — the login URL segment + the identity/JWT method claim stamped on
  every provisioned user; changing it at runtime would orphan users and break the
  registered Redirect URI.

So the minimal bootstrap is: set `client_secret` (and optionally `method`) in
`oidc.yaml`/env once, then configure issuer / client_id / redirect_url / scopes /
claims and flip `enabled` from the web UI.

The Go schema lives in `internal/config/schema/oidc.go`; the runtime overlay in
`internal/config/runtime.go` (`RuntimeSettings.OIDC`).

## Properties

### enabled

> Type  
> boolean
>
> Default  
> `false`
>
> Enable or disable the OIDC authentication backend. When `false` the backend
> does not appear in `GET /api/v1/login` and sign-in is refused (the routes stay
> mounted but inert). Editable live from **Settings → OIDC / SSO**.

### issuer

> Type  
> string
>
> The OpenID Connect issuer URL. The server fetches
> `{issuer}/.well-known/openid-configuration` to discover the authorization and
> token endpoints. For Microsoft Entra ID the value is
> `https://login.microsoftonline.com/{tenant-id}/v2.0`.

### client_id

> Type  
> string
>
> Application (client) ID copied from the Entra app registration (or equivalent
> for other providers).

### client_secret

> Type  
> string
>
> Environment variable  
> `SNOOZE_SERVER_OIDC_CLIENT_SECRET`
>
> OAuth 2.0 client secret. **Do not put this value in a YAML file committed to
> version control.** Provide it via the environment variable shown above or via
> a secret-file mechanism supported by your deployment tooling.

### redirect_url

> Type  
> string
>
> The Redirect URI registered on the identity provider. Must match exactly,
> including scheme and path. The typical value is
> `https://<snooze-host>/api/v1/login/microsoft/callback`.

### scopes

> Type  
> []string (YAML sequence or comma-separated string in env)
>
> Default  
> `["openid", "profile", "email"]`
>
> OAuth 2.0 scopes to request. The `openid` scope is required for OIDC. Add
> `offline_access` if a refresh token is needed. When overriding via environment
> variable (`SNOOZE_SERVER_OIDC_SCOPES`), separate values with commas.

### method

> Type  
> string
>
> Default  
> `microsoft`
>
> Identifier used in the login route paths (`/api/v1/login/{method}/start`) and
> sets the backend's `name` field in the backends list returned by
> `GET /api/v1/login`.

### display_name

> Type  
> string
>
> Default  
> `Microsoft 365`
>
> Human-readable label shown on the login-page button (e.g. "Continue with
> Microsoft 365").

### icon

> Type  
> string
>
> Default  
> `microsoft`
>
> Icon identifier passed to the login-page frontend. The web UI maps this string
> to the corresponding brand icon.

### roles_claim

> Type  
> string
>
> Default  
> `roles`
>
> Name of the ID-token claim that carries the list of app roles assigned to the
> user. For Entra this is the `roles` claim populated when App Roles are defined
> on the application and assigned to users or groups.

### groups_claim

> Type  
> string
>
> Default  
> `groups`
>
> Name of the ID-token claim that carries the list of group object IDs the user
> belongs to. Useful when role mapping is done against Entra group IDs rather
> than app roles.

### admin_role_value

> Type  
> string
>
> Default  
> `Admin`
>
> The value that triggers automatic admin role assignment on a **fresh install**.
> When the database is empty and OIDC is enabled, the seeded `admin` Snooze role
> has this value added to its `groups[]` list so any user whose `roles` claim
> contains `Admin` immediately receives the Snooze admin role. On existing
> installs you add the value manually through the Roles UI (see
> [Role mapping](#role-mapping)).

## Example configuration

```yaml title="oidc.yaml"
# OpenID Connect (e.g. Microsoft 365 / Entra) authentication.
# File-config only. Leave client_secret out of version control — supply it via
# SNOOZE_SERVER_OIDC_CLIENT_SECRET or a secret file.
enabled: false
issuer: "https://login.microsoftonline.com/<tenant-id>/v2.0"
client_id: "<application-client-id>"
client_secret: ""        # set via SNOOZE_SERVER_OIDC_CLIENT_SECRET
redirect_url: "https://<snooze-host>/api/v1/login/microsoft/callback"
scopes: ["openid", "profile", "email"]
method: "microsoft"
display_name: "Microsoft 365"
icon: "microsoft"
roles_claim: "roles"
groups_claim: "groups"
admin_role_value: "Admin"
```

## Editing OIDC settings in the web UI

Open **Settings → OIDC / SSO** (requires write access to settings). The tab
exposes the live, DB-backed fields — `enabled`, `issuer`, `client_id`,
`redirect_url`, `scopes` (space-separated), `roles_claim`, and `groups_claim` —
and edits take effect on the next sign-in without a restart (the server rebuilds
its OIDC discovery when the issuer/client/scopes/redirect change).

`client_secret` and `method` are **not** shown here by design (see the two-tier
note above): set them in `oidc.yaml`/env. The group→role mapping itself is
managed on the **Roles** page (each role's **Groups** field), not here.

## Microsoft Entra app registration walkthrough

1. **Register the application.** In the [Azure portal](https://portal.azure.com),
   open **Entra ID → App registrations** and choose an existing app or create a
   new one. Copy the **Application (client) ID** and the **Directory (tenant) ID**
   — you will need both.

2. **Add the Redirect URI.** Under **Authentication → Add a platform → Web**,
   add the URI:
   ```
   https://<snooze-host>/api/v1/login/microsoft/callback
   ```
   The value must match `redirect_url` in your OIDC config exactly (scheme,
   host, and path).

3. **Create a client secret.** Under **Certificates & secrets → Client secrets**,
   create a new secret and copy its value immediately (it is shown only once).
   Supply it to Snooze via the `SNOOZE_SERVER_OIDC_CLIENT_SECRET` environment
   variable — do not put it in a YAML file or commit it to version control.

4. **Define App Roles.** Under **App roles**, create at minimum an `Admin` role
   (value: `Admin`) with **Allowed member types: Users/Groups**. Add further
   roles such as `Editor` or `Viewer` if you want fine-grained access. Then
   open **Enterprise applications → {your app} → Users and groups** and assign
   your users or groups to the appropriate app roles.

5. **Emit claims in the ID token.** Under **Token configuration**, add an
   **optional claim** for `roles` on the **ID** token type. If you intend to
   map Entra security groups via `groups_claim`, also add the `groups` claim;
   note that large group memberships can inflate the token — App Roles are
   generally preferred.

### Issuer URL

Set `issuer` to:

```
https://login.microsoftonline.com/<tenant-id>/v2.0
```

where `<tenant-id>` is the Directory (tenant) ID from step 1. Single-tenant
apps use this form; multi-tenant apps can substitute `common` or
`organizations` for the tenant ID.

## Role mapping {#role-mapping}

After token validation, the values in the `roles` and `groups` claims are
collected and treated as the authenticated user's **groups** within Snooze. The
existing group-to-role mapping then applies: a Snooze role whose `groups[]`
list contains one of those values is granted to the user.

**Fresh installs** (empty database with OIDC enabled): the `admin` Snooze role
is seeded with `admin_role_value` (default `Admin`) already in its `groups[]`,
so any Entra user with the `Admin` App Role immediately receives Snooze admin
access — no manual configuration required.

**Existing installs**: open **Settings → Roles → admin** in the web UI, add
`Admin` (or your custom app-role value) to the role's **Groups** field, and
save. Repeat for any other roles you want to map.

## Egerie reference deployment

The Egerie Snooze instance reuses the shared Grafana Entra app registration:

| Field | Value |
|---|---|
| Tenant ID | `58f3eb38-738a-4716-8a24-b09d70407c69` |
| Client ID | `98162c08-edc3-4d29-9f8f-5b62780c0abc` |
| Client secret | Supplied via `SNOOZE_SERVER_OIDC_CLIENT_SECRET` — not stored in this repo. |
| Redirect URI | `https://snooze.egerie.eu/api/v1/login/microsoft/callback` |

This URI must be added to the **Authentication** → **Redirect URIs** list on
that app registration before the OIDC flow will work.
