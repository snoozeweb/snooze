---
sidebar_position: 9.5
---

# Pingdom (input)

## Overview

The **pingdom** plugin is an in-process WebhookReceiver that accepts [Pingdom](https://www.pingdom.com/) uptime / synthetic-check **state-change webhooks** and converts each notification into a single Snooze record. It is registered at `/api/v1/webhook/pingdom`.

Pingdom POSTs this payload whenever a monitored check changes state (UP ⇄ DOWN, or into PAUSED). The plugin maps the check name to the record host and the check type to the process, and derives the severity from Pingdom's `current_state` and `importance_level`. The record then flows through the standard Snooze processing pipeline (rules, snooze, notifications).

### State handling

| Pingdom event | Snooze output |
|----|----|
| `current_state = "DOWN"`, `importance_level = "HIGH"` | One open record; `Severity = "critical"`, no `State`. |
| `current_state = "DOWN"`, `importance_level = "LOW"` | One open record; `Severity = "warning"`, no `State`. |
| `current_state = "UP"` | One record with `State = "close"` and `Severity = "ok"`, closing the prior DOWN. |
| `current_state = "PAUSED"` / `"UNKNOWN"` (maintenance) | One record with `Severity = "unknown"` and no `State`. |

A DOWN event opens an alert; the matching UP event closes it via `State="close"`. PAUSED/UNKNOWN states are passed through as `unknown` so operators can route or suppress them in rules.

## Configuration

### Inbound URL

The plugin mounts at:

    /api/v1/webhook/pingdom

No authentication is required on this endpoint by default. See [Integrations](./index.md) and [Ingest configuration](../../configuration/ingest.md) for hardening options including a shared ingest token (`config.ingest.token`).

### Configuring Pingdom

1.  In the Pingdom app go to **Integrations → Integrations** and click **Add integration**.
2.  Choose **Webhook** as the integration type.
3.  Set the **URL** to `https://<snooze-host>/api/v1/webhook/pingdom`.
4.  Give the integration a name (e.g. `Snooze`) and **Activate** it.
5.  Attach the integration to the uptime checks you want to forward (in each check's **Connect integrations** / **Alerting** settings, enable the `Snooze` webhook).

Pingdom fires the webhook on every state change, so no extra "when status changes" toggle is needed beyond attaching the integration to the check.

### Curl examples

A DOWN event (high importance):

``` console
$ curl -s -X POST 'https://<snooze-host>/api/v1/webhook/pingdom' \
    -H 'Content-Type: application/json' \
    -d '{
      "check_id": 12345,
      "check_name": "My homepage",
      "check_type": "HTTP",
      "tags": [{"name": "production"}, {"name": "web"}],
      "previous_state": "UP",
      "current_state": "DOWN",
      "importance_level": "HIGH",
      "state_changed_timestamp": 1700000000,
      "description": "Host is Down",
      "long_description": "Real browser test failed (step 3/5)"
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

The matching UP (recovery) event, which closes the alert opened above:

``` console
$ curl -s -X POST 'https://<snooze-host>/api/v1/webhook/pingdom' \
    -H 'Content-Type: application/json' \
    -d '{
      "check_id": 12345,
      "check_name": "My homepage",
      "check_type": "HTTP",
      "previous_state": "DOWN",
      "current_state": "UP",
      "importance_level": "HIGH",
      "state_changed_timestamp": 1700000300,
      "description": "Host is Up"
    }'
```

### Field mapping

| Pingdom field | Snooze field | Notes |
|----|----|----|
| `"pingdom"` (constant) | `Source` | Always `"pingdom"`. |
| `check_name` | `Host` | The logical resource name. |
| `check_type` | `Process` | `"HTTP"`, `"TCP"`, `"DNS"`, etc. |
| `description` | `Message` | Short human text. |
| `current_state` + `importance_level` | `Severity` | `UP` → `"ok"`; `DOWN`+`HIGH` → `"critical"`; `DOWN`+`LOW` → `"warning"`; anything else (PAUSED/UNKNOWN/empty) → `"unknown"`. |
| `current_state == "UP"` | `State` | Set to `"close"` to close the prior DOWN; otherwise unset (open). |
| `tags` (`[{"name":"x"}, …]`) | `Tags` | Flattened to a `[]string` of tag names; non-object or name-less entries are skipped. |
| `check_id` | `Raw["check_id"]` | Retained for traceability. |
| `long_description` | `Raw["long_description"]` | Verbose detail, when present. |
| `state_changed_timestamp` | `Timestamp` | Unix epoch → UTC; the **event** time, not receive time. Absent/zero falls back to the server clock at receive time. |

### De-duplicating repeated fires

A flapping check can fire several DOWN events before it recovers. To collapse them onto a single alert rather than a flood, add an [aggregate rule](../rules.md) keyed on the fields `[host, source]`. Records sharing the same `host` (`check_name`) and `source` (`pingdom`) then aggregate onto one alert, and the eventual UP event (`State="close"`) closes it.

## Notes & limitations

- **Unauthenticated by default.** The endpoint accepts any POST from any source. Pingdom does not HMAC-sign the standard webhook, so restrict access at the network layer (Pingdom publishes its [probe IP ranges](https://www.pingdom.com/)) and/or configure a shared ingest token — see `config.ingest` / [Ingest configuration](../../configuration/ingest.md) and [Integrations](./index.md).
- **PAUSED / UNKNOWN are not suppressed automatically.** Maintenance-window states arrive as `Severity="unknown"` records. Operators who want to drop them should add a rule matching `severity == unknown AND source == pingdom`.
- **Graceful degradation on schema drift.** Pingdom's webhook is documented as v1 and is unversioned. Missing keys yield zero values rather than errors, so a partial or future-shaped payload still produces a valid record with `Source="pingdom"` and whatever fields could be extracted.
- **Timestamp fallback.** Older Pingdom plans may omit `state_changed_timestamp`; in that case `Record.Timestamp` reflects receive time rather than event time.
