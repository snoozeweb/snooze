---
sidebar_position: 1
---

# Core configuration

> Package location  
> `/etc/snooze/server-go/core.yaml`
>
> Loader  
> `internal/config` (koanf)
>
> Live reload  
> `False` (restart to re-read this file)

Core configuration. Mandatory; loaded once at startup. Runtime-mutable defaults sit in the database via the `settings` plugin instead. See [migration guide](../migration/python-to-go.md) for the field-by-field mapping from the Python 1.x `core.yaml`.

The Go-side schema lives in `internal/config/schema/core.go`.

## Properties

### listen_addr

> Type  
> string (IP address)
>
> Default  
> `'0.0.0.0'`
>
> IPv4 or IPv6 address the HTTP listener binds to.

### port

> Type  
> integer (1..65535)
>
> Default  
> `5200`
>
> TCP port the HTTP listener binds to.

### bootstrap_db

> Type  
> boolean
>
> Default  
> `True`
>
> Create the schema (Postgres / SQLite) or indexes (Mongo) on first start. Safe to leave on across restarts; the operation is idempotent.

### unix_socket

> Type  
> string (path)
>
> Default  
> `'/var/run/snooze/server.socket'`
>
> Legacy Python knob — preserved in the schema for forward compatibility with the older YAML. The Go admin socket is wired via the `--admin-socket` flag (default `/var/run/snooze/admin.sock`).

### audit_excluded_paths

> Type  
> array\[string\]
>
> Default  
> `['/api/patlite', '/metrics', '/web']`
>
> HTTP path prefixes excluded from the audit log. The router also has a hard-coded skip list for `/healthz`, `/readyz` so a busy probe loop doesn't drown the audit stream.

### process_plugins

> Type  
> array\[string\]
>
> Default  
> `['rule', 'aggregaterule', 'snooze', 'notification']`
>
> Order in which the alert-processing pipeline runs registered `Processor` plugins. Order matters: earlier plugins see the record first.

### database

> Type  
> object — one of the variants below
>
> Environment variable  
> `DATABASE_URL` (flat shortcut)
>
> See [Definitions](./core.md#definitions) below for the per-backend options.

### init_sleep

> Type  
> integer (seconds)
>
> Default  
> `5`
>
> Time to sleep before retrying bootstrap-time operations. Kept for backwards compatibility with the Python config; the Go bootstrap uses exponential backoff internally.

### create_root_user

> Type  
> boolean
>
> Default  
> `True`
>
> On first boot, create a local `root` user. The Go bootstrap generates a 24-byte random password, bcrypt-hashes it, and writes the plaintext to stderr **once**. See [Log in to the web interface](../getting_started/login.md) for the rotation steps.

### ssl

> Type  
> [SslConfig](./core.md#sslconfig)

### backup

> Type  
> [BackupConfig](./core.md#backupconfig)

### cors

> Type  
> [CorsConfig](./core.md#corsconfig)

## Definitions

### SqliteConfig (Go default)

Pure-Go SQLite backend via `modernc.org/sqlite` with JSON1. Single file, single writer, no external dependency. Ideal for single-node deployments and laptop testing.

#### type

> Type  
> `'sqlite'` (alias: `'file'`)
>
> Default  
> `'file'`

#### path

> Type  
> string (path)
>
> Default  
> `'./db.sqlite'`
>
> File path for the SQLite database. Use an absolute path in production (`/var/lib/snooze/db.sqlite`).

### MongoConfig

MongoDB driver: `go.mongodb.org/mongo-driver`. Supports replica sets, change streams (used as the message bus), and the same URI shape as the Python 1.x deployment.

#### type

> Type  
> `'mongo'` (alias: `'mongodb'`)

#### host

> Type  
> string
>
> Either a host:port pair or a full `mongodb://` URI.

#### port

> Type  
> integer
>
> Default  
> `27017`

#### database

> Type  
> string
>
> Default  
> `'snooze'`

#### dsn / url

> Either accepted; a full `mongodb://user:pass@host:port/db?…` URI overrides the per-field knobs.

#### replicaSet, tls, tlsCAFile, authSource

> Standard MongoDB URI options. The Go driver passes them through to its connection-string parser.

### PostgresConfig

Postgres driver: `jackc/pgx/v5`. The message bus rides on `LISTEN/NOTIFY` over the same connection pool.

#### type

> Type  
> `'postgres'` (alias: `'pg'`)

#### dsn

> Type  
> string
>
> libpq-style connection string. When set, overrides every per-field knob below. Required for [CloudNativePG-managed clusters](https://cloudnative-pg.io) where the URL comes from the app secret.

#### pool_min_size

> Type  
> integer
>
> Default  
> `1`

#### pool_max_size

> Type  
> integer
>
> Default  
> `10`

#### application_name

> Type  
> string
>
> Default  
> `'snooze-server'`
>
> Identifier reported to `pg_stat_activity`.

### DATABASE_URL shortcut

The flat environment variable `DATABASE_URL` accepts:

- `sqlite:/path/to/db.sqlite` *(future; for now use the typed config)*
- `mongodb://host:27017/snooze`
- `postgres://snooze:snooze@host:5432/snooze`
- `postgresql://…` *(alias)*

`Database.NormalizeURL` in `internal/config/schema/core.go` does the scheme → driver mapping.

### SslConfig

SSL configuration for the embedded HTTP listener.

#### enabled

> Type  
> boolean
>
> Default  
> `False`

#### certfile

> Type  
> string (path)
>
> Required when  
> `enabled == True`

#### keyfile

> Type  
> string (path)
>
> Required when  
> `enabled == True`

### BackupConfig

Configuration for the backup loop.

#### enabled

> Type  
> boolean
>
> Default  
> `True`

#### path

> Type  
> string (path)
>
> Default  
> `'/var/lib/snooze'`

#### excludes

> Type  
> array\[string\]
>
> Default  
> `['record', 'stats', 'comment', 'secrets', 'aggregate', 'system.profile']`
>
> Collection names skipped by the backup loop.

### CorsConfig

CORS headers emitted by the HTTP listener.

#### allow_origins

> Type  
> string (comma-separated or `*`)
>
> Default  
> `'*'`

#### allow_credentials

> Type  
> string (`'true'` / `'false'` / `'*'`)
>
> Default  
> `'*'`

