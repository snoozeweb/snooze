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

