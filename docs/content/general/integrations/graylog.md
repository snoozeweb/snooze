---
sidebar_position: 8.5
---

# Graylog (input)

## Overview

The **graylog** plugin is an in-process WebhookReceiver that accepts [Graylog stream-alert HTTP notifications](https://docs.graylog.org/docs/alerts) and converts each notification into a single Snooze record. It is registered at `/api/v1/webhook/graylog`.

Graylog POSTs this payload when a stream alert condition triggers. The plugin maps the stream title to the record host and the check-result description to the message, and accepts a set of query-string overrides — `event`, `environment`, `service`, `severity`, and `event_type` — that mirror the Alerta Graylog webhook contract. The record then flows through the standard Snooze processing pipeline (rules, snooze, notifications).

### State handling

| Graylog notification | Snooze output |
|----|----|
| Stream alert fires | One record; `Severity` from `?severity` (default `"critical"`). No `State` is set — Graylog's HTTP notification carries no resolved event. |

Graylog HTTP alerts are **fire-only**: there is no "resolved" callback in this webhook format. Records always arrive as new alerts; rely on a snooze TTL or a manual close for resolution.

## Configuration

### Inbound URL

The plugin mounts at:

    /api/v1/webhook/graylog

No authentication is required on this endpoint by default. See [Integrations](./index.md) and [Ingest configuration](../../configuration/ingest.md) for hardening options including a shared ingest token (`config.ingest.token`).

Query-string overrides may be appended to the URL configured in Graylog, e.g.:

    /api/v1/webhook/graylog?environment=production&severity=major&service=api,web

### Configuring Graylog

1.  In Graylog go to **Alerts → Notifications** and click **Create Notification**.
2.  Set the notification **Type** to **HTTP Notification** (legacy `HTTP Alarm Callback` works the same way).
3.  Set the **URL** to `https://<snooze-host>/api/v1/webhook/graylog`, optionally appending the query-string overrides above.
4.  Attach the notification to an **Event Definition** / stream alert condition.
5.  Trigger the condition (or use Graylog's test button) to verify connectivity.

### Curl example

Post a minimal Graylog stream-alert payload:

``` console
$ curl -s -X POST 'https://<snooze-host>/api/v1/webhook/graylog' \
    -H 'Content-Type: application/json' \
    -d '{
      "stream": { "id": "5c6f...", "title": "High error rate" },
      "check_result": {
        "result_description": "Stream had 47 messages in the last 5 minutes",
        "triggered_condition": { "id": "cond-uuid", "type": "message_count" }
      }
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

Override severity, environment and service via the query string:

``` console
$ curl -s -X POST 'https://<snooze-host>/api/v1/webhook/graylog?severity=warning&environment=staging&service=api,web' \
    -H 'Content-Type: application/json' \
    -d '{"stream":{"title":"High error rate"},"check_result":{"result_description":"47 messages"}}'
```

### Field mapping

| Payload field | Query-string override | Snooze field | Notes |
|----|----|----|----|
| `Source` (constant) | — | `Source` | Always `"graylog"`. |
| `stream.title` | — | `Host` | Required; an empty/absent title returns HTTP 400. |
| `check_result.result_description` | — | `Message` | |
| `check_result.triggered_condition.id` | — | `Raw["checkId"]` | |
| — | `event` (default `"Alert"`) | `Raw["event"]` | Alerta-faithful metadata; not a routing field in Snooze. |
| — | `environment` (default `""`) | `Environment` | Empty by default so default-environment rules apply. |
| — | `service` (default `""`, comma-split) | `Raw["service"]` | Stored as a `[]string`; only set when non-empty. |
| — | `severity` (default `"critical"`) | `Severity` | |
| — | `event_type` (default `"performanceAlert"`) | `Raw["event_type"]` | Alerta-faithful metadata. |
| whole payload | — | `Raw["payload"]` | The decoded notification envelope, retained for downstream rules. |
| `time.Now()` | — | `Timestamp` | The Graylog webhook carries no per-alert timestamp; the server clock at receive time is used. |

## Notes & limitations

- **Unauthenticated by default.** The endpoint accepts any POST from any source. Restrict access at the network layer and/or configure a shared ingest token — see `config.ingest` / [Ingest configuration](../../configuration/ingest.md) and [Integrations](./index.md).
- **Fire-only — no resolution signal.** Graylog's HTTP notification has no "resolved" event, so records always arrive as new alerts. Use a snooze TTL or a manual close to clear them.
- **Empty `stream.title` is rejected.** A notification with a blank or missing stream title returns HTTP 400 (the title is the record host).
- **No timestamp from source.** The Graylog webhook does not include a per-alert timestamp. `Record.Timestamp` is set to the server's `time.Now().UTC()` at receive time.
- **Query-string metadata.** `event`, `environment`, `service`, `severity`, and `event_type` are read from the URL query string, mirroring the Alerta Graylog webhook contract; only `environment` and `severity` map to typed record fields, the rest are stored under `Raw`.
