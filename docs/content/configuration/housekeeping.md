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

### cleanup_notification

> Type  
> number (time-delta)
>
> Default  
> `259200.0`
>
> Cleanup notifications that have been expired for the given duration (in seconds). Run daily

### renumber_field

> Type  
> number (time-delta)
>
> Default  
> `86400.0`
>
> Renumber given field from 0 to count(collection)-1

