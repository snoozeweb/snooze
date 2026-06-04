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

The two settings below configure this node's heartbeat identity and cadence; the standalone 1-second polling loop used by Python 1.x is gone.

The Go schema lives in `internal/config/schema/syncer.go`.

## Properties

### hostname

> Type  
> string
>
> Default  
> OS hostname, falling back to `snooze`
>
> Identity of this node in the cluster heartbeat document — the name the heartbeat runner writes. Set different values per replica.

### sync_interval

> Type  
> Duration
>
> Default  
> `1s`
>
> Cadence of the node heartbeat and the debounce window the syncer applies to change-feed events. Cache invalidation itself is driven by the backend's change feed, not a polling loop, but the heartbeat document is rewritten at this cadence so the cluster page in the WebUI reflects liveness. (The legacy `sync_interval_ms` and the `total` replica-count knob were removed in 2.0.)

