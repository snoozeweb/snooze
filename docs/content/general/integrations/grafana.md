---
sidebar_position: 4
---

# Grafana (input)

## Overview

The **grafana** plugin is an in-process WebhookReceiver that accepts the [Grafana legacy alert-notifier webhook](https://grafana.com/docs/grafana/v8.4/alerting/notifications/) and converts each evaluated metric match into a Snooze record. It is registered at `/api/v1/webhook/grafana`.

This plugin targets the **legacy webhook notifier** used by Grafana 8.4 and below, including the "Webhook" notifier available in unified-alerting compatibility mode. Grafana POSTs the payload on every state transition. The plugin fans out across the `evalMatches[]` array for alerting states, producing one Snooze record per matched series, and emits a single envelope record for resolved and no-data transitions.

### State handling

| Grafana state | Snooze output |
|----|----|
| `alerting` | One record per `evalMatches[]` entry; `Severity` from `tags.severity` (default `"critical"`). |
| `ok` | One record with `Severity="info"`, `State="close"` — triggers alert resolution. |
| `no_data` | One record with `Severity="warning"`, no state change — represents a monitoring gap. |
| `paused` | No records emitted. |

## Configuration

### Inbound URL

The plugin mounts at:

    /api/v1/webhook/grafana

No authentication is required on this endpoint by default. See [Integrations](./index.md) and [Ingest configuration](../../configuration/ingest.md) for hardening options including a shared ingest token (`config.ingest.token`).

### Adding a webhook notification channel in Grafana

1.  In Grafana go to **Alerting → Notification channels** (Grafana 7/8) and click **Add channel**.
2.  Set the type to **Webhook**.
3.  Set the **URL** to `https://<snooze-host>/api/v1/webhook/grafana`.
4.  Leave **HTTP Method** as `POST` and **Content-Type** as `application/json`.
5.  Click **Send Test** to verify connectivity.
6.  Assign the channel to an alert rule under **Alert → Notifications**.

``` text
URL: https://<snooze-host>/api/v1/webhook/grafana
HTTP Method: POST
```

To pass metadata (host, severity) that the plugin can read, add dashboard or panel tags with the keys `host`, `process`, and/or `severity`. These keys are picked up from the `evalMatches[].tags` map for alerting payloads and from the top-level `tags` map for resolved/no-data payloads.

### Curl example

Post a representative alerting payload with two matched series:

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/grafana \
    -H 'Content-Type: application/json' \
    -d '{
      "title": "[Alerting] High CPU",
      "ruleId": 42,
      "ruleName": "High CPU",
      "ruleUrl": "https://grafana.example.com/d/abc/dashboard",
      "state": "alerting",
      "message": "CPU utilisation has exceeded 90%",
      "imageUrl": "",
      "orgId": 1,
      "dashboardId": 7,
      "panelId": 3,
      "tags": {"env": "prod"},
      "evalMatches": [
        {
          "metric": "cpu_usage",
          "value": 92.5,
          "tags": {
            "host": "web-1",
            "process": "nginx",
            "severity": "critical"
          }
        },
        {
          "metric": "cpu_usage",
          "value": 91.1,
          "tags": {
            "host": "web-2",
            "process": "nginx",
            "severity": "critical"
          }
        }
      ]
    }'
```

Expected response:

    {"accepted":2,"received":2,"status":"ok"}

To test a resolution event:

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/grafana \
    -H 'Content-Type: application/json' \
    -d '{
      "title": "[OK] High CPU",
      "ruleId": 42,
      "ruleName": "High CPU",
      "ruleUrl": "https://grafana.example.com/d/abc/dashboard",
      "state": "ok",
      "message": "CPU utilisation back to normal",
      "tags": {"host": "web-1", "process": "nginx"},
      "evalMatches": []
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

### Field mapping

For `state=alerting` the mapping is applied per `evalMatches[]` entry. For `state=ok` and `state=no_data` the mapping uses the envelope only.

| Payload field | Snooze field | Notes |
|----|----|----|
| `Source` (constant) | `Source` | Always `"grafana"`. |
| `evalMatches[i].tags.host` | `Host` | Falls back to `ruleName` when absent or empty. For envelope-only states, `tags.host` (top-level) or `ruleName`. |
| `evalMatches[i].tags.severity` | `Severity` | Defaults to `"critical"` for alerting; `"info"` for ok; `"warning"` for no_data. Top-level `tags.severity` is used for envelope-only states. |
| `message` | `Message` | Falls back to `title`, then `ruleName`. |
| `evalMatches[i].tags.process` | `Process` | Falls back to `evalMatches[i].metric` for alerting states; falls back to `ruleName` for envelope-only states. |
| `time.Now()` | `Timestamp` | The legacy Grafana webhook carries no per-match timestamp; the server clock at receive time is used. |
| `state` | `State` | `"ok"` → `State="close"`; all other states leave `State` empty. |
| `evalMatches[i].metric`, `evalMatches[i].value`, `evalMatches[i].tags`, `ruleId`, `ruleName`, `ruleUrl`, `panelId`, `dashboardId`, `orgId`, `imageUrl`, `state` | `Raw` | For alerting records, the per-match data plus envelope identifiers. For envelope-only records, rule/state metadata. |

## Notes & limitations

- **Unauthenticated by default.** The endpoint accepts any POST from any source. Restrict access at the network layer and/or configure a shared ingest token — see `config.ingest` / [Ingest configuration](../../configuration/ingest.md) and [Integrations](./index.md).
- **Legacy notifier only.** This plugin targets the pre-unified-alerting Grafana webhook shape (Grafana 8.4 and below). Grafana 9+ unified alerting routes produce a different payload format that is not parsed by this plugin.
- **One record per evalMatch.** A single alerting POST may contain many matched series; each produces a separate Snooze record. Resolution (`state=ok`) always produces exactly one record.
- **No timestamp from source.** The legacy webhook does not include per-match timestamps. `Record.Timestamp` is set to the server's local `time.Now()` at receive time.
- **Tag-based metadata.** Host, process, and severity must be exposed as Grafana panel or dashboard tags under the keys `host`, `process`, and `severity` respectively. Configure these in the Grafana dashboard JSON or the panel tag editor.

