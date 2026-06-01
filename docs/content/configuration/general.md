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
> List of severities that will automatically close the aggregate upon entering the system. This is mainly for icinga/grafana that can close the alert when the status becomes green again

