---
sidebar_position: 7
---

# Ingest configuration

> Package location  
> `/etc/snooze/server-go/ingest.yaml` (Go canonical)
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False` (bootstrap defaults)

Optional, opt-in hardening for the inbound webhook receivers mounted under `/api/v1/webhook/*`. Every field is off by default, so existing deployments keep working with no ingest authentication. Network isolation (a reverse proxy or a restricted monitoring network) remains the recommended baseline; these knobs are defense-in-depth.

Plugin **CRUD** endpoints are unaffected — they always require a logged-in operator (JWT), independent of these settings.

The Go schema lives in `internal/config/schema/ingest.go`. See also the per-integration pages under [Integrations](../general/integrations/index.md) for how each receiver behaves.

## Properties

### token

> Type  
> string
>
> Default  
> `""` (disabled)
>
> Shared secret required on **every** `/api/v1/webhook/*` request, supplied as `Authorization: Bearer <token>` or as a `?token=<token>` query parameter. Compared in constant time. When empty (the default) no token is required and receivers stay unauthenticated.

### sns_verify

> Type  
> boolean
>
> Default  
> `false`
>
> Verify Amazon SNS message signatures on the `cloudwatch` receiver (validates the `SigningCertURL` host against an `sns.*.amazonaws.com` allow-list, then checks the RSA SHA1/SHA256 signature). Invalid or unsigned messages are rejected with `403`. When `false` the receiver does not fetch the certificate or verify anything.

### sentry_secret

> Type  
> string
>
> Default  
> `""` (disabled)
>
> When non-empty, verify the Sentry `sentry-hook-signature` HMAC-SHA256 header on the `sentry` receiver against this client secret (constant-time compare). A missing or mismatched signature is rejected with `403`. When empty, no signature is required.

## Example

``` yaml
---
# All fields optional; omit the file entirely to keep ingest unauthenticated.
token: "a-long-random-shared-secret"
sns_verify: true
sentry_secret: "your-sentry-client-secret"
```

