---
sidebar_position: 10
---

# InfluxDB 2 (input)

## Overview

The **influxdb2** plugin is an in-process WebhookReceiver that accepts [InfluxDB 2.x HTTP notification rule](https://docs.influxdata.com/influxdb/v2/monitor-alert/notification-rules/) webhook payloads and converts each notification into a Snooze record. It is registered at `/api/v1/webhook/influxdb2`.

InfluxDB 2 delivers alert notifications by POSTing a flat JSON document to a user-configured HTTP notification endpoint. The document carries InfluxDB system fields (prefixed with `_`) alongside any tag columns from the underlying data source. The plugin maps the `_level` field to Snooze severity, sets `State="close"` for `ok`/`normal` levels, and stores the full payload in `Raw`.

## Configuration

### Inbound URL

The plugin mounts at:

    /api/v1/webhook/influxdb2

No authentication is required on this endpoint by default. See [Integrations](./index.md) and [Ingest configuration](../../configuration/ingest.md) for hardening options including a shared ingest token (`config.ingest.token`).

### Configuring an HTTP notification endpoint in InfluxDB 2

1.  In the InfluxDB 2 UI go to **Alerts → Notification Endpoints** and click **Create Endpoint**.
2.  Choose **HTTP** as the type.
3.  Configure the endpoint:
    - **Destination URL**: `https://<snooze-host>/api/v1/webhook/influxdb2`
    - **HTTP Method**: `POST`
    - Leave authentication empty (the endpoint is unauthenticated by default).
4.  Click **Create Endpoint**.
5.  Go to **Alerts → Notification Rules** and create or edit a rule, selecting the Snooze endpoint as the notification target.

``` text
Destination URL: https://<snooze-host>/api/v1/webhook/influxdb2
HTTP Method: POST
```

### Curl example

Post a representative InfluxDB 2 notification payload:

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/influxdb2 \
    -H 'Content-Type: application/json' \
    -d '{
      "_measurement": "cpu",
      "_field": "usage_idle",
      "_level": "crit",
      "_message": "CPU idle is critically low on web-1",
      "_source_measurement": "cpu",
      "_status_timestamp": 1705320000,
      "_time": "2024-01-15T12:00:00Z",
      "host": "web-1",
      "process": "nginx",
      "severity": "critical",
      "_notification_rule_name": "HighCPU",
      "_check_name": "CPU check"
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

To test a recovery notification (level `ok` sets `State="close"`):

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/influxdb2 \
    -H 'Content-Type: application/json' \
    -d '{
      "_measurement": "cpu",
      "_level": "ok",
      "_message": "CPU idle has recovered on web-1",
      "_source_measurement": "cpu",
      "_status_timestamp": 1705323600,
      "host": "web-1",
      "process": "nginx"
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

### Field mapping

InfluxDB 2 sends a single flat JSON object per notification; the plugin always produces exactly one Snooze record.

| Payload field | Snooze field | Notes |
|----|----|----|
| `Source` (constant) | `Source` | Always `"influxdb2"`. |
| `severity` | `Severity` | When present, takes priority over the `_level` mapping. Use this custom tag to pass a Snooze-vocabulary severity directly. |
| `_level` | `Severity` (fallback) | Mapped: `crit` → `"critical"`; `warn` → `"warning"`; `info` → `"info"`; `ok` → `"ok"`; `normal` → `"ok"`. Unknown values are passed through unchanged. |
| `_message` | `Message` | The human-readable alert description from the InfluxDB notification rule. |
| `process` | `Process` | Falls back to `_source_measurement` (the InfluxDB measurement name) when the `process` tag is absent. |
| `_status_timestamp` | `Timestamp` | Interpreted as Unix epoch seconds. Falls back to server `time.Now()` when absent or zero. |
| `_level` (after mapping) | `State` | When the resolved level is `"ok"` (from `_level=ok` or `_level=normal`), `State="close"` is set automatically. |
| Full payload | `Raw` | The complete decoded payload is deep-copied into `Raw`, preserving all InfluxDB system fields (`_check_id`, `_notification_rule_name`, etc.) for downstream rule matching. |

:::note

The `Host` field is **not** set by this plugin. InfluxDB 2 notification payloads do not have a canonical host key. Add a `host` tag to your InfluxDB data and expose it in the notification rule message template; a downstream Snooze rule can then extract it from `Raw.host` and populate the `Host` field.

:::

## Notes & limitations

- **Unauthenticated by default.** The endpoint accepts any POST from any source. Restrict access at the network layer and/or configure a shared ingest token — see `config.ingest` / [Ingest configuration](../../configuration/ingest.md) and [Integrations](./index.md).
- **One record per webhook call.** InfluxDB 2 sends one notification per HTTP POST; this plugin always emits at most one Snooze record.
- **No Host field.** InfluxDB 2 notifications do not carry a canonical host identifier. Use a `process` custom tag and a downstream Snooze enrichment rule to populate `Host` from `Raw` fields.
- **Timestamp precision.** The `_status_timestamp` field is interpreted as epoch seconds; sub-second precision is not preserved.
- **Custom severity tag.** To override the `_level` mapping for a specific check, add a custom `severity` tag (with a Snooze-vocabulary value) to the InfluxDB measurement or check.

