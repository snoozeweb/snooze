---
sidebar_position: 4
---

# Housekeeper configuration

> Package location  
> `/etc/snooze/server-go/housekeeper.yaml` (Go canonical)
>
> Legacy name  
> `/etc/snooze/server/housekeeping.yaml` (still loaded)
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False`

Configuration for the housekeeper goroutine: TTLs and the cadence at which orphan / expired records are reaped. Durations accept both bare seconds (legacy Python form) and Go-style strings such as `"5m"` or `"24h"`.

The Go schema lives in `internal/config/schema/housekeeper.go`.

## Properties

### trigger_on_startup

> Type  
> boolean
>
> Default  
> `True`
>
> Trigger all housekeeping job on startup

### record_ttl

> Type  
> number (time-delta)
>
> Default  
> `172800.0`
>
> Default TTL (in seconds) for alerts incoming

### cleanup_alert

> Type  
> number (time-delta)
>
> Default  
> `300.0`
>
> Time (in seconds) between each run of alert cleaning. Alerts that exceeded their TTL will be deleted

### cleanup_aggregate

> Type  
> number (time-delta)
>
> Default  
> `300.0`
>
> Time (in seconds) between collection drop

### cleanup_comment

> Type  
> number (time-delta)
>
> Default  
> `86400.0`
>
> Time (in seconds) between each run of comment cleaning. Comments which are not bound to any alert will be deleted

### cleanup_orphans

> Type  
> number (time-delta)
>
> Default  
> `86400.0`
>
> Time (in seconds) between each run of orphans cleaning

### cleanup_audit

> Type  
> number (time-delta)
>
> Default  
> `2419200.0`
>
> Cleanup orphans audit logs that are older than the given duration (in seconds). Run daily

### cleanup_snooze

> Type  
> number (time-delta)
>
> Default  
> `259200.0`
>
> Cleanup snooze filters that have been expired for the given duration (in seconds). Run daily

> Alerts silenced by an expired, disabled or deleted snooze filter do not wait
> for this job: the fixed minute-cadence `reconcile_suppression` sweep (not
> configurable) returns them to the alerts list — see
> [A filter that can no longer silence releases its alerts](../general/snooze.md#a-filter-that-can-no-longer-silence-releases-its-alerts).

### cleanup_notification

> Type  
> number (time-delta)
>
> Default  
> `259200.0`
>
> Cleanup notifications that have been expired for the given duration (in seconds). Run daily

### cleanup_notificationlog

> Type  
> string (Go duration)
>
> Default  
> `"720h"` (30 days)
>
> Retention window for notification delivery-history rows (the
> `notificationlog` collection). Rows whose `date_epoch` is older than this
> duration are pruned by the daily cleanup job. Accepts Go duration strings
> (e.g. `"168h"`, `"2160h"`); a zero or unparseable value falls back to the
> 720h default rather than disabling pruning. Editable at runtime in
> **Settings → Housekeeping** without a server restart. Writing the rows at
> all is gated by
> [`delivery_log`](./notifications.md#delivery_log). See
> [Delivery history](../general/notifications.md#delivery-history) for what the
> rows contain and where they surface in the web UI.

### cleanup_stats

> Type  
> string (Go duration)
>
> Default  
> `"9600h"` (400 days)
>
> Retention window for dashboard stat counter buckets. Buckets older than
> this duration are pruned by the housekeeper. Accepts Go duration strings
> (e.g. `"720h"`, `"4320h"`). Editable at runtime in **Settings →
> Housekeeping** without a server restart.

### cleanup_apikey

> Type  
> string (Go duration)
>
> Default  
> `"1h"`
>
> Cadence at which expired API-key rows are purged from the database. Expired
> keys are already refused at authentication time; this job is pure
> garbage-collection of the elapsed rows. Editable at runtime without a server
> restart.

### cleanup_refresh_token

> Type  
> string (Go duration)
>
> Default  
> `"1h"`
>
> Cadence at which expired refresh-token rows are purged from the database.
> The sweep runs across all tenants. Editable at runtime without a server
> restart.

### ack_timeout

> Type  
> string (Go duration)
>
> Default  
> `"24h"`
>
> How long an acknowledgement holds before the alert's ack expires. When an
> operator acks an alert the record is stamped with a server-controlled
> `ack_until = now + ack_timeout`; once that deadline passes, the
> escalate-timeout sweep reverts the record from `ack` back to `open`. Editable
> at runtime in **Settings → Housekeeping** without a server restart.

### escalate_after

> Type  
> string (Go duration)
>
> Default  
> `"0s"` (disabled)
>
> How long an *unacknowledged* open alert may sit before auto-escalation kicks
> in. With a non-zero value, an alert left `open` past `escalate_after` is
> flipped to `esc` and its notifications are re-fired. `0` (the default)
> disables auto-escalation entirely — the escalate pass becomes a no-op.
> Editable at runtime without a server restart.

### shelve_timeout

> Type  
> string (Go duration)
>
> Default  
> `"4h"`
>
> The default length of a *timed* shelve — used when the shelve comment does not
> name its own. Posting a `shelve` comment transitions the record to `shelved`
> and stamps `shelve_until = now + duration` when the comment carries a positive
> `duration` (seconds — this is what the web console's shelve dialog sends, so
> the window the operator picked is the one that is served), falling back to
> `shelve_until = now + shelve_timeout` when it does not. Either way the deadline
> is server-controlled; once it passes, the minute-cadence `unshelve_timeout`
> sweep reverts the record to `open` (clearing `shelve_until`) and writes an auto
> comment. This is distinct from the
> legacy *permanent* shelve (`ttl=-1`, `shelve_until=0`), which has no duration
> and is never auto-returned. Editable at runtime in **Settings → Housekeeping**
> without a server restart. A timed shelve leaves `ttl` untouched, so if `ttl` is
> shorter than `shelve_timeout` the `cleanup_alert` job may delete the alert
> before the shelve expires — keep `shelve_timeout` below `record_ttl` if you need
> the alert to survive its shelve.

### resolution_hold

> Type  
> string (Go duration)
>
> Default  
> `"2h"`
>
> How long, after a person closes an alert whose analysis verdict is `resolved`
> or `self_resolved`, a re-fire of the same aggregate keeps it closed — no
> re-open, no owner clear, no notification — instead of re-opening it. Covers a
> source that keeps firing for a while after the fix (a rule over a look-back
> window). The source reporting recovery ends the hold early; a re-fire after
> the window, or at a higher severity, re-opens as usual. `0` disables the hold.
> The window is stamped on the record (`resolution_hold_until`) at close time,
> so a change applies to the next close. Editable at runtime in **Settings →
> Housekeeping**. See [Resolution hold](../general/alerts.md#resolution-hold).

## Ack expiry & auto-escalation

A minute-cadence housekeeper sweep (`escalate_timeout`) enforces the
server-controlled alert lifecycle, per tenant:

- **Expired acks revert to open.** When an operator acknowledges an alert, the
  record is stamped with `ack_until = now + ack_timeout`. Once that deadline
  passes the sweep reverts the record to `open` (clearing `ack_until`) and
  writes an automatic *"Ack expired — reverted to open"* timeline comment.
  This closes the gap where a one-shot acked alert that never recurred stayed
  silenced forever.
- **Overdue opens auto-escalate.** When `escalate_after` is set, an alert that
  is left `open` past its `escalate_at` deadline is flipped to `esc`, an
  automatic *"Auto-escalated: unacknowledged past deadline"* comment is written,
  and its notifications are re-fired. Escalation is **one-shot**: the deadline
  is cleared on the flip, so the same record is not re-escalated until a new
  transition re-arms it.

The notification dispatcher's own `frequency` throttle still applies to the
re-fired notification, so a misconfigured short `escalate_after` cannot spam.
Note that the TTL `cleanup_alert` job may delete a record before it escalates if
its `ttl` is shorter than `escalate_after` — keep `escalate_after` below
`record_ttl` for escalation to be effective.
