---
sidebar_position: 1.5
---

|                              |
|------------------------------|
| Custom source mapping (input) |

# Custom source mapping

## Overview

Not every monitoring tool speaks Snooze's canonical alert shape. A tool might
post `{"alertname": "DiskFull", "node": "web-1", "level": "critical"}` where
Snooze expects `host`, `severity`, and `message`. You do **not** need to write
or compile a Go plugin to onboard such a source. The generic
[`POST /api/v1/alerts`](./rest-api.md) endpoint already accepts arbitrary JSON,
and the rules engine can rename foreign fields onto the canonical schema at
ingest time.

This page documents the no-Go onboarding recipe: point the tool at the alert
endpoint, add a rule tree that remaps the fields, and optionally archive the
original payload with the `_preserve_raw` ingest hint.

## How it works — the three-step recipe

1. **Point the tool at the alert endpoint.** Configure your source to
   `POST` its JSON to `https://snooze.example.com/api/v1/alerts` with
   `Content-Type: application/json`. A single object or an array of objects is
   accepted. Any key that is not a canonical field is preserved on the record
   and made available to rules and templates (see the
   [REST API](./rest-api.md) reference).

2. **Write a rule tree that maps the foreign fields.** Add a
   [rule](../rules.md) whose condition matches the source and whose
   modifications `SET` the canonical fields from the foreign ones (using
   `{{ field }}` templating), then `DELETE` the foreign keys so they don't
   linger on the record.

3. **(Optional) Archive the original payload.** Add `"_preserve_raw": true`
   to the posted body. Snooze copies every unrecognised key into the record's
   `raw` field **before** any rule runs, so the verbatim foreign payload is kept
   for audit even after step 2 renames and deletes the original keys.

## Worked example — onboarding `my-tool`

Suppose `my-tool` posts:

``` json
{
  "source":        "my-tool",
  "alertname":     "DiskFull",
  "node":          "web-1",
  "level":         "critical",
  "_preserve_raw": true
}
```

Seed this rule (via the UI, or `POST /api/v1/rule`):

``` json
{
  "name":      "my-tool-mapping",
  "condition": ["=", "source", "my-tool"],
  "modifications": [
    ["SET",    "host",     "{{ node }}"],
    ["SET",    "severity", "{{ level }}"],
    ["SET",    "message",  "{{ alertname }}"],
    ["DELETE", "node"],
    ["DELETE", "level"],
    ["DELETE", "alertname"]
  ]
}
```

The record that lands in Snooze is:

``` json
{
  "source":   "my-tool",
  "host":     "web-1",
  "severity": "critical",
  "message":  "DiskFull",
  "raw": {
    "alertname": "DiskFull",
    "node":      "web-1",
    "level":     "critical"
  }
}
```

The canonical fields (`host`, `severity`, `message`) are populated from the
foreign keys, the foreign keys themselves are gone from the top level, and the
original payload is intact under `raw`.

:::info Ordering matters
`_preserve_raw` is processed in the ingest layer **before** the rule pipeline
runs. That is why `raw.node` survives even though the rule `DELETE`s `node` from
the top-level record: the copy into `raw` already happened by the time the
rule's `DELETE` executes.
:::

## Modification reference

Field remapping is done with the modification DSL. Each modification is a
positional list `["OP", ...args]`; string args support `{{ field }}` templating
against the current record view. The available operations:

| Op | Form | Use case for field mapping |
|----|----|----|
| `SET` | `["SET", field, value]` | Write a canonical field from a foreign one, e.g. `["SET", "host", "{{ node }}"]`. The value may be a literal or a `{{ template }}`. |
| `DELETE` | `["DELETE", field]` | Drop a foreign key once its value has been copied to a canonical field. |
| `REGEX_PARSE` | `["REGEX_PARSE", field, pattern]` | Split a compound field into named capture groups merged back onto the record, e.g. parse `"web-1:eth0"` into `host` and `interface`. |
| `REGEX_SUB` | `["REGEX_SUB", field, out_field, pattern, replacement]` | Rewrite a field's value into `out_field` via regex substitution (normalise a prefix, strip a suffix, …). |
| `ARRAY_APPEND` | `["ARRAY_APPEND", field, value]` | Append an element to a list field (e.g. add a routing tag to `tags`). |
| `ARRAY_DELETE` | `["ARRAY_DELETE", field, value]` | Remove a matching element from a list field. |
| `KV_SET` | `["KV_SET", dict, key_field, out_field]` | Look up `kv[dict][record[key_field]]` and assign it to `out_field` — e.g. normalise a vendor severity string, or route to a team from a hostname. **Requires a populated KV store**; see [Key-value store](../kv.md). |

See [Rules](../rules.md) for the full rule-tree model (nesting, ordering,
condition syntax).

## The `_preserve_raw` ingest hint

`_preserve_raw` is a reserved ingest hint, not a stored field. When a posted
body carries `_preserve_raw` with a truthy value (`true`, `1`, `"1"`, `"true"`),
Snooze copies every unrecognised (non-canonical) key into the record's `raw`
field, then drops `_preserve_raw` from the record entirely. It is the one-line
escape hatch for keeping the full original payload without writing any rule:

- An explicitly-posted `raw` object is **not** clobbered — its existing entries
  win, and the foreign keys are merged in alongside them.
- The copy runs in the ingest layer, **before** rules, so it captures the
  original shape even when rules later rename or delete fields.
- A falsy or absent `_preserve_raw` is a no-op: `raw` stays empty unless you set
  it explicitly. This keeps the hint opt-in, so records that legitimately use
  extra fields for pipeline state are not bloated.

## When to graduate to a compiled WebhookReceiver

The rule-based recipe covers the common case: a tool that can `POST` flat JSON
and whose fields map cleanly onto the canonical schema. Reach for a
compiled-in `WebhookReceiver` plugin (see
[`internal/pluginimpl/AGENTS.md`](https://github.com/snoozeweb/snooze)) instead
when the source:

- speaks a non-standard HTTP envelope (a custom verb, a nested payload that
  needs unpacking into one record per element);
- delivers alerts in an array wrapper that must be split into individual
  records before the pipeline sees them;
- requires signature/HMAC verification of the request body;
- needs stateful or multi-step transformation that the modification DSL cannot
  express.

A `WebhookReceiver` mounts at `/api/v1/webhook/{name}` and owns the decode step
in Go. For everything short of that, the rule-tree + `_preserve_raw` recipe on
this page is the lighter, no-deploy path.
