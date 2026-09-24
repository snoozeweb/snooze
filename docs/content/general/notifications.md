---
sidebar_position: 8
---

# Notifications

![Architecture - Notifications plugin](./images/architecture.png)

:::info

For the catalogue of notification destinations — Slack, Microsoft Teams, email, Google Chat, Mattermost, PagerDuty, Opsgenie, ServiceNow, Twilio, webhooks and more — see [Integrations](./integrations/index.md).

:::

Call a list of [Actions](./actions.md) which are alerting scripts.

Alerts have to match the Notification's condition and time constraint in order to being processed.

Notification is the only component relying on another one. Indeed, at least one Action has to be created first before being able to use it.

``` yaml
host: prod-syslog01.example.com
rules: ['is_production']
time_constraint: {}
environment: production
```

``` yaml
name: alert_production
condition: environment = production
time_constraint: {}
actions: ['sendmail_all'] # Assumes this action already exists
```

``` yaml
host: prod-syslog01.example.com
rules: ['is_production']
environment: production
notifications: ['alert_production']
```

The Alert matched the notification's condition and time constraint (it was empty), therefore the action `sendmail_all` will be called.

Any Alert matching a notification will have a new field `notifications` added with the list of matched notifications.

## Web interface

![](./images/web_notifications.png)

Name\*  
Name of the notification.

[Condition](./conditions.md)  
This rule will be triggered only if this condition is matched. Leave it blank to always match.

[Time Constraint](./timeconstraints.md)  
Time constraint during this notification will be active.

[Actions](./actions.md)  
List of actions to execute. At least one action needs to be created beforehand.

