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

## Web interface

Use the **Reject** section of the administration panel to create, edit, enable, or
disable policy rules. The `condition` field accepts the same DSL used by Snooze
filters and Aggregate rules — see [Conditions](./conditions.md) for the full syntax.
