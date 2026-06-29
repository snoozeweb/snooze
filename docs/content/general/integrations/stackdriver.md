---
sidebar_position: 39
---

# Stackdriver / Google Cloud Monitoring (input)

## Overview

The `stackdriver` plugin is an in-process WebhookReceiver that accepts [Google Cloud Monitoring](https://cloud.google.com/monitoring/support/notification-options#webhooks) (formerly Stackdriver) incident webhook notifications. It maps each incoming incident to a `snoozetypes.Record` and submits it to the Snooze processing pipeline.

This is a **push** integration: GCP Monitoring calls Snooze, not the other way around. No credentials are required on the GCP side (the endpoint is unauthenticated by design, matching the policy of the Azure Monitor and New Relic receivers).

Incident state drives the Snooze severity and `State`:

- `open` fires a **critical** alert (or the incident's own `severity`, when present).
- `acknowledged` produces `State: "ack"` with the severity preserved.
- `closed` produces `State: "close"` with the severity downgraded to `ok`.

An optional `documentation.content` JSON blob can override any record field before the record is built — a flexible escape hatch that avoids writing a full rule.

## Configuration

The plugin registers itself automatically when the Snooze server loads. No additional server-side configuration is required. The inbound webhook URL is:

``` text
/api/v1/webhook/stackdriver
```

Full external example (replace the host):

    https://snooze.example.com/api/v1/webhook/stackdriver

### Wiring a GCP Monitoring notification channel

1.  In the Google Cloud Console, open **Monitoring → Alerting → Notification channels**.
2.  Under **Webhooks**, click **Add new**.
3.  Set the **Endpoint URL** to your Snooze webhook URL (e.g. `https://snooze.example.com/api/v1/webhook/stackdriver`).
4.  Leave authentication off (GCP Monitoring webhooks do not carry a shared secret — see [Notes & limitations](#notes--limitations) for network controls).
5.  Save the channel, then attach it to one or more **Alerting policies** (open the policy → **Notifications** → add the webhook channel).

When the alerting policy opens an incident, GCP POSTs an incident body to Snooze with `state: "open"`. When the incident is acknowledged or closed, GCP POSTs again with the updated `state`, and Snooze emits a record with `State: "ack"` or `State: "close"` so downstream processors can update the matching alert.

### curl example

The following command simulates a GCP Monitoring "open" incident notification:

``` console
$ curl -X POST https://snooze.example.com/api/v1/webhook/stackdriver \
    -H 'Content-Type: application/json' \
    -d '{
      "incident": {
        "incident_id": "0.abc123",
        "resource_name": "web-1",
        "resource_id": "1234567890",
        "condition_name": "CPU above 90%",
        "policy_name": "High CPU policy",
        "state": "open",
        "severity": "critical",
        "summary": "CPU usage for web-1 is above the threshold",
        "url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.abc123",
        "started_at": 1700000000,
        "ended_at": null
      }
    }'
```

Expected response:

``` json
{"accepted": 1, "received": 1, "status": "ok"}
```

### Field reference

The following table describes how GCP Monitoring incident fields map to Snooze record fields.

| Snooze field | Source | Notes |
|----|----|----|
| `Source` | (fixed) | Always `"stackdriver"` |
| `Host` | `incident.resource_name` | The monitored resource name. |
| `Process` | `incident.condition_name` | The specific alerting-policy condition that fired. |
| `Severity` | `incident.state` / `incident.severity` | See the state-mapping table below. |
| `State` | `incident.state` | See the state-mapping table below. |
| `Message` | `incident.summary` | Human-readable incident summary. |
| `Tags` | `incident.policy_name` | A single tag carrying the policy name, **only** when it is non-empty. |
| `Raw` | `incident_id`, `resource_id`, `url`, `started_at`, `policy_name`, `ended_at` | `ended_at` is included only when non-null (open incidents send `null`). |

#### State mapping

| GCP `state` | `Severity` | `State` |
|----|----|----|
| `open` | `severity` field, or `critical` when absent | *(empty)* |
| `acknowledged` | preserved (the incident's `severity`) | `ack` |
| `closed` | `ok` | `close` |
| anything else | `indeterminate` | *(empty)* |

### `documentation` override

If the incident carries a `documentation.content` string and that string parses as a JSON object, the recognised fields (`severity`, `summary`, `environment`, `origin`) overlay the incident **before** the record is built. This lets operators reshape a record straight from the alerting policy's documentation field without writing a Snooze rule.

``` console
$ curl -X POST https://snooze.example.com/api/v1/webhook/stackdriver \
    -H 'Content-Type: application/json' \
    -d '{
      "incident": {
        "incident_id": "0.abc123",
        "resource_name": "web-1",
        "condition_name": "CPU above 90%",
        "policy_name": "High CPU policy",
        "state": "open",
        "severity": "critical",
        "summary": "original summary",
        "started_at": 1700000000,
        "ended_at": null,
        "documentation": {
          "content": "{\"severity\":\"warning\",\"summary\":\"CPU elevated, auto-remediation in progress\"}"
        }
      }
    }'
```

The resulting record carries `severity: warning` and the overridden summary. If `documentation.content` is **not** valid JSON, the override is logged at WARN and ignored — the record is still built from the raw incident fields (no `400`, no panic).

## End-to-end test setup

The e2e test posts a realistic "open" incident payload to a live snooze-server instance and asserts a 2xx response.

Required environment variable:

`SNOOZE_E2E_STACKDRIVER_URL`  
Full URL to the Stackdriver webhook endpoint on the target snooze-server.

``` console
$ export SNOOZE_E2E_STACKDRIVER_URL=https://snooze.example.com/api/v1/webhook/stackdriver
$ go test -run TestStackdriverE2E ./internal/pluginimpl/stackdriver/...
```

## Notes & limitations

- **Signature verification** is not implemented. GCP Monitoring webhooks do not carry a shared-secret HMAC header (unlike PagerDuty), so request authenticity cannot be verified at the application layer. Protect the endpoint with network controls: a firewall / ingress allowlist restricting the source to Google's published webhook egress ranges, or a reverse proxy enforcing IP rules in front of Snooze.
- **`ended_at` is null for open incidents.** The plugin only writes `ended_at` into `Raw` when GCP sends a non-null value (i.e. for closed incidents).
- **The `severity` field is optional.** GCP Monitoring added it in 2022; older notification-channel configurations may omit it. When absent, an `open` incident defaults to `critical`.
- **Multitenancy** needs no special handling: the API router's tenant-extraction middleware runs before the webhook handler, exactly as for the Azure Monitor and CloudWatch receivers.
