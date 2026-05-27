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

