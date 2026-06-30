---
sidebar_position: 4.5
---

# Auth-proxy (trusted-header) mode

> Package location  
> `/etc/snooze/server-go/auth_proxy.yaml` (Go canonical)
>
> Loader  
> `internal/config` (koanf). File-config only — there are no DB-backed runtime
> overrides for this section.
>
> Live reload  
> **No.** Changing `auth_proxy.*` requires a server restart.

Auth-proxy mode lets you put Snooze behind a trusted reverse proxy
(**oauth2-proxy**, **Pomerium**, Apache **mod_auth_openidc**, …) that has
already authenticated the user and forwards the identity in HTTP headers. Snooze
reads a username header and an optional groups header, JIT-provisions a
`method="proxy"` user on first sight, maps the groups to roles via the existing
RBAC resolver, and authenticates the request — **no Snooze-issued JWT and no
`snz_` API key required.** This is how you bolt an arbitrary IdP onto Snooze
without shipping a per-IdP provider: Snooze inherits whatever SSO the proxy
supports.

## :warning: Security warning — read before enabling

**Trusting an identity header with no upstream proxy is a complete
authentication bypass.** If Snooze is reachable directly, any client can send
`X-Forwarded-User: root` and become an administrator. Enabling this mode is only
safe when **both** of the following hold:

1. **Snooze is only reachable through the proxy.** The proxy terminates every
   request; clients cannot reach the Snooze listener directly (network policy,
   firewall, loopback bind, or a sidecar).
2. **The proxy strips any client-supplied copy of the identity headers** before
   it adds its own. oauth2-proxy and Pomerium do this for the headers they set;
   verify it for *both* the username header **and** the groups header — a
   forwarded raw `X-Forwarded-Groups` from the client would otherwise let a user
   grant themselves any role.

In addition, **always set `trusted_proxies`** to the proxy's own IP/CIDR so
Snooze only honors the headers from that source. The mode is **disabled by
default** and stays a no-op until you explicitly flip `enabled: true`.

## Configuration

```yaml
# /etc/snooze/server-go/auth_proxy.yaml
enabled: true                       # default false — the whole mode is off until set
user_header: X-Forwarded-User       # request header carrying the username (required when enabled)
groups_header: X-Forwarded-Groups   # request header carrying the user's groups (optional)
groups_separator: ","               # how groups_header is split (default ",")
auto_signup: true                   # JIT-create unknown users on first sight (default true)
trusted_proxies:                    # IP/CIDR allowlist of the proxy as Snooze sees it
  - 10.0.0.0/8
  - 192.168.1.10
method: proxy                       # identity method tag + Claims.method (default "proxy")
```

Every field can also be supplied via environment variables
(`SNOOZE_SERVER_AUTH_PROXY_ENABLED`, `SNOOZE_SERVER_AUTH_PROXY_USER_HEADER`, …).
`trusted_proxies` is a list field, so its env value comma-splits:
`SNOOZE_SERVER_AUTH_PROXY_TRUSTED_PROXIES=10.0.0.0/8,192.168.1.10`.

| Field              | Default              | Notes |
|--------------------|----------------------|-------|
| `enabled`          | `false`              | Master switch. When false the auth middleware is byte-identical to the standard Bearer-only path. |
| `user_header`      | `X-Forwarded-User`   | **Required when enabled.** Empty + enabled fails validation at boot. |
| `groups_header`    | `X-Forwarded-Groups` | Optional. An absent/empty header yields a user with no groups (and thus no group-mapped roles). |
| `groups_separator` | `,`                  | Splits the groups header; surrounding whitespace is trimmed and empty entries dropped. |
| `auto_signup`      | `true`               | When false, an unknown proxy user is rejected with **403** instead of being created. |
| `trusted_proxies`  | _(empty)_            | IP or CIDR entries. Empty while enabled is **valid but logs a loud boot WARN** (fail-open). |
| `method`           | `proxy`              | Stamped on the provisioned user document and the session claims. |

### Validation

