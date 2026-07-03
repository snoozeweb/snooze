---
sidebar_position: 15.5
---

# Alert federation

## Overview

Alert federation lets one Snooze server mirror accepted alerts to one or more
downstream Snooze (or generic HTTP) peers. Operators use it to build
hub-and-spoke or active/active topologies — for example a per-site server that
relays everything to a central hub, or two regional hubs that exchange alerts —
without scripting per-action webhooks.

Federation runs **after** the pipeline accepts a record: it relays only what
survives `reject`, `snooze`, aggregation, and the rest of the pipeline, and it
relays the **post-pipeline record JSON** so the peer ingests an already-normalized
alert through its own `POST /api/v1/alerts` endpoint.

Relay is **fire-and-forget**: a slow or failing peer never blocks or fails local
ingestion. The local alert is always persisted; the relay POST runs on a detached,
bounded-timeout goroutine and a peer error is only logged.

:::info

Federation is distinct from [Clustering](./clustering.md). Clustering replicates
**configuration** between nodes of one cluster (one shared database). Federation
relays **alerts** between independent Snooze deployments (separate databases),
each of which re-runs its own pipeline on the incoming alert.

:::

## The `forward` collection

Destinations are CRUD-able rows in the `forward` collection, edited at
`/api/v1/forward` (or in the web UI) and **hot-reloaded** — an edit takes effect
on the next alert, no restart needed. One document per downstream peer:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Destination name (primary key; `duplicate_policy: reject`). |
| `enabled` | bool | no | Defaults to `true`. Set `false` to soft-disable without deleting. |
| `endpoint` | string | yes | Peer URL, e.g. `https://peer.example.com/api/v1/alerts`. |
| `condition` | Condition | no | Standard [condition DSL](./conditions.md) scoping which alerts relay. Empty = relay everything. |
| `event_classes` | string[] | no | `["*"]` or `["alerts"]`. Only `alerts` is meaningful today; unknown values are rejected at write time. Empty defaults to covering alerts. |
| `auth` | object | no | Per-destination authentication (see below). |
| `tls_insecure` | bool | no | Skip TLS certificate verification for this peer (lab/self-signed only). |
| `timeout` | integer | no | Per-relay request timeout in seconds. Defaults to the built-in relay timeout. |

### Authentication

The `auth` sub-document selects how the relay POST authenticates to the peer.
Three schemes are supported (Hawk-HMAC is **not**):

| `auth.type` | Fields | Effect |
|---|---|---|
| `""` (omitted) | — | No authentication header. |
| `bearer` | `token` | Sends `Authorization: Bearer <token>`. |
| `basic` | `username`, `password` | Sends `Authorization: Basic <base64(user:pass)>`. |
| `apikey` | `api_key`, `header` | Sends `<header>: <api_key>`. `header` defaults to `X-API-Key`. |

Every scheme **fails closed**: a `bearer` with no `token`, a `basic` with no
`username`, or an `apikey` with no `api_key` is an error and the relay to that
destination is skipped (and logged) rather than sent unauthenticated.

When relaying to another Snooze server, point the peer's
[ingest token](./ingest_tokens.md) at the destination — the peer re-resolves the
tenant from the credential it receives, so the configured auth must map to the
intended tenant on the peer.

### Example

```json
{
  "name": "central-hub",
  "enabled": true,
  "endpoint": "https://hub.example.com/api/v1/alerts",
  "condition": ["=", "severity", "critical"],
  "event_classes": ["alerts"],
  "auth": { "type": "bearer", "token": "<hub-ingest-token>" }
}
```

This relays only `critical` alerts to the hub, authenticating with the hub's
ingest token.

## Loop prevention with `X-Snooze-Loop`

In a cyclic topology (A → B → A, or a mesh of hubs) a naive relay would loop
forever. Snooze prevents this with the `X-Snooze-Loop` request header — a
comma-separated chain of the server ids an alert has already passed through.

On each relay:

1. The relaying server reads the inbound `X-Snooze-Loop` chain (empty for a
   directly-submitted alert).
2. If this server's own id is **already in the chain**, the alert is accepted and
   persisted locally (HTTP 200) but is **not** re-relayed — the loop is broken.
3. Otherwise the server appends its own id to the chain and relays to each
   matching destination, skipping any destination whose `name` already appears in
   the inbound chain.

A server's id is its `syncer.hostname` (see [Syncer configuration](../configuration/syncer.md)).

## HA prerequisite: distinct `syncer.hostname`

Loop detection keys on `syncer.hostname`. **Each federated node must set a
distinct `syncer.hostname`** — if two nodes share a hostname, one could mistake a
peer's alert for its own and drop relays it should forward (or fail to break a
loop). Set it explicitly per node:

```yaml
# syncer.yaml on node A
hostname: hub-eu
```

```yaml
# syncer.yaml on node B
hostname: hub-us
```

The default falls back to the OS hostname, which is usually distinct already, but
relying on that is fragile in container/Kubernetes deployments where hostnames may
collide — set it explicitly.

## Managing destinations in the console

Forward destinations can be created, edited, enabled/disabled, and deleted from
the web console at **Admin → Federation** (`/web/admin/forward`).

### Admin page

The list page shows all configured destinations with columns: name, enabled
status, endpoint URL, auth type, condition summary, and event classes. Click any
row to open the editor drawer.

### Editor sections

The drawer has four sections:

1. **Identity** — The destination name (primary key; must be unique). The Enabled
   toggle appears beside the title and can be flipped without opening the full
   drawer.

2. **Destination** — Endpoint URL (where the relay POST is sent) and event classes
   selector (`All (*)` or `Alerts`). Only these two values are accepted by the
   backend.

3. **Condition** — Optional [condition DSL](./conditions.md) to scope which alerts
   are relayed. Leave empty (ALWAYS_TRUE) to relay all accepted alerts.

4. **Advanced** (collapsible, closed by default) — Auth sub-form, TLS verification
   toggle, and a timeout field (Go duration string, e.g. `30s`, `1m`).

### Auth sub-form

Select the authentication type from the dropdown in the Advanced section:

| Type | Fields shown |
|---|---|
| None | — |
| Bearer token | Token (password field) |
| Basic | Username + Password |
| API key | Key value + Header name (defaults to `X-API-Key`) |

### HA prerequisite reminder

:::warning

Each Snooze node in an HA cluster must set a distinct `syncer.hostname`.
Nodes that find their own hostname in the inbound `X-Snooze-Loop` header accept
the alert without re-relaying — but if two nodes share a hostname, loop detection
breaks. See the HA prerequisite section above for details.

:::

## Caveats

- **No retries / dead-letter.** Relay is fire-and-forget with no retry, matching
  the upstream Alerta behaviour, to avoid amplification storms. A peer that is
  down simply misses those alerts.
- **Tenant scoping.** A `forward` destination is tenant-scoped; the relay runs
  with the record's tenant context, so cross-tenant leakage cannot happen via the
  destination cache. The peer re-resolves the tenant from the credential it
  receives.
