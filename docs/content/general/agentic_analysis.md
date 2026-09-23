---
sidebar_position: 10.5
---

# Agentic analysis & protected fields

An **agentic analysis** is the machine-authored answer to the two questions an
operator asks about any alert: *why did this fire* and *what do I do about it*.
It is stored on the alert itself, under the `agentic` field, and it is written
through one dedicated endpoint that validates its shape.

`agentic` is the first **protected field**: a document key that only that
endpoint may write. Every other write path in Snooze treats it as read-only.

## The shape

```json
{
  "agentic": {
    "root_cause": {
      "summary": "jwt-key-sync OOMKilled at its 64Mi limit",
      "scope": "ovh/monitoring/jwt-key-sync",
      "evidence": [
        "kubectl describe pod: Last State Terminated, Reason: OOMKilled",
        "container_memory_max_usage_bytes peaked at 67Mi"
      ],
      "confidence": "high"
    },
    "remediation_plan": {
      "steps": [
        {
          "action": "Raise the memory limit to 128Mi",
          "command": "kubectl -n monitoring set resources deploy/jwt-key-sync --limits=memory=128Mi",
          "risk": "low"
        }
      ],
      "rollback": [
        {"action": "Revert the limit to 64Mi", "risk": "low"}
      ],
      "automatable": true
    },
    "analysis": {
      "at": "2026-09-21T09:40:00Z",
      "by": "alert-agent",
      "source": "alert-rca"
    }
  }
}
```

Length limits count characters, not bytes. The schema is deliberately small
and closed, because both ends of it are machines: an agent writing it should spend its budget investigating rather
than formatting, and an agent reading it wants fixed keys and closed enums it
can branch on.

| Field | Required | Notes |
|---|---|---|
| `root_cause.summary` | yes | One sentence, ≤ 500 characters. |
| `root_cause.scope` | no | What is broken, addressed however fits the alert. ≤ 200 characters. |
| `root_cause.evidence` | no | Up to 10 short observations, ≤ 500 characters each. |
| `root_cause.confidence` | yes | `high` \| `medium` \| `low`. |
| `remediation_plan.steps` | yes | 1–20 steps, each `{action, command?, risk}` (`action` ≤ 500, `command` ≤ 1000). |
| `remediation_plan.rollback` | no | Up to 20 steps, same shape. |
| `remediation_plan.automatable` | no | True only when the steps are safe to run unattended. |
| `analysis` | — | Stamped by the server; a client that sends it gets a 422. |

The request body is capped at 512 KiB; a maximal, schema-valid payload fits
well inside that. The cap exists only to stop a runaway client streaming
megabytes at the endpoint before validation even gets a chance to reject it.
The decoder also rejects trailing data after the JSON object, reports a wrong
type (a numeric `confidence`, say) as a field-keyed validation error like any
other, and refuses strings containing the NUL character — they cannot be
stored on PostgreSQL.

`confidence: low` is the right answer for an investigation that did not
conclude. Recording the trail and saying so beats leaving the alert to be
re-investigated from scratch by the next agent that walks past it.

## Reading and writing

```bash
snooze record agentic get   <uid>
snooze record agentic set   <uid> '<json>'      # or --file analysis.json, --file -
snooze record agentic clear <uid>
```

The same surface over HTTP:

| Verb | Path | Permission |
|---|---|---|
| `GET` | `/api/v1/record/{uid}/agentic` | `ro_record` (or `rw_record`, or a wildcard) |
| `PUT` | `/api/v1/record/{uid}/agentic` | `rw_protected`, literally |
| `DELETE` | `/api/v1/record/{uid}/agentic` | `rw_protected`, literally |

A write replaces the whole subtree — a re-analysis with fewer evidence items
does not leave the old ones behind. It does **not** touch `date_epoch`:
analysing a months-old alert must not make it look freshly seen.

Validation reports every violation at once, keyed by JSON path, so a payload
is fixed in one round-trip rather than five:

```json
{
  "error": {
    "code": "validation_error",
    "message": "agentic payload failed schema validation",
    "details": {
      "root_cause.confidence": "must be one of high|medium|low",
      "remediation_plan.steps[0].risk": "is required (low|medium|high)"
    }
  }
}
```

Unknown fields are rejected rather than dropped: an agent that misspells
`remediation` should be told, not left believing it stored something.

AI assistants reach the same endpoint through the MCP server
(`snooze-mcp`): the `get_alert_analysis` and `set_alert_analysis` tools.

## In the web UI

The alert inspector carries an **Analysis** tab, between Flow and Deliveries.
It reads the same endpoint, so it shows whatever the agent loop last wrote: the
cause, the scope, the evidence, the ordered plan with each step's risk and
command, and whether the plan is automatable. The tab label carries the
confidence (`Analysis · High`), and the inspector's header gains a one-line
`Cause:` summary above the tabs — a reader who only wants the verdict never has
to open the tab.

Anyone who can read the alert can read its analysis. **Edit**, **Remove** and,
on an unanalysed alert, **Write analysis** appear only for an identity holding
`rw_protected` — literally, so an `rw_all` admin sees the read-only view, the
same answer the endpoint would give. A save is a replace, not a merge: it
overwrites the whole subtree and restamps provenance with the signed-in user
and `source: snooze-web`, including when it corrects an analysis an agent
wrote. The editor validates locally against the rules above before it sends, and
places the server's field-keyed 422 on the same fields.

In the alerts table, an analysed alert carries a small dot beside its severity
badge, coloured by confidence, titled `Analysed · low confidence`. It rides the
severity cell rather than taking a column, so it costs the message no width.

