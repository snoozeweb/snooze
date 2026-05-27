---
sidebar_position: 2
---

# Prometheus (input)

## Overview

The **prometheus** plugin is an in-process WebhookReceiver that accepts the [Prometheus Alertmanager webhook payload](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config) and converts each alert in the `alerts[]` array into a Snooze record. It is registered at `/api/v1/webhook/prometheus`.

Prometheus Alertmanager delivers firing and resolved alerts by POSTing a JSON envelope to a user-configured webhook URL. The plugin maps labels and annotations from each alert to the standard Snooze record schema, including severity normalisation and automatic resolution (`State: "close"`) for `resolved` status alerts.

:::note

This plugin reproduces the historical Python `prometheus` receiver semantics: the `Source` label is `"prometheus"` and the default severity for unlabelled firing alerts is `"critical"`. If you need the explicit AlertManager v4 receiver with slightly different defaults and port-stripping, use the [Prometheus Alertmanager (input)](./alertmanager.md) plugin instead.

:::

## Configuration

### Inbound URL

The plugin mounts at:

    /api/v1/webhook/prometheus

No authentication is required on this endpoint by default. See [Integrations](./index.md) and [Ingest configuration](../../configuration/ingest.md) for hardening options including a shared ingest token (`config.ingest.token`).

### Pointing Alertmanager at Snooze

Add a `webhook_config` receiver to your Alertmanager configuration:

``` yaml
receivers:
  - name: snooze
    webhook_configs:
      - url: 'https://<snooze-host>/api/v1/webhook/prometheus'
        send_resolved: true

route:
  receiver: snooze
```

Set `send_resolved: true` so that resolved alerts generate a `State="close"` record in Snooze and open alerts are automatically closed.

### Curl example

Post a minimal two-alert payload:

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/prometheus \
    -H 'Content-Type: application/json' \
    -d '{
      "version": "4",
      "groupKey": "{}:{alertname=\"HighCPU\"}",
      "status": "firing",
      "receiver": "snooze",
      "groupLabels": {"alertname": "HighCPU"},
      "commonLabels": {"env": "prod"},
      "commonAnnotations": {},
      "externalURL": "https://alertmanager.example.com",
      "alerts": [
        {
          "status": "firing",
          "labels": {
            "alertname": "HighCPU",
            "instance": "web-1.local:9100",
            "severity": "critical",
            "service": "nginx",
            "env": "prod"
          },
          "annotations": {
            "summary": "CPU usage above 90% on web-1"
          },
          "startsAt": "2024-01-15T12:00:00Z",
          "endsAt": "0001-01-01T00:00:00Z",
          "generatorURL": "https://prometheus.example.com/graph",
          "fingerprint": "abc123def456"
        }
      ]
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

### Field mapping

One Snooze record is produced per entry in the `alerts[]` array. Labels are merged in the order `commonLabels` → `groupLabels` → `alert.labels`, with later values winning.

| Payload field | Snooze field | Notes |
|----|----|----|
| `Source` (constant) | `Source` | Always `"prometheus"`. |
| `alert.labels.host` | `Host` | Falls back to `labels.instance`, then `labels.exported_instance`. Empty string when none of the candidates are set. |
| `alert.labels.severity` | `Severity` | For `status=firing`: uses this label value; defaults to `"critical"` when absent. For `status=resolved`: always `"ok"`. Any other status: `"unknown"`. |
| `alert.annotations.message` | `Message` | Falls back to `annotations.summary` → `annotations.description` → `annotations.externalURL`. |
| `alert.labels.process` | `Process` | Falls back to `labels.service`. Empty when neither is set. |
| `alert.startsAt` | `Timestamp` | Falls back to server `time.Now()` when absent or zero. |
| `alert.status` | `State` | `"resolved"` → `State="close"`; all other values leave `State` empty. |
| `alert.labels`, `alert.annotations`, `alert.generatorURL`, `externalURL`, `alert.fingerprint`, `alert.status` | `Raw` | The full decoded payload is stored in `Raw` for downstream rules. |

## Notes & limitations

- **Unauthenticated by default.** The endpoint accepts any POST from any source. Place Snooze behind a reverse proxy restricted to your Alertmanager IP(s) and/or configure a shared ingest token — see `config.ingest` / [Ingest configuration](../../configuration/ingest.md) and [Integrations](./index.md).
- **One record per alert.** A single webhook POST can carry many alerts; each becomes an independent Snooze record.
- **Resolution pairing.** The `"close"` record carries the same `labels.host` / `labels.service` values as the firing record. Configure aggregation rules on `Raw.fingerprint` or the alert name label for reliable deduplication.
- **Label JSON types.** Prometheus label values are always strings in the Alertmanager wire format; however, this plugin handles raw JSON values (numbers, arrays) that some senders may emit.
- **Relationship to the alertmanager plugin.** Both plugins consume the same wire shape. The differences are the `Source` constant (`"prometheus"` vs `"AlertManager"`), the host-not-found fallback (empty string vs `"-"`), and the process label search order (`process→service` vs `process→service→alertgroup→job`). Pick the one that matches the label conventions already present in your Prometheus rules.