[Frequency](#frequency)  
Keep sending notifications. If acknowledged or closed, no more notification will be sent.

Comment  
Description.

### Frequency

This parameter controls how many times this notification should be triggered and at which interval. It has 3 subparameters (in order):

delay (`0`)  
Time in seconds to wait before triggering this notification the first time.

every (`0`)  
Time in seconds until the next notification trigger.

total (`1`)  
Number of total notifications sent (-1 means indefinitely)

By default a notification is immediately executed only once.

It an alert that triggered this notification gets [acknowledged](./alerts.md#acknowledge) or [closed](./alerts.md#close) before the notification is sent, no more will be sent.

``` yaml
delay (10) every (60) total (4)
# Sends a notification after 10s
# then keep sending a notification every 60s
# until 4 notifications have been sent in total

delay (0) every (10) total (-1)
# Sends a notification immediately
# then keep sending a notification every 10s
# indefinitely (will stop if someone acknowledges the alert)
```

## Alert flow: matched notifications and action outcomes

Each processed alert records the path it took through the pipeline:

- `notifications` — the names of the notification entries whose condition and
  time window matched the alert.
- `actions` — one entry per action a matched notification referenced, each with
  a `status`:
  - `success` — the notifier sent it.
  - `error` — the send failed, or the action is misconfigured (the `error`
    field carries the reason).
  - `skipped` — the notification's frequency is disabled (`total: 0`).
  - `pending` — the send is in flight (resolves to `success`/`error`).
  - `sent` — dispatched while outcome tracking is disabled (see the
    `persist_action_outcomes` flag in [Notification configuration](../configuration/notifications.md#persist_action_outcomes)).

Open an alert's detail drawer in the **Alerts** view (click the row, or pick
**View details** from the row-actions menu) and open the **Flow** tab. It draws the path the
alert's **last occurrence** took as a line of stages: input → rules →
aggregate → snooze → notifications. Each matched notification shows the actions
it fired, with their outcome in colour; hover a failed action for the error.

The chart shows where the run stopped, using the record's `plugins` trail:

- **Silenced**: the **Snooze** stage is marked as the stop and names the snooze
  that matched.
- **Held as a repeat**: the **Aggregate** stage is marked as the stop. The
  occurrence came in inside the aggregate's throttle window, or repeated an
  already-closed alert, so it was counted but not notified.

Stages after the stop are greyed out on a dashed line. When an earlier run did
notify, what it sent is still listed under **Last notified via**, so you can see
who has already been told.

## Delivery history

Every time an action actually sends — not just matches — Snooze writes one
**delivery** row: a permanent record of that send, kept independently of the
alert it was for. A delivery row is:

- **One row per actual send attempt by one action.** An unbatched action
  writes one row per (alert × action). A batching action (`batch: true` on
  mail, webhook or script) writes exactly **one row per flush**, listing every
  alert that was in the batch — not one row per alert.
- Stored regardless of whether the send worked, with two exceptions: a
  notification whose [frequency](#frequency) is disabled (`total: 0`) never
  reaches an action, so it writes nothing at all (there is nothing to log); a
  **misconfigured action** (the action doesn't exist, has no notifier
  configured, or points at a notifier plugin that isn't registered) writes a
  failed row so an operator opening the history sees "this never sends"
  instead of silence — rate-limited to one row per (notification, action)
  pair every 10 minutes, so a broken action sitting on a noisy condition
  doesn't write a row per ingested alert forever.

Each row carries: the send's completion time and duration; `success` or
`error` (with the error text on failure); the action name and the notifier
plugin it used (`mail`, `webhook`, `script`, `slack`, …); whether it was a
batch flush and why the bucket flushed (`size`, `timer`, or `shutdown` — the
server drained pending batches on a graceful stop); the notification(s) that
routed the send; a **snapshot** of every alert covered — host, severity,
message (truncated to 512 characters), state — captured at send time, so the
row still renders correctly after the alert record itself has expired; the
re-escalation context (count and reason), when the delivery was a
re-escalation and not a first send; and, when the notifier produced one, an
external reference (e.g. a JIRA issue key) with a link.

The alert snapshot is **de-duplicated**: when the same alert reaches one
batching action twice (two notifications routing it into the same bucket),
`alerts[]` keeps one entry per alert (keyed on uid, or hash when the uid
hasn't been resolved yet) and `alert_count` is that deduped count, not the
number of times the alert was queued. An `alerts[]` entry omits `uid`
entirely when the alert's own record hadn't been assigned one yet at send
time — the `hash` is always present and is what the UI falls back to.

### Where to see it

- **Notifications** page → open a row (or pick **View details** from the
  row-actions menu) → the **Deliveries** tab, shown first because "did this
  actually send?" is the question that brings an operator to a notification's
  detail. The same tab, same default, appears on an **action**'s details.
- The notifications table has **Sent** (a count) and **Last sent** columns,
  stamped only by a *successful* delivery.
- An alert's detail drawer (**Alerts** → click a row) has a **Deliveries** tab
  and, in the summary header, a **Notified** entry — "8m ago via mail-oncall",
  or in red "Failed 8m ago via …" when the most recent send for that alert
  errored.
- The dashboard's **Notifications** panel ranks notifications by send count
  for the current window and links each one into its Deliveries tab,
  pre-filtered to that same time window (shown as a dismissable **Window**
  chip).

### Filtering and deep links

The Deliveries tab has three chips: **All**, **Failed**, **Batched** — client-
side narrowing of the same scope; the row count above the chips always
describes the whole scope, not the narrowed list.

A batched row shows every member alert (worst severity first) with a
**"View all N alerts"** link that opens the **Alerts** table with a query the
operator can see and edit, e.g. `uid IN ["a1…", "a2…"]` in the search box
(falling back to `hash IN […]` past 200 uids, or when a uid never resolved).
Because **alert records expire before delivery rows do** (2 days by default —
[`record_ttl`](../configuration/housekeeping.md#record_ttl) — versus 30 days
of delivery retention), an old delivery row keeps its alert snapshot forever,
but its "View all" link can land on an empty alerts list once the alerts
themselves have been cleaned up.

### Repeats are folded on an alert

An alert that stays open and re-notifies — every quarter of an hour for two
weeks, say — leaves thousands of identical rows behind. On an alert's
Deliveries tab they are folded: consecutive, all-successful dispatches of one
notification to the same actions become **one row**, showing the newest
dispatch plus `×1,420 · every ~16 min · since Sep 17th 10:46`. **Show all**
lists every dispatch of that run, oldest ones on demand. A dispatch with a
failed send is never folded, and it splits the run around it, so a failure
always stands out. Neither is a dispatch that produced a **new ticket
reference** — the Jira send that opened `AD-775`, or the first one quoting an
issue key — so the link to the ticket is always on its own row; later sends
repeating the same reference fold as usual. (A chat thread id is not a ticket
reference and never splits a run.) Each notification is folded on its own, so two
notifications routing the same alert do not break each other's runs.

The line above the chips sums up the alert's whole history: the delivery
count, the failures, how often it has been notifying, when it started and
when it last sent. **Failed** and **Batched** list raw rows, not runs.

The alert's **Timeline** tab folds the same way: a run of identical automatic
entries (the "New escalation" written on each re-notification) is one entry
with its count, cadence and start, and **Show all** lists their times.
Anything a person wrote is never folded, and it splits the run around it.

Folding reads at most 10,000 of the alert's rows, newest first. Past that the
oldest run reads "since at least …".

### REST

`GET /api/v1/notificationlog` supports the same list/search/`q=` surface as
every other collection, including `POST /api/v1/notificationlog/search`
(the DSL-in-body form), which is treated as a read for authorization
purposes. Requires `ro_notificationlog`: the built-in admin role has it, and
the seeded **notifications** role includes it too (an idempotent boot-time
backfill grants it to any pre-existing `notifications` role that predates
the delivery log). Custom roles need `ro_notificationlog` added explicitly.

`GET /api/v1/notificationlog/runs?alert_uid=<uid>&limit&offset` returns one
alert's log folded into runs (see above), newest first, with a `meta` block
summing up the whole log: `sends`, `dispatches`, `first_epoch`, `last_epoch`,
`interval_s` (the median gap between dispatches) and `truncated`. Each run
carries its `dispatches`, `sends`, `first_epoch`, `last_epoch`, `interval_s` and
the rows of its newest dispatch (`latest`). `GET /api/v1/comment/runs?record_uid=<uid>`
does the same for the alert's timeline. Both are gated like the list they fold.

Rows are written exclusively by the notification dispatcher — `POST`, `PUT`
and `PATCH` on `/api/v1/notificationlog` are refused with `403` regardless of
permissions, since a writable history would let anyone holding
`rw_notificationlog` fabricate a delivery that never happened. `DELETE` is
still allowed (with `rw_notificationlog`): deleting history ahead of the
retention sweep is destructive but not deceptive.

```bash
curl -G "https://<snooze>/api/v1/notificationlog" \
  -H 'Authorization: Bearer <token>' \
  --data-urlencode "q=<base64url condition>" \
  --data-urlencode "orderby=date_epoch" \
  --data-urlencode "asc=false"
```

Useful conditions: `notification_uids CONTAINS "<uid>"` (every delivery a
given notification produced) and `alert_uids CONTAINS "<uid>"` (every
delivery a given alert was part of) — `CONTAINS` on an array field is a
regex match against each element server-side, and uids are regex-safe so
this is exactly the intended filter. `batch = true` needs a boolean literal,
not the string `"true"`. The common queries (`date_epoch`, `status`,
`action`, `notifier`) are backed by an index on every backend — expression
indexes on SQLite and Postgres, per-field indexes on Mongo. A row looks
like:

```jsonc
{
  "uid": "1f2e…",
  "date_epoch": 1757340000,
  "queued_epoch": 1757339998,
  "duration_ms": 412,
  "status": "success",
  "action": "mail-oncall",
  "notifier": "mail",
  "batch": false,
  "notification_uids": ["7f9c…"],
  "notification_names": ["page-oncall"],
  "alert_count": 1,
  "alert_uids": ["a1b2…"],
  "alert_hashes": ["h1…"],
  "alerts": [
    {
      "uid": "a1b2…",
      "hash": "h1…",
      "host": "db-01",
      "severity": "critical",
      "message": "disk 98%",
      "state": "open",
      "notification": "page-oncall"
    }
  ],
  "escalation_count": 0
}
```

### Testing an action

**Send test** delivers immediately, even on a batching action (`batch: true`
on mail, webhook or script): the test payload bypasses the batch bucket
entirely rather than joining it, so a synthetic alert never rides out with
real alerts in their delivery-history row. A test send is a real probe of the
transport — a 200 or the notifier's own error comes back synchronously — but
it is otherwise invisible: it writes no delivery row, bumps no `hits`/
`last_sent` counters, and stamps nothing onto any record.

### Configuration

Writing rows is gated by
[`notification.delivery_log`](../configuration/notifications.md#delivery_log)
(default on); turning it off does not affect the `hits`/`last_sent` counters
on the notification, only the history rows. Retention is
[`housekeeping.cleanup_notificationlog`](../configuration/housekeeping.md#cleanup_notificationlog)
(default 30 days).

## Federating alerts to a Snooze peer

To relay accepted alerts to a downstream Snooze (or generic HTTP) peer — for
hub-and-spoke or active/active topologies — configure a **notification** whose
action is a **Forward to another Snooze peer** (`snoozepeer`) target. See the
[Snooze peer / federation](./integrations/snoozepeer.md) integration page for the
full action-field reference.

1. Create an **action** of type *Forward to another Snooze peer* with the peer's
   endpoint (e.g. `https://hub.example.com/api/v1/alerts`) and optional
   per-peer auth (bearer / basic / apikey), TLS-verification, and timeout.
2. Create a **notification** whose **condition** scopes which accepted alerts
   relay (leave empty to relay everything) and whose **actions** list includes
   the snoozepeer action.

Relay sends the normalized post-pipeline record to the peer's
`POST /api/v1/alerts`, which re-runs its own pipeline on it. Relay is
**fire-and-forget**: a slow or failing peer never blocks or fails local
ingestion, and there are no retries (to avoid amplification storms).

### Loop prevention

In a cyclic topology (A → B → A, or a mesh of hubs) Snooze prevents infinite
relaying with the `X-Snooze-Loop` header — a comma-separated chain of the server
ids an alert has already passed through. A server that finds its own id in the
inbound chain accepts the alert but does not re-relay it. A server's id is its
`syncer.hostname`.

### HA prerequisite: distinct `syncer.hostname`

Loop detection keys on `syncer.hostname`. **Each federated node must set a
distinct `syncer.hostname`** — if two nodes share one, loop detection breaks.
Set it explicitly per node (the OS-hostname default is fragile in
container/Kubernetes deployments):

``` yaml
# syncer.yaml on node A
hostname: hub-eu
```

### Upgrading from the standalone Federation page

Earlier releases had a dedicated **Federation** admin page backed by a `forward`
collection. Federation is now a notification action. Deployments that had
`forward` destinations convert them once by running
`snooze-server migrate forward-to-action` (idempotent; safe to run on a database
with no forward destinations — it does nothing).

## Re-escalation

An alert can fire more than once in its lifetime, and every output plugin
handles a re-escalation differently from a first delivery — updating the ticket
it already opened, replying in the chat thread it already started, or simply
getting louder. See [Re-escalation](./escalation.md) for the per-plugin
behaviour, the fields the escalation stamps on a record, and how to write a
notification rule that fires only for escalations.
