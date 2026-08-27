---
sidebar_position: 8.5
---

# Re-escalation

An alert can fire more than once in its lifetime. Snooze distinguishes the
**first delivery** of an alert from a **re-escalation** of one already in
flight, and every output plugin behaves differently for the two.

The rule the whole design enforces: **one alert, one object**. An operator with
one problem should end up with one JIRA ticket, one ServiceNow incident, one
Statuspage incident, one chat thread — with new information added to it — not a
queue of near-identical duplicates competing for their attention.

## What counts as a re-escalation

Three producers escalate an alert, and all three stamp the same context onto
the record:

| Producer | Trigger | `escalation_reason` |
|---|---|---|
| Escalate-timeout sweep | An open alert passed `escalate_at` without being acknowledged (see [Housekeeping](./housekeeping.md)) | `timeout` |
| Operator | An `esc` comment from the web UI or a chat command (`/esc` in Teams, the buttons in Slack/Telegram) | `manual` |
| Aggregate rule watchlist | A watched field changed on an acknowledged alert (see [Aggregate rules](./aggregaterules.md)) | `watchlist` |

The stamped fields are:

| Field | Meaning |
|---|---|
| `escalation_count` | `0` on a first delivery, `1` on the first re-escalation, and so on |
| `escalated_at` | Epoch seconds of the current escalation |
| `escalation_reason` | `timeout`, `manual`, or `watchlist` |
| `escalation_actor` | The operator's login, on a manual escalation only |

Closing an alert **resets** the count, so the alert's next occurrence is a fresh
incident rather than a comment on a ticket that was just resolved.

An operator escalating from the UI or a chat command re-fires the notification
dispatcher. No other transition does, so acknowledging, closing or shelving an
alert produces no extra notifications.

### Targeting only re-escalations

`escalation_count` is available to notification conditions, so a rule can fire
only for escalations:

```
escalation_count > 0
```

This is useful for a second, louder action — an SMS or a phone call — layered on
top of a normal chat notification.

## Behaviour per output plugin

Plugins fall into three classes.

### Stateful targets — update, never duplicate

These record the object they created against the alert and update it on every
escalation.

| Plugin | On re-escalation | Correlated by |
|---|---|---|
| [JIRA](./integrations/jira.md) | Comments on the existing issue; raises its priority if severity rose; transitions it back out of a Done status. Configurable via **On re-escalation**: `reopen` (default), `comment`, `new` (a second, linked issue), `skip`. | The issue key, stored on the alert |
| [ServiceNow](./integrations/servicenow.md) | PATCHes the existing incident with work notes; raises urgency/impact if severity rose; returns a resolved incident to In Progress | `correlation_id` (the alert hash) |
| [Statuspage](./integrations/statuspage.md) | Posts an incident update on the existing incident, advancing `investigating` → `identified` at most once. Never drags an incident an operator moved to `monitoring` back. | The rendered incident name |
| [Opsgenie](./integrations/opsgenie.md) | Adds a note, raises the priority if severity rose, and **un-acknowledges** the alert so the on-call is notified again (disable with **Un-acknowledge on re-escalation**) | The alias (the alert hash) |
| [PagerDuty](./integrations/pagerduty.md) | Re-triggers on the same dedup key, so PagerDuty appends to the existing incident; the escalation count and reason travel in `custom_details` | The dedup key (the alert hash) |

:::note
The Opsgenie alias and the PagerDuty dedup key are derived from the alert
**alone** and never from the escalation. That is deliberate: folding anything
escalation-specific into either would make the provider treat each escalation
as a brand-new alert, turning one incident into a queue of identical ones.
:::

A stale handle — the ticket was deleted, the incident resolved and archived —
falls back to creating a new object, so an alert can never be black-holed.

### Chat targets — reply in the thread

| Plugin | On re-escalation |
|---|---|
| [Google Chat](./integrations/googlechat.md) | A short text reply in the alert's thread. Threading is on by default (`thread_key` defaults to the alert hash); clear it for one conversation per message |
| [Slack](./integrations/slack.md) | A threaded reply under the first message, with `reply_broadcast` so the channel sees it too. **Bot-token mode only** — see the limitation below |
| [Telegram](./integrations/telegram.md) | A reply to the original message |
| [Teams](./integrations/teams.md) | A threaded reply carrying the escalation number and reason — **`snooze-teams` daemon only**; see the limitation below |

### Stateless targets — escalate the urgency

There is no conversation and no handle to reuse, so an escalation is expressed
by being harder to ignore.

| Plugin | On re-escalation |
|---|---|
| [Mail](./integrations/mail.md) | Threaded into the original conversation via `In-Reply-To`/`References` (the `Message-ID` is derived deterministically from the alert), with `Importance: high` and an `[ESCALATED #N]` subject |
| [ntfy](./integrations/ntfy.md) | Priority raised one step (capped at 5) plus a siren tag |
| [Pushover](./integrations/pushover.md) | Priority raised toward 2 (emergency), which retries until the recipient acknowledges |
| [Twilio](./integrations/twilio.md) | Short form (`ESC #3 host: message`) since SMS is billed per segment. Two opt-in knobs: **Escalation throttle** to stop a flapping alert ringing a phone every minute, and **Switch to a voice call at escalation** |
| [SNS](./integrations/sns.md) | The escalation context as message attributes, so a filter policy or subscriber can route on it |
| [Patlite](./integrations/patlite.md) | The light state is raised (steady → `blink1` → `blink2`). The colour is left alone: that reports severity |
| [Script](./integrations/script.md) | `SNOOZE_ESCALATION_COUNT`, `SNOOZE_ESCALATION_REASON`, `SNOOZE_ESCALATION_ACTOR`, `SNOOZE_PREVIOUS_SEVERITY` and `SNOOZE_STATE` in the environment — the script decides |
| [Webhook](./integrations/webhook.md) | `.Escalation` and `.NotifyRef` in the body template; the default (template-less) body carries the escalation fields |
| [Snoozepeer](./integrations/snoozepeer.md) | Forwards the escalation context, so the peer's own pipeline and notifiers escalate natively |

## Known limitations

These are limitations of the transport, not of Snooze, and each is worth knowing
before you pick a mode:

- **Slack in webhook mode cannot thread.** An incoming webhook returns no
  message identity, so there is nothing to reply under. The escalation arrives
  as a normal message carrying a `New escalation #N` banner. Use a bot token if
  you want real threads.
- **Teams and Mattermost incoming webhooks cannot thread**, for the same
  reason — a Mattermost incoming webhook returns no post id. Both carry the
  banner instead. Real Teams threading requires the optional `snooze-teams`
  daemon, which posts through the Graph API.
- **Discord webhooks cannot open a thread** on their own message (that needs a
  bot token), so Discord carries the banner.
- **Statuspage has no external-reference concept**, so its correlation is by
  rendered incident name. Two alerts that render to the same name will be
  treated as one incident.
- **Patlite has no per-alert state at all** — a tower light is either showing
  something or it is not. It can only get more insistent.

## Where the handle is stored

A notifier that creates an external object stores its identifier on the alert as
`notify_ref_<action name>`, scoped per action so two actions targeting the same
notifier keep independent objects. Aggregate rules carry the field forward onto
every subsequent occurrence of the alert, which is what lets the notifier find
its object again.

The older `response_<action name>` field, written by the webhook plugin's
`inject_response`, is still read, so a deployment that threads Teams through the
webhook bridge keeps working with no migration.
