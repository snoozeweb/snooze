---
sidebar_position: 5
---

# Web configuration

> Package location  
> `/etc/snooze/server-go/web.yaml` (Go canonical)
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False` (restart the server to re-read)

Configuration for the embedded web UI that snooze-server serves alongside the
API at `/web/`. Both fields default to the packaged install layout, so most
deployments never need a `web.yaml`.

An explicitly passed `snooze-server --web-dir` flag **overrides this section**
(`--web-dir=""` disables the UI regardless of `enabled`). When the flag is not
given, the section governs. A configured directory that is missing or
unreadable logs a warning and falls back to the API-only stub — it does not
prevent the server from starting.

The Go schema lives in `internal/config/schema/web.go`.

## Properties

### enabled

> Type  
> boolean
>
> Environment variable  
> `SNOOZE_SERVER_WEB_ENABLED`
>
> Default  
> `True`
>
> Serve the bundled web UI. When `false`, snooze-server exposes the API only —
> useful for headless deployments or when a separate frontend serves the UI.

### path

> Type  
> string (path)
>
> Environment variable  
> `SNOOZE_SERVER_WEB_PATH`
>
> Default  
> `'/var/lib/snooze/web'`
>
> Directory holding the built web UI assets (the contents of `web/dist`). The
> default matches where the deb/rpm packages install the bundle. Migrating
> from Python 1.x? Drop or update an old `path: /opt/snooze/web` — that
> location holds the obsolete Python UI (see the
> [migration notes](../migration/python-to-go.md)).

## Example

``` yaml
---
enabled: true
path: /var/lib/snooze/web
```