The dashboard has two views, switched from the segmented control next to its
title: **Overview** and **Analyses**. The Analyses view lists the open alerts
somebody has already explained; the "Right now" tile strip counts them on an
**Analysed** tile ("17 of 42 open") that opens the view when clicked. The view
rides in the URL, so `/web/dashboard?view=analyses` is a deep link — and because
the list is live rather than windowed, the time-range picker is not on screen
there. Both surfaces read the record collection, so they are offered only to an
identity holding `ro_record` (or `rw_record`); a stats-only role gets the
Overview, with no switch and no tile, and that deep link lands on the Overview
too.

The view lists **every** analysed alert — it is a work queue, not a top-N — so
it opens straight onto the list with no headline above it. The ratio against the
open backlog is the **Analysed** tile's job ("17 of 42 open"), where the number
sits next to the other Right-now counts it is meant to be read against. A safety
ceiling of 500 rows still applies; on the rare run that hits it, the view says
so ("Showing 500 of 812 analysed alerts") rather than truncating in silence.

Rows are newest analysis first, and a row is not a table row: it is a header bar
over two blocks. The header names the alert on the left (a severity rail down
the row, the severity word, the host, the alert name) and sets everything scalar
on the right of the same line — a **confidence** chip and, where the plan claims
it, an **Automatable** one, then the step count, when it was written and by whom — so confidence reads as a column down a screenful of
rows. Below it, **the root cause and the remediation plan sit side by side**
under a `Why` / `What to do` pair of labels. Three regions, three hairlines: one
under the header, one between the cause and the plan, and the rail.

The plan prints its steps in order as one aligned grid — ordinal, action, risk —
with each step's command on a second line in mono. The risk tag appears **only
above `low`**: a plan whose every step is tagged `LOW` says nothing and buries
the one step that is not, and the column costs no width when no step needs it.
Four steps show; a longer plan ends with `+N more steps` and defers to the
inspector. A very long cause clamps at five lines, for the same reason.

A subtree missing any of that still gets a row (an em-dash cause, an em-dash
plan, no confidence badge), because the server counted it. The confidence and
automatable chips narrow the list in place. Below ~880px the two columns become
one: the cause is the paragraph above the plan. Clicking a row opens that alert with its inspector already on the
Analysis tab, pinning the table to that one alert so an old alert with a fresh
analysis is never off the first page:
`/web/alerts?tab=all&record=<uid>&analysis=1&search=uid = "<uid>"`.

## Protected fields

Protection is a property of the field name, and it is **recursive**:
protecting `agentic` protects `agentic.root_cause.summary` and everything else
beneath it. The registry lives in code (`internal/protected`) rather than in
configuration — an operator-editable protection list would be one typo away
from unprotecting the data the concept exists to protect.

What each write path does with a protected field:

| Path | Behaviour |
|---|---|
| `PUT /api/v1/record/{uid}/agentic` | The one writer. Validates, stamps provenance, audits. |
| Alert ingestion (any input, any daemon) | **Strips** the field and logs a warning; the alert is processed normally. |
| `POST /api/v1/{plugin}` (create) | **403 `protected_field`**, naming the field, always. |
| `PUT`/`PATCH /api/v1/{plugin}/{uid}` | **403 `protected_field`** unless the body's value is byte-identical to what is already stored — a read-modify-write client that echoes the record back is not a write. A different value, or any value on a record that currently has none, is still 403. |
| `PUT /api/v1/{plugin}/{uid}` omitting the field | The stored value is carried forward, so a replace cannot be a back-door delete. |
| `POST /api/v1/{plugin}/bulk_update` (`set`) | **403**, same reason. |
| Rule modifications (`SET`, `DELETE`, `ARRAY_APPEND`, `ARRAY_DELETE`, `REGEX_SUB`, `REGEX_PARSE` capture groups, `KV_SET` out_field) | Refused when saving the rule; also refused at runtime, which catches a field name computed from a template. |
| Notifier `inject_response` / notify-ref stamps | Refused at the dispatcher's inject chokepoint. |

Ingestion strips where the API refuses, and the difference is deliberate. A
sender that happens to emit a key named `agentic` must not be able to break its
own alerting; an operator or agent calling the API deliberately deserves an
error it can act on.

The echo-back exception on `PUT`/`PATCH` exists for the same reason: a
generic "edit this alert" tool that round-trips the whole document must not
break the moment the alert has been analysed.

A re-fire of an already-analysed alert keeps its analysis: the pipeline's final
write is a merge, and a key the incoming alert does not mention is left alone.

One nuance worth stating outright: an alert sent with the `_preserve_raw`
ingest hint archives its unrecognised keys under `raw` *before* the strip runs,
so a sender that posts `agentic` ends up with a copy at `raw.agentic`. That is
the documented purpose of `raw` — a verbatim archive of what the sender
actually sent — and it is inert: it is not the protected field, nothing
promotes it back to the top level, and a rule that tried to copy it there is
refused like any other write to `agentic`.

### The `rw_protected` permission

Writing any protected field requires `rw_protected`, and the check is
**literal** — the `rw_all` admin wildcard does not satisfy it. This is not a
boundary against a hostile administrator: an identity holding `rw_role`
(which `rw_all` grants) can create a role carrying `rw_protected` and assign
it to itself in two API calls, and both of those calls are audited. What the
literal check buys is protection against ACCIDENTAL or tooling-driven writes
by admin credentials, plus a visible trail when someone deliberately escalates
around it. Grant `rw_protected` explicitly, to the identities that run
analysis agents.

The root token issued by the admin unix socket carries `rw_all` and therefore
also cannot write protected fields. Break-glass access means granting a role
`rw_protected`, not reaching for the root token.

Every write and clear emits an audit row (`agentic_set` / `agentic_clear`)
against the record, alongside every other change to it.
