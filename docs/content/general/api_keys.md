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

## Use a key

```bash
curl -sS https://snooze.example/api/v1/rule \
  -H "Authorization: Bearer snz_…"
```

The server recognizes the `snz_` prefix, looks the key up, re-resolves the
owner's current permissions, intersects them with the key's grant, and authorizes
the request with the result.

## Manage your keys

`GET /api/v1/user/me/apikeys` lists your keys (without the secret);
`DELETE /api/v1/user/me/apikeys/{id}` revokes one. The UI exposes both.

## Admin: all keys in a tenant

Users with `ro_apikey` can view every key in their tenant on the **API Keys**
admin page; `rw_apikey` can rename, change expiry, and revoke any of them.
Admins never see raw secrets and cannot mint keys on another user's behalf —
revocation is the lever.
