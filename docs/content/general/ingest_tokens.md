---
sidebar_position: 12
---

# Per-tenant ingest tokens

Each tenant has an optional **ingest token** — an opaque random string that
routes unauthenticated alert ingestion to that tenant. This page explains
how tokens are issued, rotated, and used by alert senders.

See [Multi-tenancy](./multitenancy.md) for the broader context, and
[Ingest configuration](../configuration/ingest.md) for the global
`ingest.token` guard that applies on top.

## How routing works

When `POST /api/v1/alerts` or `POST /api/v1/webhook/*` is called without a
user JWT, the server inspects the `Authorization: Bearer` header (or the
`?token=` query parameter) and looks for a matching per-tenant ingest token:

1. Token present and matches a tenant's `ingest_token` → request is
   processed as that tenant.
2. Token present but unknown → falls back to the `default` tenant (no error
   is returned to the caller; the fallback is intentional and logged at debug
   level).
3. No token → falls back to the `default` tenant.

This means a single-tenant deployment with no ingest token configured
continues to work exactly as before — all traffic lands in `default`.

:::note

The global `ingest.token` guard (bootstrap YAML, `ingest.token` key) is
evaluated **before** per-tenant routing. If a global token is set, every
request must carry it regardless of which tenant it targets. Per-tenant
tokens cannot substitute for the global guard.

:::

## Retrieving the ingest token

The `ingest_token` field is returned in the create response and in
`GET /api/v1/tenant/{id}`. It is omitted from the list endpoint for security.

```bash
# Platform-admin token required
TOKEN=$(curl -s -X POST \
  -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"<password>"}' \
  http://localhost:5200/api/v1/login/local | jq -r .token)

curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:5200/api/v1/tenant/acme | jq .data.ingest_token
```

## Sending alerts to a specific tenant

Supply the ingest token as a Bearer token:

```bash
INGEST_TOKEN="a3f8b2c1..."

# Direct alert
curl -s -X POST \
  -H "Authorization: Bearer $INGEST_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"message":"disk full","host":"srv01","severity":"err"}' \
  http://snooze.example.com/api/v1/alerts

# Alertmanager webhook
curl -s -X POST \
  -H "Authorization: Bearer $INGEST_TOKEN" \
  -H 'Content-Type: application/json' \
  -d @alertmanager_payload.json \
  http://snooze.example.com/api/v1/webhook/alertmanager
```

Or as a query parameter (for tools that do not support custom headers):

```bash
curl -s -X POST \
  "http://snooze.example.com/api/v1/alerts?token=$INGEST_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"message":"disk full","host":"srv01"}'
```

## Configuring Alertmanager for a tenant

In your Alertmanager `alertmanager.yml`, set the `Authorization` header on
the webhook receiver:

```yaml
receivers:
  - name: snooze-acme
    webhook_configs:
      - url: 'https://snooze.example.com/api/v1/webhook/alertmanager'
        http_config:
          authorization:
            type: Bearer
            credentials: 'a3f8b2c1...'
```

## Rotating the ingest token

Rotate with `PATCH /api/v1/tenant/{id}`. The server does not auto-generate
a replacement — supply a new value explicitly. The old token is invalidated
immediately on the server side.

```bash
NEW_TOKEN=$(openssl rand -hex 32)

curl -s -X PATCH \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"ingest_token\":\"$NEW_TOKEN\"}" \
  http://localhost:5200/api/v1/tenant/acme

echo "New ingest token: $NEW_TOKEN"
```

Update every alert sender with the new token before invalidating the old one
(update sender → rotate server-side).

## Disabling unauthenticated ingestion for a tenant

Set `ingest_token` to an empty string to disable token-based routing for
that tenant. The tenant's alerts can still be submitted by a logged-in user
JWT or by the `default` tenant fallback (if no other token matches).

```bash
curl -s -X PATCH \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"ingest_token":""}' \
  http://localhost:5200/api/v1/tenant/acme
```
