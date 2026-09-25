---
sidebar_position: 3.5
---

# API keys

API keys let scripts and services call the Snooze API without an interactive
login. A key is tied to the user who created it and carries a **subset of that
user's permissions** chosen at creation. Permissions are bounded **live**: a
key never grants more than its owner currently holds, and it stops working the
moment the owner is disabled or deleted.

## Create a key (UI)

**Profile → API Keys → New API key.** Pick a name, an optional expiry (capped at
the server's maximum, 365 days by default), and the permissions to grant — you
can only pick from permissions you already hold. The key is shown **once**, on
creation. Copy it then; it cannot be retrieved again.

## Create a key (API)

```bash
curl -sS -X POST https://snooze.example/api/v1/user/me/apikeys \
  -H "Authorization: Bearer $YOUR_SESSION_JWT" \
  -H "Content-Type: application/json" \
  -d '{"name":"ci-bot","permissions":["ro_record","rw_rule"],"expires_at":"2026-12-31T00:00:00Z"}'
# => 201 { "uid": "...", "name": "ci-bot", "key": "snz_…", "expires_at": ... }
```

`expires_at` is optional (defaults to the cap); `permissions` must be a subset
of your own. You cannot create a key while authenticated with a key.

## Create a key (CLI)

```console
$ snooze apikey create --name laptop --perm rw_record,rw_protected --expires 90d
Created API key "laptop" (uid 6f1…, expires 2026-12-24T12:00:00Z).

Key — shown once, copy it now:

  snz_…
```

`--perm` is required (repeat it or comma-separate): run it without one and the
CLI lists the permissions you hold to choose from — there is no "grant
everything" default. `--expires` takes `90d`, a Go duration (`720h`) or an
RFC3339 time; omitted, the server's cap applies. Minting needs a password
session, so if your CLI is already configured with a key, override it for this
one call: `snooze --token= --user <login> apikey create …`.

`snooze apikey list` shows your keys (never their secrets) with expiry and last
use; `snooze apikey revoke <uid>` revokes one. Both work from a key.

A key used for [agentic analyses](agentic_analysis.md) needs `rw_protected`,
which `rw_all` does not imply.

## Use a key

```bash
curl -sS https://snooze.example/api/v1/rule \
  -H "Authorization: Bearer snz_…"
```

With the CLI, put it in `~/.config/snooze/client.yaml` (or
`/etc/snooze/client.yaml`) — it replaces the username/password login:

```yaml
server: https://snooze.example
credentials:
  token: snz_…
```

`chmod 600` the file (the CLI warns when a file holding a secret is readable by
other users). `$SNOOZE_TOKEN` and `--token` override it, in that order of
precedence: `--token` > `$SNOOZE_TOKEN` > `credentials.token`. `snooze whoami`
shows who the server sees behind the configured credential, and
`snooze record list --owner me` resolves "me" through the key. A rejected key
(expired, revoked, owner disabled) fails outright — the CLI never falls back to
a password login from the same file.

Actions taken with a key are attributed to its owner: acknowledging or closing
an alert makes the key's owner the alert's owner, under the owner's own login
method. `GET /api/v1/user/me` returns the identity behind any credential
(`via: apikey`, the key's uid, name, prefix and expiry).

The server recognizes the `snz_` prefix, looks the key up, re-resolves the
owner's current permissions, intersects them with the key's grant, and authorizes
the request with the result.

## Manage your keys

`GET /api/v1/user/me/apikeys` lists your keys (without the secret);
`DELETE /api/v1/user/me/apikeys/{id}` revokes one. The UI exposes both.

> Keys returned by `GET /api/v1/user/me/apikeys` include `last_used_at`
> (Unix epoch, absent for never-used keys) and `use_count` (lower-bound
> request count, updated at most once per hour). Use these to identify
> stale machine keys that can safely be revoked.

## Admin: all keys in a tenant

Users with `ro_apikey` can view every key in their tenant on the **API Keys**
admin page; `rw_apikey` can rename, change expiry, and revoke any of them.
Admins never see raw secrets and cannot mint keys on another user's behalf —
revocation is the lever.

#### Console display

The **Admin → API keys** table now shows a **Last used** column (sortable — click to sort
by staleness) and a **Uses** column. Both values are updated at most once per hour; `use_count`
is a lower-bound estimate.

On the **Profile** page, each key's subtitle includes "last used X ago" (or "last used never"
for a key that has never authenticated a request). Keys unused for more than 30 days show a
**Stale** badge as a prompt to revoke them.

In the **Edit** drawer (Admin → API keys → click a row), the read-only Usage section shows the
last-used timestamp and total uses, with a note that values are updated at most hourly.
