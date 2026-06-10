---
sidebar_position: 1
---

# Migrating from Snooze 1.x (Python) to Snooze 2.0 (Go)

Snooze 2.0 is a ground-up rewrite of `snooze-server` from Python to Go.
The wire contract is intentionally close to the Python API but a handful
of legacy shapes have been retired. This guide is for operators with a
running 1.x cluster who want to move to 2.0 without redoing their data.

> See also: [`/CHANGELOG.md`](https://github.com/snoozeweb/snooze/blob/master/CHANGELOG.md) for the full v2.0.0
> entry, and the [Configuration](../configuration/index.md) section for the
> new two-tier configuration model.

## Why migrate

* Single statically-linked binary. No more Python virtualenv, no
  `uv sync`, no `kombu[mongodb]`.
* SQLite + MongoDB + Postgres all first-class. SQLite is now the
  zero-dependency default for small deployments; the legacy `file`
  backend (TinyDB) is gone and `database.type: file` now selects SQLite.
* Real OpenTelemetry traces and a Prometheus registry served at
  `/metrics`. Structured `log/slog` JSON logs by default.
* The kombu / amqp-on-mongo bridge is retired; the message bus is the
  backend's own change feed (Postgres `LISTEN/NOTIFY`, Mongo change
  streams, or an in-process channel for SQLite).
* OpenAPI 3.1 spec ships in `api/openapi.yaml`.

