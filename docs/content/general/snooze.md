---
sidebar_position: 7
---

# Snooze filters

![Architecture - Snooze filters plugin](./images/architecture.png)

## Overview

Stop alerts from being notified.

Alerts have to match the Snooze filter's condition and time constraint in order to being processed.

Snooze filters are especially useful to reduce noise in case an alert does not need to be notified.

Several reasons could justify creating a Snooze filter. Maybe the alert was not a critical issue after all or the escalating time itself was not considered critical.

``` yaml
host: dev-syslog01.example.com
rules: ['is_development']
environment: development
timestamp: 2020-07-15 04:00:00 # Monday
```

``` yaml
name: snooze_dev
condition: environment = development
time_constraint:
    datetime:
      - from:  2021-07-01 00:00:00
        until: 2021-07-31 23:59:59
    time:
      - from:  00:00:00
        until: 00:08:00
    weekdays:
      - weekdays: [1,4] # Monday, Thursday
```

``` yaml
host: dev-syslog01.example.com
rules: ['is_development']
environment: development
timestamp: 2020-07-15 04:00:00
snoozed: snooze_dev
```

The alert matched the Snooze filter, therefore it got stopped before being executed by the next Process plugin.

Any alert matching a Snooze filter will have a new field `snoozed` added with the Snooze filter name.

## Web interface

![](./images/web_snooze.png)

Name\*  
Name of the snooze filter.

[Condition](./conditions.md)  
This rule will be triggered only if this condition is matched. Leave it blank to always match.

[Time Constraint](./timeconstraints.md)  
Time constraint during this snooze filter will be active. See [Time Constraints](./timeconstraints.md)

Discard  
Discard alerts matching this snooze filter.

Comment  
Description.

It is possible to see how many times a alert was snoozed by checking the number on the very right. Whenever clicking on it, a list of alerts that have been snoozed by the corresponding filter will be displayed on the **Alerts** page under the **Snoozed** tab.

### Status and remaining countdown

Each row in the snooze list carries a server-computed **Status** badge and a
**Remaining** countdown, both derived at read time from the filter's
`time_constraints.datetime` window (no extra request, no stored field):

- **Status** — the lifecycle of the filter's absolute datetime window:
  - **active** (green) — the window covers the current moment; the filter is
    suppressing matching alerts right now.
  - **pending** (amber) — the window starts in the future; the filter is
    scheduled but not yet suppressing.
  - **expired** (grey) — the window has already closed; the filter no longer
    matches.
  - **always on** (blue) — no datetime constraint is set, so the filter fires
    indefinitely (weekday/time-of-day constraints do not bound the lifespan).
- **Remaining** — for an active, time-bounded filter, the time left until its
  window closes (e.g. `2h 15m`); `forever` for an always-on filter; `—` when
  there is no countdown (pending, expired, or open-ended).

The Active / Upcoming / Expired tabs use the same status: *always-on* folds
into **Active**, and *pending* maps to **Upcoming**.

### Silence for… shortcut

The editor's **Silence for…** row sets an absolute datetime window from now
without opening the date-picker — the common "quiet this host until morning"
case. Click a preset (**1h**, **4h**, **24h**, **7d**) or type a free-text
duration and press **Apply**. The free-text field accepts a sequence of
`<number><unit>` segments where unit is `d` (days), `h` (hours), or `m`
(minutes) — for example `2h`, `30m`, or `1d12h`. Invalid input is rejected
with a message and leaves the form untouched. Applying a duration replaces the
filter's single absolute datetime range with `from = now`, `until = now +
duration`; recurring windows (weekdays, daily time-of-day) still use the full
time-constraints editor below.

