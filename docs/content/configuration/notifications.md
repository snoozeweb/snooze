---
sidebar_position: 5
---

# Notification configuration

> Package location  
> `/etc/snooze/server-go/notification.yaml` (Go canonical)
>
> Legacy name  
> `/etc/snooze/server/notifications.yaml` (still loaded)
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False` (bootstrap defaults)
>
> Runtime store  
> `settings` plugin

Default notification frequency / retry. The YAML seeds the defaults at startup; runtime overrides live in the `settings` collection.

The Go schema lives in `internal/config/schema/notification.go`.

## Properties

### notification_freq

> Type  
> number (time-delta)
>
> Default  
> `60.0`
>
> Time (in seconds) to wait before sending the next notification

### notification_retry

> Type  
> integer
>
> Default  
> `3`
>
> Number of times to retry sending a failed notification

### persist_action_outcomes

> Type  
> boolean
>
> Default  
> `true`
>
> When enabled, the server resolves each fired action's outcome (`pending` →
> `success`/`error`) and writes it back onto the alert record — one extra
> merge-write per notifying alert. Disable it on single-writer SQLite under high
> alert volume: the matched notifications and the action list are still recorded,
> but fired actions stay at status `sent` instead of resolving to
> `success`/`error`. See the alert Flow tab in
> [Notifications](../general/notifications.md#alert-flow-matched-notifications-and-action-outcomes).

### delivery_log

> Type  
> boolean
>
> Default  
> `true`
>
> Records one row per send attempt in the tenant-scoped `notificationlog`
> collection — the delivery history. Each row carries the send time, the action
> and notifier used, `success`/`error` (with the error text), the duration, the
> notification(s) it fired for and a snapshot of the alert(s) it covered
> (host / severity / message / state), so a delivery still renders after the
> alert record itself has expired. A batching action (webhook / mail / script
> with `batch: true`) writes exactly one row per flush listing every member.
> This is what backs the **Deliveries** tab on a notification, an action and an
> alert. Disable it on write-constrained deployments (single-writer SQLite at
> high alert volume): the per-action outcome stamps on the record and the
> notification `hits`/`last_sent` counters are unaffected, only the history
> rows stop being written. Editable at runtime in **Settings →
> Notifications** without a server restart. Row retention is
> [`cleanup_notificationlog`](./housekeeping.md#cleanup_notificationlog). See
> [Delivery history](../general/notifications.md#delivery-history) for what
> gets logged and where it surfaces in the web UI.
