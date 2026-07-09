---
sidebar_position: 40
---

# Snooze peer / federation (output)

## Overview

The **snoozepeer** plugin is an in-process Notifier that relays a matching alert to a downstream Snooze (or generic HTTP) peer. It is wired as a notification *Action* in the Snooze UI — labelled **Forward to another Snooze peer** — and is the mechanism behind **alert federation**: hub-and-spoke, active/active, or mesh topologies where one Snooze forwards accepted alerts to another.

When an alert matches a Notification rule that references a snoozepeer action, the plugin marshals the raw post-pipeline record as JSON and `POST`s it to the peer's `/api/v1/alerts`, which re-runs its own pipeline on the record. The plugin owns no database collection — its configuration lives entirely on the action — and reuses the [webhook](./webhook.md) plugin's authentication and HTTP-client code path.

Key capabilities:

- Relays the normalized record to any peer that accepts `POST /api/v1/alerts` (another Snooze, or a compatible HTTP endpoint).
- Optional per-peer authentication (Bearer / Basic / API key).
- Optional TLS certificate-verification bypass for private/self-signed endpoints.
- Loop prevention for cyclic topologies via the `X-Snooze-Loop` header.
- Fire-and-forget: a slow or failing peer never blocks or fails local ingestion.

## Configuration

Wire the plugin through a **Notification → Action** in the Snooze UI or configuration file. Set the action type to **Forward to another Snooze peer** (`snoozepeer`) and fill the `action_form` fields described below. Then attach the action to a **Notification** whose **condition** scopes which accepted alerts relay (leave the condition empty to relay everything).

### Action fields

| Field | Component | Default | Description |
|----|----|----|----|
| `endpoint` | String | *(required)* | The peer's ingest URL, e.g. `https://peer.example.com/api/v1/alerts`. The record is `POST`ed here verbatim. Not templated — it is a fixed URL, not a Go template. |
| `auth` | Object | *(optional)* | Optional per-peer authentication block. Supported shapes: `{type: bearer, token: "..."}`, `{type: basic, username: "...", password: "..."}`, or `{type: apikey, api_key: "...", header: "X-Api-Key"}`. |
| `tls_insecure` | Switch | `false` | Skip TLS certificate verification when calling an HTTPS peer. Use only for trusted private / self-signed endpoints. |
| `timeout` | Number | `30` | Per-relay request timeout in **seconds**. `0` falls back to the HTTP client's default. |

### Request shape

For each matching alert the plugin sends:

- **Body** — the full post-pipeline record, JSON-encoded.
- **`Content-Type: application/json`**.
- **`X-Snooze-Loop`** — the comma-separated chain of server ids the alert has already traversed (see [Loop prevention](#loop-prevention)), with this server's id appended.
- **`X-Snooze-Relayed-At`** — the relay timestamp in RFC 3339.

## Loop prevention

In a cyclic topology (A → B → A, or a mesh of hubs) Snooze prevents infinite relaying with the `X-Snooze-Loop` header — a comma-separated chain of the server ids an alert has already passed through. On each relay the plugin:

- **Suppresses** the relay (no-op) when **this server's own id is already in the chain** — the alert has come back around; it is accepted locally but not re-relayed. This is recorded as a `success` action outcome, not an error.
- **Skips** the relay when **the target peer's id is already in the chain** — that peer has already seen this alert.
- Otherwise appends this server's id to the chain and forwards.

A server's id is its `syncer.hostname`.

### HA prerequisite: distinct `syncer.hostname`

Loop detection keys on `syncer.hostname`. **Each federated node must set a distinct `syncer.hostname`** — if two nodes share one, loop detection breaks. Set it explicitly per node (the OS-hostname default is fragile in container/Kubernetes deployments):

``` yaml
# syncer.yaml on node A
hostname: hub-eu
```

## Example

``` yaml
endpoint:     "https://hub.example.com/api/v1/alerts"
auth:
  type:  bearer
  token: "eyJhbGci..."
tls_insecure: false
timeout:      30
```

``` yaml
# relay to a private peer with an API key, self-signed TLS
endpoint:     "https://peer.internal/api/v1/alerts"
auth:
  type:    apikey
  api_key: "s3cret"
  header:  "X-Api-Key"
tls_insecure: true
```

## Testing / verifying

1.  **Create a test action** in the Snooze UI (Actions → New → *Forward to another Snooze peer*) with `endpoint` pointing at a second Snooze instance's `/api/v1/alerts`, or a request-capture service such as `https://webhook.site`.

2.  **Create a matching Notification rule** that routes a specific condition (e.g. `source = "test"`) to the new action.

3.  **Send a test alert**:

        $ snooze alert source=test host=test-host severity=info \
            "message=federation smoke-test"

4.  **Verify the relay** arrived: on the peer Snooze, confirm the alert appears in the alert list; or, against a capture endpoint, confirm the `POST` body is the JSON record and that it carried the `X-Snooze-Loop` and `X-Snooze-Relayed-At` headers.

Per-alert relay outcomes (success / error per action) are tracked on the originating record when the notification has **Persist action outcomes** enabled — inspect the record in the Snooze UI after the notification fires.

## Notes & limitations

- **Fire-and-forget**: relay is dispatched by the notification worker and there are **no retries** (to avoid amplification storms). A slow or failing peer never blocks or fails local ingestion.
- The plugin returns an error for any HTTP status outside the `2xx` range and for a missing/blank `endpoint`; the failure is recorded as the action's error outcome.
- The `endpoint` is a fixed URL — unlike the [webhook](./webhook.md) plugin's `url`, it is **not** rendered as a Go template.
- The relayed body is the raw normalized record; the peer re-runs its own rules/aggregation pipeline on it.
- Loop prevention only works when every federated node has a **distinct `syncer.hostname`** — see the [HA prerequisite](#ha-prerequisite-distinct-syncerhostname) above.