When `enabled: true`, boot fails if `user_header` is empty or if any
`trusted_proxies` entry is not a valid IP or CIDR. An **empty**
`trusted_proxies` list is *not* a hard error — some deployments terminate the
proxy on the same pod/loopback where an allowlist is redundant — but Snooze logs
a loud WARN once at boot so the fail-open posture is never silent.

## How the gate behaves

The proxy branch runs **before** the normal `Authorization` check and is a
strict fall-through gate — it never rejects a request that simply isn't a
proxied one:

- **Untrusted peer** (when `trusted_proxies` is non-empty and the immediate TCP
  peer address is outside it) → the headers are ignored and the request
  continues to the Bearer/`snz_`/JWT path.
- **Missing username header** → fall through (so the proxy itself can reach
  `/api/v1/login`, health checks, etc.).
- **Valid `Bearer` JWT or `snz_` key present** → still authenticates normally;
  proxy mode coexists with CLI tools, the syncer, and API keys.
- **Unknown user + `auto_signup: false`** → **403** (`forbidden` envelope).
- **Successful proxy auth** → claims + tenant are stamped and the request
  proceeds; it never falls through to Bearer.

Groups are **re-synced** onto the user document on every authenticated request,
so a change to the user's groups at the IdP takes effect on the next request
(role mapping stays live rather than frozen at first sign-in).

### `trusted_proxies` semantics

`trusted_proxies` is **the proxy's own IP as Snooze sees it on the TCP
connection** — not the end-user's IP. The gate matches against the **immediate
TCP peer address** (`RemoteAddr` as the Go HTTP server reads it from the socket),
captured by a dedicated `CapturePeerIP` middleware that runs **before** chi's
`RealIP`. It is therefore **immune to `X-Forwarded-For` / `X-Real-IP` spoofing**:
a client reaching Snooze directly cannot forge a trusted address by setting those
headers, because the gate never consults them.

:::note Audit and ingest are unaffected
The audit log's `remote` field and the client IP stamped onto ingested records
still honor `X-Forwarded-For` / `X-Real-IP` (operators want the real client
behind the proxy there). Only the `trusted_proxies` trust decision uses the
genuine peer.
:::

Set `trusted_proxies` to the proxy's real connecting address (the socket peer
Snooze observes). In a single-hop deployment that is the proxy's own IP. This
does **not** remove the requirement to strip inbound client copies of the
identity headers at the proxy (see the security warning above): the IP allowlist
proves the *connection* came from the proxy, but the proxy must still ensure the
*identity headers* on that connection are its own and not a client's.

## Groups → roles mapping

Resolved roles are the union of:

- roles attached directly to the user document, and
- roles whose **`groups`** field intersects the groups from `groups_header`.

To grant a proxy group a role, open **Settings → Roles**, edit the target role,
and add the IdP group value to its **Groups** list. A proxy user only ever
receives the permissions those mapped roles carry — it **cannot self-escalate**.
In particular, the platform control-plane permissions (`ro_tenant` / `rw_tenant`
gating `/api/v1/tenant`) are reserved: they are never granted to a proxy group
unless that group is mapped to the seeded `platform_admin` role, and the
`rw_all` wildcard does not satisfy them.

Provisioned proxy users land in the **`default` tenant**. (Attribute-based
tenant resolution is layered on top in a later release.)

## Example — oauth2-proxy

Run oauth2-proxy in front of Snooze and have it forward the authenticated
identity as request headers:

```
oauth2-proxy \
  --upstream=http://snooze-server:5200 \
  --set-xauthrequest=true \
  --pass-user-headers=true \
  --set-authorization-header=false \
  --email-domain=example.com \
  ... (provider/client flags)
```

`--set-xauthrequest` makes oauth2-proxy emit `X-Auth-Request-User` /
`X-Auth-Request-Groups`; with `--pass-user-headers` the upstream also receives
`X-Forwarded-User` / `X-Forwarded-Groups`. Point `user_header` /
`groups_header` at whichever pair your proxy sets, and set `trusted_proxies` to
the oauth2-proxy address. Ensure the proxy is configured to **strip** any
inbound copies of those headers from the client.
