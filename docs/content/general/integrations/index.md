---
sidebar_position: 0
---

# Integrations

Snooze ships a broad catalogue of **input** (ingest) and **output** (notification) integrations. Inputs map a foreign alert source onto a Snooze record; outputs deliver matching alerts to an external destination.

Each page documents the integration's configuration surface and how to verify it. The newer plugin-based integrations also ship an env-gated end-to-end test (see each page's "End-to-end test setup" section); run them all with:

``` console
$ task go:test:e2e        # or: go test -run E2E ./...
```

Every such test self-skips unless its `SNOOZE_E2E_*` credentials are exported, so the suite stays green with no external dependencies.

## Authenticating ingest

Inbound webhook receivers (`/api/v1/webhook/*`) are **unauthenticated by default**, matching historical behaviour — anyone who can reach the endpoint can submit alerts. Network isolation (a reverse proxy or a restricted monitoring network) is the recommended baseline. Three opt-in layers harden ingest on top of it, configured in the bootstrap `ingest` section:

- **Shared ingest token** — set `ingest.token` to require every webhook request to carry it as `Authorization: Bearer <token>` or `?token=<token>`. Applies to all receivers at once.
- **Per-source signatures** — set `ingest.sns_verify: true` to verify Amazon SNS message signatures on the *cloudwatch* receiver, and/or `ingest.sentry_secret` to verify the Sentry `sentry-hook-signature` HMAC on the *sentry* receiver.
- **Per-heartbeat token** — the *heartbeat* ping carries an unguessable per-heartbeat token in its URL, while its CRUD collection uses normal operator authentication. The shared ingest token (if set) stacks on top.

``` yaml
ingest:
  token: "<random shared secret>"          # require on all webhook receivers
  sns_verify: true                         # verify SNS signatures (cloudwatch)
  sentry_secret: "<sentry client secret>"  # verify Sentry HMAC (sentry)
```

Plugin **CRUD** endpoints (rules, snoozes, notifications, heartbeats, …) are unaffected — they always require a logged-in operator (JWT), independent of these ingest knobs.

## Inputs

New here? Start with **[Send your first alert](./sending-alerts.md)** for the
fastest paths to get alerts flowing, then see the per-integration pages below.

## Outputs



