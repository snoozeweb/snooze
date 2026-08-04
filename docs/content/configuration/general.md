---
sidebar_position: 2
---

# General configuration

> Package location  
> `/etc/snooze/server-go/general.yaml`
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False` (the YAML is bootstrap-only)
>
> Runtime store  
> `settings` plugin (`PATCH /api/v1/settings/{key}`)

General configuration of snooze. The YAML file seeds the defaults read by the server at startup; thereafter the runtime-mutable values live in the `settings` collection in the database and are edited through the WebUI or the REST API.

The Go schema lives in `internal/config/schema/general.go`.

## Properties

### default_auth_backend

> Type  
> 'local' \| 'ldap' \| 'anonymous'
>
> Default  
> `'local'`
>
> Backend that will be first in the list of displayed authentication backends

### local_users_enabled

> Type  
> boolean
>
> Default  
> `True`
>
> Enable the creation of local users in snooze. This can be disabled when another reliable authentication backend is used, and the admin want to make auditing easier

### metrics_enabled

> Type  
> boolean
>
> Default  
> `True`
>
> Enable Prometheus metrics (the `/metrics` scrape endpoint) **and** dashboard
> stat counter writes. When set to `false`, no counter buckets are persisted and
> the dashboard charts will be empty.

### anonymous_enabled

> Type  
> boolean
>
> Default  
> `False`
>
> Enable anonymous user login. When a user log in as anonymous, he will be given user permissions

### ok_severities

> Type  
> array\[string\]
>
> Default  
> `['ok', 'success']`
>
> List of severities that will automatically close the aggregate upon entering the system. This is enforced centrally in the ingest pipeline for **all** inputs (syslog, SNMP traps, raw API posts — not just webhook integrations), and only stamps records that arrive without an explicit state; a receiver that already sets `state` itself (e.g. a webhook integration reporting its own close) is unaffected. This is mainly for icinga/grafana that can close the alert when the status becomes green again. Editable live from the web UI (Settings → General) — the config file value is only the baseline used until it is overridden there.

### snooze_bypass_severities

> Type  
> array\[string\]
>
> Default  
> `[]`
>
> Escalation safety rail: severities exempt from **all** snooze filters. A record whose `severity` matches any entry passes straight through the snooze plugin as if no rule matched — checked before any snooze rule is evaluated. Set it to e.g. `['critical']` so a broad maintenance filter can never silence an escalation to the highest priority. Closes of an existing alert already bypass snooze filters automatically and do not need to be listed here. Values are matched case-insensitively; the default (empty) leaves suppression behaviour unchanged. Editable live from the web UI (Settings → General) — previously this required a config-file edit and a restart.

Snooze ranks severities on a syslog-anchored ladder (`emerg > alert > crit/critical > err/error > warn/warning > notice > info > debug > ok`), with common aliases folded in. Values are matched case-insensitively; unknown severities are accepted and treated as opaque (they don't participate in ordering until ranked — see the console severity settings).

