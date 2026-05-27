---
sidebar_position: 8
---

# Sentry (input)

## Overview

The Sentry integration is an **input** WebhookReceiver plugin that runs in-process inside `snooze-server`. It accepts inbound HTTP POST requests from Sentry and maps them to Snooze alert records.

Both Sentry delivery shapes are supported:

- **Legacy webhook plugin** — the flat JSON payload produced by Sentry's built-in *Webhook* plugin (Sentry 8.x–21.x and the legacy `sentry-webhook` integration). Fields appear at the top level alongside an `event` sub-object.
- **Modern Integration** (issue alert / event alert) — the envelope with a top-level `data` key containing `data.issue` and/or `data.event`, plus an optional `action` field (`triggered`, `created`, or `resolved`).

Shape detection is automatic: if the payload contains a top-level `"data"` key it is treated as a modern Integration payload; otherwise as the legacy webhook shape.

## Configuration

### Inbound URL

The webhook endpoint is:

``` text
POST /api/v1/webhook/sentry
```

No authentication is required on this endpoint (the route is deliberately unauthenticated so Sentry can push without credentials, matching the `grafana` and `alertmanager` receiver policy). Optional HMAC signature verification is available as opt-in hardening — see *Notes & limitations* below.

### Sentry alert-rule setup — Modern Integration

1.  In Sentry, go to **Settings → Integrations → WebHooks** and add your Snooze instance URL as the webhook target: `https://<snooze-host>/api/v1/webhook/sentry`.
2.  Create an **Alert Rule** (Issues or Performance) and add the action **"Send a notification via a webhook"**, selecting the integration you just created.
3.  Sentry will POST the modern `{"action": "...", "data": {...}}` payload to the endpoint whenever the rule fires or resolves.

### Sentry alert-rule setup — Legacy webhook plugin

1.  In Sentry, go to **Settings → Legacy Integrations → WebHooks**.
2.  Add `https://<snooze-host>/api/v1/webhook/sentry` as a callback URL.
3.  Sentry will POST the legacy flat payload whenever an issue is created or updated.

### `curl` example (legacy shape)

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/sentry \
    -H "Content-Type: application/json" \
    -d '{
          "id": "1",
          "project": "my-backend",
          "project_name": "My Backend",
          "culprit": "app.main in app.py",
          "message": "ZeroDivisionError: division by zero",
          "url": "https://sentry.io/organizations/myorg/issues/1/",
          "level": "error",
          "server_name": "web-01",
          "event": {
              "event_id": "abc123def456",
              "tags": [["server_name", "web-01"], ["environment", "prod"]],
              "environment": "production",
              "release": "1.2.3"
          }
        }'
{"accepted":1,"received":1,"status":"ok"}
```

### `curl` example (modern Integration shape)

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/sentry \
    -H "Content-Type: application/json" \
    -d '{
          "action": "triggered",
          "data": {
              "issue": {
                  "title": "ValueError: unexpected value",
                  "culprit": "validate in validators.py",
                  "level": "warning",
                  "permalink": "https://sentry.io/organizations/myorg/issues/42/",
                  "project": {"slug": "my-backend", "name": "My Backend"}
              },
              "triggered_rule": "High error rate"
          }
        }'
{"accepted":1,"received":1,"status":"ok"}
```

### Field mapping

The following table summarises how Sentry fields are mapped to Snooze record fields.

| Snooze field | Legacy source | Modern Integration source |
|----|----|----|
| `Source` | `"sentry"` | `"sentry"` |
| `Severity` | `level` field (see severity mapping below) | `data.issue.level` or `data.event.level` |
| `Host` | `server_name` → `event.tags["server_name"]` → `project` → `project_name` | `data.event.server_name` → `data.event.tags["server_name"]` → `data.issue.project.slug` |
| `Process` | `project` → `project_name` | `data.issue.project.slug` → `data.event.project` |
| `Message` | `message` → `culprit` | `data.issue.title` → `data.issue.culprit` → `data.event.message` → `data.event.title` |
| `State` | *(not set)* | `"close"` when `action == "resolved"` |
| `Raw` | `url`, `project`, `event_id`, `environment`, `release`, `level` | `permalink` (as `url`), `project`, `event_id`, `environment`, `release`, `level` |

### Severity mapping

| Sentry `level`       | Snooze `Severity` |
|----------------------|-------------------|
| `fatal`              | `critical`        |
| `error`              | `critical`        |
| `warning`            | `warning`         |
| `info`               | `info`            |
| `debug`              | `info`            |
| *(unknown or empty)* | `critical`        |

## End-to-end test setup

An env-gated E2E test is included in the package. It posts a realistic sample payload to a live `snooze-server` and asserts a 2xx response.

**Required environment variable**

`SNOOZE_E2E_SENTRY_URL`  
The full URL of the Sentry webhook endpoint on your running snooze-server instance, e.g. `http://localhost:5200/api/v1/webhook/sentry`.

``` console
# Start snooze-server with the sentry plugin enabled, then:
$ export SNOOZE_E2E_SENTRY_URL="http://localhost:5200/api/v1/webhook/sentry"
$ go test -run TestSentryE2E ./internal/pluginimpl/sentry/...
```

Both legacy and modern payload shapes have a corresponding E2E test (`TestSentryE2E` and `TestSentryE2EModern`). Both sample payloads are **unsigned**, so they exercise the default (verify-off) path. To E2E-test the signed path, set `config.ingest.sentry_secret` on the running server and send a request whose `sentry-hook-signature` header is the hex HMAC-SHA256 of the exact request body keyed by that secret.

## Notes & limitations

- **Sentry tags array vs. object**: Sentry delivers event tags as a list of `[key, value]` pairs in the legacy plugin shape but may deliver them as a map in some modern contexts. The plugin handles both forms when looking for the `server_name` tag.
- **Sentry-request-signature verification (opt-in)**: Sentry HMAC-signs webhook requests with the `sentry-hook-signature` header. By default this plugin does **not** verify it, so behavior is unchanged from earlier releases and unsigned bodies are accepted. Set `config.ingest.sentry_secret` to your Sentry client secret to opt in. When set, the plugin reads the **raw** request body, computes `HMAC-SHA256(secret, raw_body)`, hex-encodes it, and constant-time-compares it (`hmac.Equal`) against the `sentry-hook-signature` header. A missing header or a mismatch responds **403** and the body is not processed. When the secret is empty (the default) no header is required. As defense-in-depth you may still place an internet-facing instance behind a reverse proxy that enforces an IP allowlist for Sentry's outbound ranges.
- **One record per webhook call**: Both payload shapes produce exactly one Snooze record per delivery. Sentry batches multiple events into a single issue; the plugin records the issue-level alert, not individual events.
- **Resolve flow**: The `action == "resolved"` path sets `State: "close"` on the emitted record, which the downstream snooze rule engine uses to close the matching open alert.

