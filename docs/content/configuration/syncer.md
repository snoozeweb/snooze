---
sidebar_position: 6
---

# Syncer configuration

> Package location  
> `/etc/snooze/server-go/syncer.yaml`
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False`

The syncer keeps each replica's in-memory plugin caches consistent. In 2.0 the cross-replica bus rides on the **backend's** change feed:

- MongoDB — change streams (`watch()` on each collection)
- PostgreSQL — `LISTEN/NOTIFY` (one channel per collection)
- SQLite — an in-process channel (single-replica, no fan-out needed)

The settings below are mostly status-reporting knobs; the standalone 1-second polling loop used by Python 1.x is gone.

The Go schema lives in `internal/config/schema/syncer.go`.

## Properties

### hostname

> Type  
> string
>
> Default  
> `os.Hostname()`
>
> Identity of this node in the cluster heartbeat document. Set different values per replica.

### total

> Type  
> integer
>
> Default  
> `1`
>
> Expected number of replicas (used by the verbose health endpoint to flag degraded clusters).

### sync_interval / sync_interval_ms

> Type  
> Duration / integer (ms)
>
> Default  
> `1s` / `1000`
>
> Heartbeat interval. The actual cache invalidation does not rely on this loop in 2.0 — it is driven by the backend's change feed — but the heartbeat document is still written at this cadence so the cluster page in the WebUI reflects liveness.

