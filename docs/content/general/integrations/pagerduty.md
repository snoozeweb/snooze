---
sidebar_position: 32
---

# PagerDuty (output)

## Overview

The PagerDuty integration is an **output** (Notifier) plugin that forwards Snooze alerts to PagerDuty using the [Events API v2](https://developer.pagerduty.com/docs/events-api-v2/overview/). It runs in-process as part of `snooze-server`; no additional daemon is required.

For each matching record the plugin posts a `trigger` or `resolve` event to `POST {api_base}/v2/enqueue`. A `resolve` is sent when `record.State` equals `"close"`; every other state sends a `trigger`. The `dedup_key` field is derived from the record's `hash` (set by the Aggregate Rule plugin) so a later resolve correlates correctly to the original trigger in PagerDuty.

### Severity mapping

Snooze uses syslog-style severity words; PagerDuty accepts exactly four values (`critical`, `error`, `warning`, `info`). The mapping is:

| Snooze severity             | PagerDuty |
|-----------------------------|-----------|
| emergency, critical         | critical  |
| error, err                  | error     |
| warning                     | warning   |
| notice, info, debug         | info      |
| (unknown / empty) + trigger | critical  |
| (unknown / empty) + resolve | info      |

Set **Severity** to `auto` (the default) to derive the value automatically. Set it to an explicit value to override the mapping for all alerts routed through a particular action.

## Configuration

Wire the integration through a Snooze notification action. The action_form fields are:

| Field | Required | Description |
|----|----|----|
| Routing Key | yes | PagerDuty integration key (Events API v2). Stored as a `Password` field; never shown after saving. |
| Severity | no | `auto` (default) or one of `critical` / `error` / `warning` / `info`. |
| Client | no | Label shown in PagerDuty for the originating app. Default: `Snooze`. |
| Client URL | no | URL of the Snooze instance (shown in PagerDuty). |
| API Base URL | no | Override for private service regions. Default: `https://events.pagerduty.com`. |
| Timeout | no | HTTP request timeout as a Go duration. Default: `10s`. |

### Field reference

``` yaml
routing_key: "r0k3y0000000000000000000000000001"   # required
severity: auto           # auto | critical | error | warning | info
client: Snooze
client_url: https://snooze.example.com
api_base: https://events.pagerduty.com
timeout: 10s
```

The plugin constructs a `payload.summary` of the form:

``` text
<severity> on <host>: <message>
```

truncated to 1 024 Unicode code points (the PagerDuty limit).

`payload.custom_details` carries a compact map of record metadata (UID, source, process, environment, hash, tags, raw fields) for use in PagerDuty event rules or responder notes.

## Inbound webhook (status sync)

### Overview

The PagerDuty integration is **bidirectional**. In addition to the outbound notifier above, Snooze exposes an inbound webhook receiver at `POST /api/v1/webhook/pagerduty`. When an operator acknowledges or resolves an incident in PagerDuty, PagerDuty posts a [webhook v2](https://developer.pagerduty.com/docs/webhooks/webhooks-overview/) payload to that endpoint, and Snooze patches the matching record's `state` automatically — so the on-call engineer does not have to also close or ack the alert in the Snooze console. The notification and aggregate-rule pipelines then react naturally (suppression on ack/close, re-escalation if the ack lapses).

This is a synchronous **state-sync** path, not an alert-create path: each message patches an existing record directly. It never creates new records.

### Setup

In PagerDuty, add a **Generic V2 Webhook** extension (Service → Integrations → Add → Generic Webhook, or an account-level webhook subscription) pointing at:

``` text
https://<snooze-host>/api/v1/webhook/pagerduty
```

Subscribe it to the incident lifecycle events you want mirrored (acknowledge, resolve, trigger, unacknowledge, escalate). Snooze tolerates extra event types — they are ignored as a no-op.

### Supported event types

| PagerDuty event type | Snooze state | Effect |
|----------------------|-------------|--------|
| `incident.acknowledge` | `ack` | suppresses notifications |
| `incident.resolve` | `close` | closes the record |
| `incident.trigger` | (cleared) | re-open (no state = firing) |
| `incident.unacknowledge` | (cleared) | re-open |
| `incident.escalate` | (cleared) | re-open |
| `incident.assign`, `incident.delegate` | — | ignored (no-op) |

Unknown / future event types are a **fail-open no-op**: the endpoint returns `200 OK` with `updated: 0` and writes nothing, so a Snooze upgrade never causes PagerDuty to retry forever.

A re-open truly clears the `state` field (it is removed, not set to an empty string), matching how Snooze clears state elsewhere — so the record returns to the firing state.

### Dedup-key convention

The webhook locates the record by the incident's `data.incident.incident_key`. That value is exactly the `dedup_key` the outbound notifier set when it triggered the incident: the record's **`hash`** (the Aggregate Rule deduplication key) when populated, falling back to the record's **`uid`**. The receiver therefore looks the record up by `hash` first and by `uid` second — the same priority the outbound side uses — so a round-trip (Snooze triggers → operator acks in PagerDuty → Snooze acks the record) correlates correctly.

A `200 OK` response carries `{"status":"ok","updated":N}` where `N` is the number of records patched. A payload whose `incident_key` matches no record returns `404`; a bad/empty payload returns `400`; a DB write failure returns `500`.

### Authentication

Like every Snooze webhook receiver, this endpoint is **unauthenticated by default** and relies on network isolation. When a shared `ingest.token` is configured it must be supplied as `Authorization: Bearer <token>` (or `?token=<token>`); a per-tenant `ingest_token` routes the payload to a specific tenant. PagerDuty's webhook signature (`X-PagerDuty-Signature`) is **not** validated in this release.

## End-to-end test setup

The package ships an env-gated e2e test in `e2e_test.go`. When the env var is absent, the test is skipped automatically so `go test ./...` stays green in CI.

**Steps:**

1.  In your PagerDuty account, open (or create) a service.
2.  Add a new integration: choose **Events API v2** and copy the **Integration Key** (also labelled *Routing Key* in some UIs).
3.  Export the key and run the test:

``` console
$ export SNOOZE_E2E_PAGERDUTY_ROUTING_KEY="<your-routing-key>"
$ go test -v -run TestPagerDutyE2E ./internal/pluginimpl/pagerduty/...
```

The test triggers a warning-severity event, waits 2 seconds, then resolves it using the same `dedup_key` (`snooze-e2e-test-dedupkey`). Check your PagerDuty service's activity log to confirm the incident was opened and immediately resolved.

Environment variables read by the e2e test:

| Variable | Purpose |
|----|----|
| `SNOOZE_E2E_PAGERDUTY_ROUTING_KEY` | Events API v2 integration/routing key. **Required** — test is skipped when unset. |

## Notes & limitations

- **HTTP/HTTPS only.** The plugin uses `net/http` with the system TLS trust store. Self-signed PagerDuty endpoints (on-premises PagerDuty) are not supported without a custom `api_base` pointing to a trusted endpoint.
- **No gRPC.** The PagerDuty Events API v2 is HTTP/JSON only, so no gRPC variant is needed.
- **Rate limits.** PagerDuty imposes an inbound rate limit per routing key. Use Snooze's Aggregate Rule plugin to deduplicate noisy alerts before they reach this notifier.
- **\`\`links\`\` field.** The `links` array defined by the Events API v2 spec is not exposed as an action_form knob today. Add a custom webhook action chained before the PagerDuty action if you need to attach URLs.

## Re-escalation

A [re-escalation](../escalation.md) is another `trigger` on the **same dedup
key**, so PagerDuty appends it to the incident it already has instead of opening
a second one. The dedup key is derived from the alert alone and never from the
escalation — folding anything escalation-specific into it would split one
incident into a queue of identical ones.

The escalation count, reason, escalating operator and previous severity travel
in `custom_details`, where responders can read them and event rules can route
on them. Nothing is added on a first delivery.
