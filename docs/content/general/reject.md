---
sidebar_position: 8
---

# Reject policies

## Overview

Reject policies are a hard gate that fires **before** an alert is persisted. When an
inbound alert matches an enabled policy rule, the pipeline aborts immediately, no
record is written to the database, and the sender receives an HTTP **422
Unprocessable Entity** response with code `policy_rejected` and the matching rule's
name in the message.

This differs from a [Snooze](./snooze.md) discard rule, which silently drops the
alert and returns HTTP 200. Use reject policies when the sending agent needs
actionable feedback about misconfigured alerts — for example, a scraper or push
integration that should stop sending alerts from a decommissioned source.

Pipeline position: `reject` runs **before** `rule` and `snooze` (lexicographic
order), so the gate fires as early as possible.

## Rule schema

Each reject rule document stored in the `reject` collection has the following fields:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Human-readable policy name. Returned in the 422 message. |
| `enabled` | bool | no | Defaults to `true`. Set to `false` to soft-disable a rule without deleting it. |
| `condition` | Condition | no | Standard Snooze [condition DSL](./conditions.md). Leave blank to match every alert. |

## HTTP response on rejection

When every record in a batch is rejected, the endpoint returns:

```
HTTP/1.1 422 Unprocessable Entity
Content-Type: application/json

{
  "error": {
    "code": "policy_rejected",
    "message": "policy_rejected: blacklist-untrusted-source"
  }
}
```

For a **mixed batch** (some records pass, some are rejected) the endpoint returns
HTTP 200 with the accepted records in `data` and the rejection reasons in `errors`:

```json
{
  "data": [{ "host": "allowed-host", ... }],
  "errors": ["policy_rejected: blacklist-untrusted-source"]
}
```

## Example: blacklist a source pattern

Create a rule that rejects any alert whose `source` field starts with the string
`legacy-`:

```yaml
name: blacklist-legacy-sources
enabled: true
condition: ['regex', 'source', '^legacy-']
```

Once saved, a POST to `/api/v1/alerts` with `"source": "legacy-syslog01"` returns:

```json
{
  "error": {
    "code": "policy_rejected",
    "message": "policy_rejected: blacklist-legacy-sources"
  }
}
```

## Managing reject rules in the console

Reject rules are managed in the web console under the **Reject** tab on the
**Rules** page (alongside the **Rules** and **Aggregates** tabs) — there is no
separate nav entry. The tab lists every reject rule as a flat table and lets you:

- **Create** a rule with **+ New** — set a `name`, the **Enabled** toggle, and a
  `condition` using the standard condition editor.
- **Edit** a rule by clicking its row, which opens the same drawer pre-filled.
- **Enable / disable** a rule from the editor's Enabled toggle (a disabled rule is
  kept but not evaluated).
- **Delete** one or many rules from the row context menu or the bulk selection.

A reject rule has exactly three fields — `name`, `enabled`, and `condition` — so the
editor has **no** modifications (those are Rules), aggregate fields, or time
constraints (those are Snoozes). The `condition` field accepts the same DSL used by
Snooze filters and Aggregate rules — see [Conditions](./conditions.md) for the full
syntax. Reject is a flat first-match list (evaluation order is the backend's, not
user-orderable), so there is no drag-to-reorder as there is for the modify-rule tree.

### Reject vs Rules vs Aggregates vs Snoozes

The Rules page hosts three ingest-time tools; Snoozes is the time-based silencer.
Use the one that matches the intent:

| Tool | What it does | Returns to sender |
|---|---|---|
| **Reject** | Drops a matching alert at ingest, **before** Rules — nothing is persisted. | HTTP **422** (`policy_rejected`) — the sender is told. |
| **Rules** | Transforms a matching alert (set/delete fields, …), then continues the pipeline. | HTTP 200 — the alert is stored. |
| **Aggregates** | Collapses duplicate alerts into one record by a key field. | HTTP 200 — de-duplicated. |
| **Snoozes** | Silences matching alerts for a **time-bounded** window (optionally discarding). | HTTP 200 — silently snoozed/discarded. |

For **time-bounded** silencing — quiet a noisy source until Monday, mute during a
maintenance window — use a [Snooze](./snooze.md), not a reject rule. Reject is a
permanent, terminal gate that reports the rejection back to the sender.
