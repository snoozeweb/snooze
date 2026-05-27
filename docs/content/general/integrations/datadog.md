---
sidebar_position: 6
---

# Datadog (input)

## Overview

The **datadog** plugin is an in-process WebhookReceiver that accepts [Datadog monitor alert](https://docs.datadoghq.com/monitors/notify/) webhook notifications and converts them into Snooze records. It is registered at `/api/v1/webhook/datadog`.

Datadog delivers monitor alerts by POSTing a JSON payload to a user-configured webhook URL. The plugin maps the incoming fields to the standard Snooze record schema, including severity normalisation and automatic resolution (`State: "close"`) for recovery events.

## Configuration

### Step 1 — configure the Snooze webhook URL in Datadog

1.  In the Datadog UI go to **Integrations → Webhooks**.

2.  Create a new webhook with:

    - **URL**: `https://<snooze-host>/api/v1/webhook/datadog`
    - **Payload**: paste the template below (replace it entirely):

    ``` json
    {
      "alert_id":         "$ALERT_ID",
      "title":            "$EVENT_TITLE",
      "body":             "$EVENT_MSG",
      "event_type":       "$EVENT_TYPE",
      "alert_type":       "$ALERT_TYPE",
      "alert_transition": "$ALERT_TRANSITION",
      "date":             $DATE,
      "org_id":           "$ORG_ID",
      "host":             "$HOSTNAME",
      "tags":             "$TAGS",
      "priority":         "$PRIORITY",
      "aggreg_key":       "$AGGREG_KEY",
      "link":             "$LINK"
    }
    ```

3.  Leave **Custom Headers** empty (no authentication is needed on the Snooze side; the webhook endpoint is publicly accessible).

### Step 2 — notify via the webhook in a monitor

In any Datadog monitor, add `@webhook-<name>` in the notification message (where `<name>` is the name you gave the webhook in step 1). Datadog will POST the payload above on every state change.

### Field reference

The table below describes every key in the recommended template and how it maps to a Snooze record field.

| Datadog variable | Snooze field | Notes |
|----|----|----|
| `$ALERT_ID` | `Raw.alert_id`, fallback `Host` | Used as `Host` when `$HOSTNAME` is empty. |
| `$EVENT_TITLE` | `Message` | Primary message text; falls back to `$EVENT_MSG`. |
| `$EVENT_MSG` | `Message` (fallback) | Used when `$EVENT_TITLE` is empty. |
| `$EVENT_TYPE` | `Raw.event_type` | Values: `triggered`, `recovered`, `re_triggered`, `no_data`. `recovered` / `resolved` trigger `State="close"`. |
| `$ALERT_TYPE` | `Severity` + `State` | `error` → `critical`; `warning` → `warning`; `success` → `info` + `State="close"`; `info` → `info`. Default: `critical`. |
| `$ALERT_TRANSITION` | `State` | `Recovered` sets `State="close"` regardless of `alert_type`. |
| `$DATE` | `Raw.date` (not mapped; `Timestamp` is set to `time.Now()`) | Epoch milliseconds. Retained in `Raw` for reference. |
| `$ORG_ID` | `Raw.org_id` | Datadog organisation ID. |
| `$HOSTNAME` | `Host` | Falls back to `alert_id` when empty. |
| `$TAGS` | `Tags`, `Process` | Comma-separated string split into `Tags`. First `service:` or `process:` tag value becomes `Process`. |
| `$PRIORITY` | `Raw.priority` | `normal` or `low`. |
| `$AGGREG_KEY` | `Raw.aggreg_key` | Monitor aggregation key for grouping related alerts. |
| `$LINK` | `Raw.link` | Direct URL to the monitor in Datadog. |

### Curl example

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/datadog \
    -H 'Content-Type: application/json' \
    -d '{
      "alert_id":         "123456789",
      "title":            "[Triggered] High CPU on web-1",
      "body":             "CPU usage exceeded threshold",
      "event_type":       "triggered",
      "alert_type":       "error",
      "alert_transition": "Triggered",
      "date":             1716800000000,
      "org_id":           "99",
      "host":             "web-1",
      "tags":             "service:nginx,env:prod,team:ops",
      "priority":         "normal",
      "aggreg_key":       "agg-001",
      "link":             "https://app.datadoghq.com/monitors/123456789"
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

## End-to-end test setup

The `TestDatadogE2E` test in this package posts a realistic payload to a running snooze-server instance and asserts a `2xx` response. It is skipped unless the environment variable below is set.

| Environment variable | Value |
|----|----|
| `SNOOZE_E2E_DATADOG_URL` | Full URL of the running snooze-server webhook endpoint, e.g. `http://localhost:5200/api/v1/webhook/datadog`. |

``` console
$ export SNOOZE_E2E_DATADOG_URL="http://localhost:5200/api/v1/webhook/datadog"
$ go test -run TestDatadogE2E ./internal/pluginimpl/datadog/...
```

## Notes & limitations

- **No signature verification.** Datadog does not sign webhook payloads (unlike some other services). If the endpoint must be protected, place Snooze behind a reverse proxy that restricts access to Datadog's published IP ranges.
- **One record per webhook call.** Datadog sends one monitor-state event per HTTP POST; this plugin always emits at most one Snooze record.
- **Timestamp accuracy.** The `$DATE` epoch-millisecond value is stored in `Raw` for reference, but `Record.Timestamp` is set to the server's local `time.Now()` at receive time. Sub-second precision is not preserved.
- **Recovery detection.** A `State="close"` record is emitted when `alert_type == "success"` **or** `alert_transition == "Recovered"` **or** `event_type` is `"recovered"` / `"resolved"`. Configure downstream Snooze rules to match on `Raw.alert_id` or `Raw.aggreg_key` for reliable alert pairing.
- **No_data / re_triggered states.** `event_type` values like `no_data` and `re_triggered` are forwarded as-is. No special handling beyond the standard severity mapping is applied.