The wire contract is **mostly** compatible: existing alert ingestion
webhooks (Grafana, AlertManager, InfluxDB 2, Kapacitor, Prometheus) work
without change. Custom CRUD clients need light adjustments — see the
[API mapping](#api-endpoint-mapping) below.

## Configuration mapping

Two-tier model: a small bootstrap YAML directory (read once at startup)
and a database-backed `settings` plugin (mutable at runtime through the
REST API or the WebUI).

| Python 1.x location                              | Go 2.0 location                                                       | Notes                                                                  |
|--------------------------------------------------|-----------------------------------------------------------------------|------------------------------------------------------------------------|
| `/etc/snooze/server/core.yaml`                   | `/etc/snooze/server-go/core.yaml`                                     | Same schema; see `internal/config/schema/core.go`                      |
| `/etc/snooze/server/general.yaml`                | `/etc/snooze/server-go/general.yaml`                                  | Subset moved to the `settings` plugin (DB)                             |
| `/etc/snooze/server/ldap_auth.yaml`              | `/etc/snooze/server-go/ldap.yaml`                                     | Renamed; field names unchanged                                         |
| `/etc/snooze/server/housekeeping.yaml`           | `/etc/snooze/server-go/housekeeper.yaml`                              | Renamed                                                                |
| `/etc/snooze/server/notification.yaml`           | `/etc/snooze/server-go/notification.yaml`                             | Unchanged                                                              |
| `/etc/snooze/server/web.yaml`                    | `/etc/snooze/server-go/web.yaml`                                      | Same fields, but **drop or update a 1.x `path`**: `/opt/snooze/web` is the obsolete Python bundle; the Go default is `/var/lib/snooze/web` |
| WebUI Settings → General                         | `settings` collection in the DB                                        | Editable via `PATCH /api/v1/settings/{key}` or the WebUI               |
| WebUI Settings → Notifications                   | `settings` collection (`notification_*` keys)                          | Editable via API                                                       |
| `DATABASE_URL=mongodb://…`                       | `DATABASE_URL=mongodb://…`                                            | Unchanged                                                              |
| `DATABASE_URL=postgresql://…`                    | `DATABASE_URL=postgresql://…`                                         | Unchanged                                                              |
| (new)                                            | `database.type: sqlite` + `database.path: /var/lib/snooze/db.sqlite`  | Pure-Go SQLite/JSON1                                                   |
| Python `core.bootstrap_db: true`                 | `core.bootstrap_db: true`                                             | Same flag                                                              |
| (none — Python rewrote YAML on disk)             | `--config /etc/snooze/server-go`                                      | Hot-reload removed; restart to re-read bootstrap YAML                  |
| `core.unix_socket`                               | `core.unix_socket`                                                    | Default `/var/run/snooze/server.socket`                                |
| `core.cluster.enabled`                           | (gone)                                                                | Replaced by the per-DB syncer; enabled by default                      |
| `core.ssl.{enabled,certfile,keyfile}`            | `core.ssl.{enabled,certfile,keyfile}`                                 | Unchanged                                                              |
| `core.cors.{allow_origins,allow_credentials}`    | `core.cors.{allow_origins,allow_credentials}`                         | Unchanged                                                              |
| `audit.excluded_paths`                           | `core.audit_excluded_paths`                                           | Moved under `core` section                                             |
| `general.default_auth_backend`                   | `general.default_auth_backend`                                        | `local`, `ldap`, `anonymous`                                           |
| `general.metrics`                                | `general.metrics_enabled`                                             | Renamed                                                                |
| `general.anonymous_enabled`                      | `general.anonymous_enabled`                                           | Unchanged                                                              |
| `general.ok_severities`                          | `general.ok_severities`                                               | Case-folded at load time                                               |
| `general.local_users_enabled`                    | `general.local_users_enabled`                                         | Unchanged                                                              |
| `ldap_auth.bind_dn` / `…`                         | `ldap.bind_dn` / `…`                                                  | Renamed section, field names unchanged                                  |
| `housekeeping.*`                                 | `housekeeper.*`                                                       | Durations accept both bare seconds and Go-style `"5m"`                 |
| `notification.notification_freq` (seconds)       | `notification.notification_freq` (Duration; `60s` or `60`)            | Stringly typed → Duration                                              |
| `syncer.interval_ms`                             | `syncer.sync_interval` (Duration)                                    | Single knob now; the legacy `sync_interval_ms` was dropped             |
| New                                              | `auth.token_secret`, `auth.token_lease`, `auth.token_algorithm`       | `token_secret` overrides the DB-generated JWT key (≥32 bytes)          |

Environment variables follow `SNOOZE_SERVER_<SECTION>_<KEY>` with `_` as the
separator: e.g. `SNOOZE_SERVER_CORE_PORT=5201` overrides `core.port`,
`SNOOZE_SERVER_AUTH_TOKEN_SECRET=…` overrides `auth.token_secret`. The flat
`DATABASE_URL` shortcut is preserved.

When `auth.token_secret` (env `SNOOZE_SERVER_AUTH_TOKEN_SECRET`) is set it
becomes the HS256 signing key, taking precedence over the random key Snooze
otherwise generates and stores in the `secrets` collection. The value must be
at least 32 bytes or the server refuses to boot. Leave it empty to keep the
auto-generated, per-install key (the default).

### Runtime-editable settings (the `settings` plugin)

The Python WebUI rewrote `general.yaml` on disk under a `filelock`. In
2.0 those values live in the `settings` collection in the database and
are reachable through the same REST CRUD shape as any other plugin:

```bash
curl -H "Authorization: Bearer $TOKEN" \
     https://snooze.example.com/api/v1/settings
curl -X PATCH -H "Authorization: Bearer $TOKEN" \
     -d '{"value": ["ok","success","resolved"]}' \
     https://snooze.example.com/api/v1/settings/ok_severities
```

Changes take effect immediately across every replica via the syncer's
backend-native bus. The bootstrap YAML continues to provide defaults at
first boot.

## API endpoint mapping

### Authorization header

```diff
- Authorization: JWT eyJhbGciOi…
+ Authorization: Bearer eyJhbGciOi…
```

Tokens themselves are still HS256 JWTs by default; the `JWT` scheme name
on the wire is the only change.

### List endpoints

The positional URL shape used by 1.x:

```
GET /api/falcon/rule/(.+)/(\d+)/(\d+)/(.+)/(true|false)
GET /api/falcon/rule/<search>/<perpage>/<pagenb>/<orderby>/<asc>
```

…is replaced by query-string parameters and a `data` envelope:

```
GET /api/v1/rule?q=<base64url-json>&offset=0&limit=20&orderby=name&asc=true
```

The `q` parameter is a base64url-encoded JSON condition (the same
condition AST the Python `Condition` model used). For complex queries
that overflow URL length:

```
POST /api/v1/rule/search
Content-Type: application/json

{"condition": {"type": "AND", "left": …, "right": …},
 "offset": 0, "limit": 100, "orderby": "name", "asc": true}
```

Response shape (every paginated endpoint):

```json
{
  "data": [{"uid": "…", "name": "…", …}, …],
  "meta": {"count": 20, "limit": 20, "offset": 0, "total": 137}
}
```

### CRUD verbs

| Operation        | Python 1.x                                            | Go 2.0                                  |
|------------------|-------------------------------------------------------|-----------------------------------------|
| Create           | `POST /api/falcon/{plugin}` (array or single)         | `POST /api/v1/{plugin}` (array or single) |
| Replace          | `POST /api/falcon/{plugin}?replace=true`              | `PUT /api/v1/{plugin}/{uid}`            |
| Partial update   | (none — clients re-POSTed)                            | `PATCH /api/v1/{plugin}/{uid}`          |
| Get one          | `GET /api/falcon/{plugin}/<uid>` (positional)         | `GET /api/v1/{plugin}/{uid}`            |
| Delete one       | `DELETE /api/falcon/{plugin}/<uid>`                   | `DELETE /api/v1/{plugin}/{uid}`         |
| Bulk delete      | `DELETE /api/falcon/{plugin}` with body               | `DELETE /api/v1/{plugin}?q=<b64-json>`  |
| Search           | (folded into list URL)                                | `POST /api/v1/{plugin}/search`          |
| Schema           | (Pydantic auto)                                       | `GET /api/v1/schema/{plugin}`           |
| Permissions      | (`/api/permissions`)                                   | `GET /api/v1/permissions`               |

### Globals

| Function          | Python 1.x                | Go 2.0                            |
|-------------------|---------------------------|-----------------------------------|
| Health            | `GET /api/health`         | `GET /healthz` (liveness)         |
|                   |                           | `GET /readyz` (readiness)         |
|                   |                           | `GET /api/v1/health` (verbose)    |
| Metrics           | `GET /metrics` (optional) | `GET /metrics` (always)           |
| Login (local)     | `POST /api/login`         | `POST /api/v1/login/local`        |
| Login (LDAP)      | `POST /api/login/ldap`    | `POST /api/v1/login/ldap`         |
| Login (anonymous) | `GET /api/login/anonymous`| `POST /api/v1/login/anonymous`    |
| Backend list      | `GET /api/login`          | `GET /api/v1/login`               |
| Alert ingest      | `POST /api/alert`         | `POST /api/v1/alerts`             |
| Webhooks          | `POST /api/webhook/{name}`| `POST /api/v1/webhook/{name}`     |

The webhook receivers (`alertmanager`, `grafana`, `influxdb2`,
`kapacitor`, `prometheus`) accept the same payload shapes as 1.x.

### Error envelope

```json
{
  "error": {
    "code": "validation_error",
    "message": "value out of range",
    "details": {"field": "ttl"},
    "request_id": "01HJZ8…",
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
  }
}
```

Stable code values: `bad_request`, `unauthorized`, `forbidden`,
`not_found`, `conflict`, `validation_error`, `unavailable`, `internal`.

## CLI command mapping

The Python codebase used a single `snooze-server` entry point with a
fistful of click subcommands and bash glue scripts (`snooze-clt`). In
2.0 there is one daemon binary plus a separate `snooze` CLI:

| Python 1.x                                | Go 2.0                                                                |
|-------------------------------------------|-----------------------------------------------------------------------|
| `snooze-server` (Falcon WSGI app)          | `snooze-server --config /etc/snooze/server-go` (default subcommand)   |
| `snooze-server --version`                  | `snooze-server version`                                               |
| `snooze-clt token` (admin script)         | `snooze-server root-token [--socket /var/run/snooze/admin.sock]`      |
| `snooze-clt …` (read-only DB queries)     | `snooze` (separate CLI binary; see `cmd/snooze/main.go`)              |
| (none — manual)                            | `snooze-server migrate-config --from /etc/snooze/server` *(stub)*     |
| `snooze-input-relp` (Python component)     | `snooze-relp`                                                         |
| `snooze-input-syslog`                      | `snooze-syslog`                                                       |
| `snooze-input-snmptrap`                    | `snooze-snmptrap`                                                     |
| `snooze-output-mattermost`                 | `snooze-mattermost`                                                   |
| `snooze-output-googlechat`                 | `snooze-googlechat`                                                   |
| `snooze-output-teams`                      | `snooze-teams`                                                        |
| `snooze-output-smtp` (forwarder)           | `snooze-smtp`                                                         |
| `snooze-pacemaker` (resource agent)        | `snooze-pacemaker`                                                    |

Selected flags on `snooze-server`:

* `--config <dir>` — directory containing the bootstrap YAML files
  (default `/etc/snooze/server-go`).
* `--listen host:port` — override `core.listen_addr:core.port`.
* `--admin-socket <path>` — admin Unix socket location (default
  `/var/run/snooze/admin.sock`). Used by the `root-token` subcommand.
* `--log-format json|text`, `--log-level debug|info|warn|error`.
* `--otel-endpoint host:4317` — enable OTLP/gRPC trace export.
* `--no-admin-socket`, `--no-http` — debugging knobs.

## Root user rotation

The Python bootstrap wrote `sha256("root")` into the local user document
on first boot, so `root:root` always worked until you changed it through
the WebUI. The Go bootstrap:

1. Looks for an existing `(name=root, method=local)` document.
2. If none exists, generates a 24-byte random password, bcrypt-hashes it,
   stores the hash, and **prints the plaintext password once to stderr**:

   ```
   snooze-server: bootstrap: root password = <base64url>
   ```

3. Subsequent boots do nothing.

If you are upgrading an existing database, your old root document is
kept (and `root:root` still works, because the existing password hash
hasn't been rewritten). Rotate it immediately:

```bash
# Mint a one-shot token over the admin Unix socket (uid 0 only).
sudo snooze-server root-token
# → eyJhbGciOiJIUzI1NiIsInR5c…

# Set a new password through the regular API.
TOKEN=eyJ…
NEW=$(openssl rand -base64 24)
curl -X PATCH \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d "{\"password\":\"$NEW\"}" \
     https://snooze.example.com/api/v1/user/root@local
```

The server hashes `password` with bcrypt on write; plaintext never
touches the database. The one-shot admin-socket token has a 5-minute
lease; it is meant as the rescue path on the first boot, not a long-
lived credential.

## DB migration

In-place: 2.0 reads the same collection layout as 1.x, with two
exceptions documented below. Operators with an existing MongoDB
deployment can point a fresh `snooze-server` at the same database and
let it pick up where Python left off.

### Collections to drop

Snooze 1.x used `kombu[mongodb]` as its in-cluster message bus. The
broker stored its state in three collections:

```
snooze_kombu_messages
snooze_kombu_message_queues
snooze_kombu_broadcast_*
```

Snooze 2.0 uses the native change feed of each backend (Mongo change
streams, Postgres `LISTEN/NOTIFY`, in-process channel for SQLite) and
**does not touch** these collections. They can be dropped at any time:

```javascript
// mongosh
use snooze;
db.snooze_kombu_messages.drop();
db.snooze_kombu_message_queues.drop();
db.getCollectionNames()
  .filter(n => n.startsWith("snooze_kombu_broadcast_"))
  .forEach(n => db.getCollection(n).drop());
```

### Collections that gained schema

* `settings` — new in 2.0. Seeded from `general.yaml` /
  `notification.yaml` on first boot if empty. Documents are
  `{key, value, type, scope, updated_at}`.
* `user` — `password` field is now a bcrypt hash (60 chars,
  `$2a$…`). Existing sha256 hashes from 1.x are still accepted; the
  next successful login transparently re-hashes the credential to
  bcrypt.

### Postgres / SQLite

The Postgres backend stores documents one-table-per-collection in a
single `jsonb` column. The schema is created automatically on first
start when `core.bootstrap_db: true` (the default). The SQLite backend
uses one file (`./db.sqlite` by default; configurable through
`database.path`) and creates its tables on demand.

### Cross-backend migration

Going from Mongo to Postgres or to SQLite is **out of scope** for this
guide: there is no built-in dump/restore tool. The general approach is:

1. Read each collection with the v1 API (`GET /api/v1/{collection}` with
   pagination).
2. Bootstrap a fresh server on the target backend.
3. POST the records back. UIDs are preserved.

A future release will ship a `snooze-server migrate-db` subcommand.
Until then, the `snooze` CLI's `dump` / `load` verbs (TODO: not yet
implemented; tracked in `cmd/snooze/main.go`) are the recommended path.

## Quick checklist

- [ ] Drop `Authorization: JWT` from every client. Use `Bearer`.
- [ ] Replace positional list URLs with the `?q=&offset=&limit=` form.
- [ ] Wrap your list-response parsers to consume `{data, meta}`.
- [ ] Move `general.metrics` → `general.metrics_enabled` in YAML.
- [ ] Move runtime tweaks out of YAML hot-reload and into the `settings`
      plugin.
- [ ] Drop the `snooze_kombu_*` collections.
- [ ] Rotate the root password on first boot.
- [ ] If you ran a custom Python plugin, port it to Go under
      `internal/pluginimpl/<name>/` or drop it.
- [ ] Update `/etc/snooze/server` → `/etc/snooze/server-go`, or pass
      `--config /etc/snooze/server` and accept the legacy filename
      aliases (`ldap_auth.yaml`, `housekeeping.yaml`).

