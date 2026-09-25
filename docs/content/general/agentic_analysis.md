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
      "detail": "The token-sync container peaked at 67Mi during the hourly key rotation; the limit has been 64Mi since the chart was created.",
      "scope": "ovh/monitoring/jwt-key-sync",
      "evidence": [
        "kubectl describe pod: Last State Terminated, Reason: OOMKilled",
        "container_memory_max_usage_bytes peaked at 67Mi"
      ],
      "caveats": ["Only the last 6h of container metrics were retained"],
      "confidence": "high"
    },
    "remediation_plan": {
      "status": "action_required",
      "steps": [
        {
          "action": "Raise the memory limit to 128Mi",
          "command": "kubectl -n monitoring set resources deploy/jwt-key-sync --limits=memory=128Mi",
          "risk": "low",
          "when": "now"
        },
        {
          "action": "Persist the new limit in the chart values",
          "risk": "low",
          "when": "follow_up"
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
| `root_cause.summary` | yes | **One sentence** — the headline a triager reads (aim for ≤ 160 characters). Hard limit 500. |
| `root_cause.detail` | no | The explanation behind the summary: chain of events, timings, ruled-out causes. ≤ 2000 characters. |
| `root_cause.scope` | no | What is broken, addressed however fits the alert. ≤ 200 characters. |
| `root_cause.evidence` | no | Up to 10 short observations, ≤ 500 characters each. |
| `root_cause.caveats` | no | Up to 5 limits of the investigation (what could not be checked, what is inferred), ≤ 300 characters each. |
| `root_cause.confidence` | yes | `high` \| `medium` \| `low`. |
| `remediation_plan.status` | no | The verdict: `action_required` \| `self_resolved` \| `monitoring` \| `resolved`. `self_resolved` means the alert recovered on its own; `resolved` means somebody (a person or an agent) applied a fix. |
| `remediation_plan.steps` | yes | 1–20 steps, each `{action, command?, risk, when?}` (`action` ≤ 500, `command` ≤ 1000, `when` = `now` \| `follow_up`). |
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
snooze record agentic status <uid> resolved       # change only the verdict
snooze record agentic clear <uid>
```

`get` prints the analysis for a human — provenance, the verdict chip and
confidence, scope, summary, detail, caveats before evidence, then the plan as
numbered **Now** / **Follow-up** / **Rollback** steps with each step's risk and
its command on a line of its own. `--json` prints the raw subtree. In
`snooze record show <uid>` the analysis is one digest line (verdict ·
confidence · summary) pointing at `agentic get`.

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

Both surfaces read an analysis in the same order, the order on-call needs it:
**is there anything to do** (the plan's `status`), **what happened in one line**
(the headline), **how far to trust it** (confidence, caveats, freshness), and
only then **why** (detail and evidence).

- **Headline.** `summary` is set as the headline and `detail` as the paragraph
  under it. An older analysis with no `detail` and a long summary is split at
  its first sentence (or clipped at a word, with the rest continuing below),
  so the headline never becomes a paragraph set in display type.
- **Verdict.** `status` renders as a chip before the headline: *Action
  required*, *Monitoring*, *Self-resolved* or *Resolved*, each with an icon.
  *Resolved* takes the quieter closed-state colour rather than
  *Self-resolved*'s green, so "somebody fixed it" and "it recovered on its
  own" stay apart at a glance. It is usually written by whoever applied the
  fix, after acting on an *Action required* plan — the snooze skill, or by
  hand with `snooze record agentic status <uid> resolved`. Either way the
  alert still needs closing once it stays green.
- **Confidence** is a neutral three-segment meter plus the word (`▮▮▮ High
  confidence`). It deliberately takes no severity colour, so it cannot read as
  a second severity beside the alert's own.
- **Author.** Anything not saved from the web editor is labelled **AI
  analysis**, followed by the tool tag (`alert-rca`, `snooze-cli`) and the
  subject. One a person saved reads *Written by &lt;user&gt;*.
- **Risk** is marked only above `low`: a *Medium risk* or *High risk* chip. A
  plan where every step says "Low risk" buries the one step that isn't.
- **Now / Follow-up.** When steps carry `when`, the plan splits into *Now*
  (on-call work while the alert is live) and *Follow-up* (post-incident
  work). Each step keeps its original number.
- **Caveats** are kept apart from the evidence and read before it. An older
  agent's evidence line starting with `caveat:` is treated as a caveat.

### The alert inspector

The **Analysis** tab sits between Flow and Deliveries. While another tab is
open, the inspector header carries one line pointing at the analysis
(`[verdict] AI analysis · <headline> ›`). Clicking it opens the tab, and the
line steps aside while the tab is open.

The tab shows, in this order:

1. The provenance line with the confidence meter, plus **Edit** and a **⋯**
   menu holding **Remove**.
2. A notice when the alert has fired again since the analysis was written.
3. The verdict, the headline, the detail and the scope.
4. **What to do**, marked *Automatable* or *Manual*.
5. **Caveats**.
6. **Evidence · N**, folded by default, one numbered line per item. When
   every line was read with the same probe (`kubectl --context ovh: …`), the
   probe is printed once above the list.

Anyone who can read the alert can read its analysis. **Edit**, **Remove**
and, on an unanalysed alert, **Write analysis** appear only for an identity
holding `rw_protected`. The check is literal, so an `rw_all` admin sees the
read-only view, the same answer the endpoint would give.

A save is a replace, not a merge. It overwrites the whole subtree and
restamps provenance with the signed-in user and `source: snooze-web`,
including when it corrects an analysis an agent wrote. The editor covers every
field above (detail, caveats, the plan status, each step's *When*). It
validates locally against the rules above before it sends, and places the
server's field-keyed 422 on the same fields.

In the alerts table, an analysed alert carries a small dot beside its severity
badge. The dot is coloured by the verdict, not by confidence, and titled
`Analysed · high confidence · Monitoring`.

### The dashboard's Analyses view

The dashboard has two views, switched from the segmented control next to its
title: **Overview** and **Analyses**. The switch shows the analysed count. The
"Right now" tile strip also counts analysed alerts, on an **Analysed** tile
("17 of 42 open") that opens the view when clicked.

The view and the list's order ride in the URL, so
`/web/dashboard?view=analyses&sort=recent` is a deep link and Back undoes a
view switch or a re-sort.
Both surfaces read the record collection, so they are offered only to an
identity holding `ro_record` (or `rw_record`). A stats-only role gets the
Overview, with no switch and no tile.

The view lists **every** analysed open alert as a work queue. A safety ceiling
of 500 rows applies, and the view says so when it bites. Rows sort **most
urgent first**: severity, then unacknowledged before acknowledged, then
fired time. *Newest analysis* is the alternative sort (`?sort=recent`).

Filters narrow the list in place:

- **Confidence:** *Any*, *Medium+*, *High*.
- **Automatable:** *Any*, *Yes*, *No*.
- **Verdict:** shown once some analysis states one.

Each row names the alert: severity, host and message as a heading that links
to it, with its state and when it fired on the right. Collapsed, the row shows:

- the verdict and the headline;
- the author, when the analysis was written, the confidence meter and the
  caveat count;
- a plan summary: "Now: …" and "Follow-ups: N · M medium/high risk", or
  "Plan: N steps" when the steps are not split.

**Expand** reveals the full explanation, the caveats and every step, with its
command and a copy button.

The keyboard works the list: J/K (or the arrow keys) move between rows,
Enter opens the alert, and Space or E expands. **Open alert** lands on the
alert with its inspector on the Analysis tab, filtered to the analysed alerts,
so the inspector's previous/next walks the queue. With more analysed alerts
than one alerts page holds, it pins the table to that one alert instead.

The inspector's tab is in the URL too, as `pane` (`flow`, `analysis`,
`deliveries` or `record`; Timeline when absent), so
`/web/alerts?record=<uid>&pane=analysis` opens an alert on its analysis and
Back steps through the tabs you visited. The older `analysis=1` link still
works. Back, a sidebar link or closing the inspector with an unsaved analysis
draft asks before it discards the draft.

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
