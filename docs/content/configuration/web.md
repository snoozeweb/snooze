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

## Console defaults (`/api/v1/config`)

The `web.yaml` section above is **file config** — it governs how the server
serves the bundle. The *contents* of the console (which columns the alert list
shows, how often it refreshes, branding, the severity ladder) are **runtime
settings**, so they live in the DB-backed `console` settings section and are
editable from the admin **Settings** page without a restart or rebuild.

The server exposes them at a single public, read-only endpoint:

```
GET /api/v1/config
```

The SPA fetches this document once at boot and uses its built-in hardcodes only
as a fallback. The endpoint requires **no auth token** — the login screen needs
branding before a token exists — and is deliberately presentation-only (it does
not expose `client_id`, tenant, or provider details). The response is the
server's code defaults overlaid by any keys set in the `console` settings
section:

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `refresh_interval` | int (seconds) | `5` | Alert-list auto-refresh cadence. |
| `sort_by` | string | `-date_epoch` | Default sort; a `-` prefix means descending. |
| `default_filter` | string | `""` | Search expression pre-filled in the alerts SearchBar on a clean load; URL `?search=` overrides it per session. |
| `columns` | string[] | `date_epoch, severity, state, acked_by, hits, host, process, source, environment, ttl, message` | Ordered alert-table column ids. |
| `severity_ranks` | map\<string,int> | built-in ladder | Label → rank (0 = most severe). Merged onto the built-in ladder. |
| `logo` | string | `""` | Logo URL or `data:` URI; empty uses the bundled Snooze logo. |
| `title` | string | `""` | Browser tab title; empty defaults to `"Snooze"`. |
| `audio` | string | `""` | URL of an audio file played when new alerts arrive during auto-refresh; empty disables. |
| `clipboard_template` | string | `""` | `{{field}}` template for the row copy action; empty formats as pretty-printed JSON. |

The response also carries a derived `severity_order` array (labels most→least
severe). It is **always recomputed** from the merged `severity_ranks` and is
never stored or edited directly.

Edit these keys from the admin Settings page (the **Console** group), or via the
settings CRUD API:

```
PUT /api/v1/settings/console
```

### Branding and UX fields

**`console.logo`** — Set to a URL (e.g. `https://cdn.example.com/logo.png`) or a `data:` URI to
replace the bundled Snooze logo in the sidebar and login screen. An empty value (the default)
restores the bundled logo. Prefer a URL over a data URI — large data URIs bloat the public config
blob served to every browser session.

**`console.title`** — The browser-tab title. Defaults to `"Snooze"` when empty; set it to your
organisation name (e.g. `"Acme Ops"`) to brand the tab across every page.

**`console.audio`** — URL of an audio file (`.wav`, `.mp3`, `.ogg`) played once each time the
auto-refresh poll detects new incoming alerts (i.e. the total count grows). The cue is silent on
initial page load and when auto-refresh is disabled. Browsers require a prior user gesture before
they allow audio playback; if the alert cue does not sound, ensure the operator has interacted with
the page (e.g. clicked something after logging in). Set to empty to disable entirely.

**`console.clipboard_template`** — A `{{field}}` substitution template for the "Copy" action in
the alert row context menu. Each `{{fieldname}}` is replaced with the matching field value from the
alert record (unknown fields expand to an empty string). When empty, the action copies the full
alert as pretty-printed JSON (the prior default). Example: `{{host}} — {{message}}` copies a
one-liner summary.

**`console.default_filter`** — A SearchBar DSL expression pre-filled on a clean page load. Useful
for operators who always work within a specific scope (e.g. `severity = critical`). The URL
`?search=` parameter overrides it per-session, so individual deep-links and saved searches are
always honoured.

### Ranking a custom severity

Snooze owns a fixed palette of six severity **theme tokens**
(`--severity-{critical,error,warning,info,ok}`, plus a muted fallback) — colours
are CSS, light/dark-aware, and **not** configured here. To onboard a custom
severity label, give it a **rank** instead of a colour. `severity_ranks` merges
key-by-key onto the built-in ladder, so you add one entry without re-listing the
whole ladder:

```jsonc
// PUT /api/v1/settings/console  (the console section's stored values)
{
  "severity_ranks": { "p1": 2 }
}
```

Rank `2` is the `critical` bucket, so `p1` now **renders red** (the
`--severity-critical` token) and **sorts alongside the criticals** — no rebuild,
no hex, theme-safe. The built-in ladder (`emerg`=0, `alert`=1, `crit`/`critical`/
`fatal`=2, `err`/`error`=3, `warn`/`warning`=4, `notice`=5, `info`=6, `debug`=7,
`ok`=8) remains in effect for every label you do not override; the server ladder
shipped by `/api/v1/config` is the single runtime source of truth (the frontend
keeps an identical map only as an offline fallback).
