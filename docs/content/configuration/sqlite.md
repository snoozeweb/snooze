---
sidebar_position: 8
---

# SQLite backend

Snooze 2.0 ships a SQLite backend powered by [modernc.org/sqlite](https://modernc.org/sqlite) — a pure-Go translation of the SQLite C source, so the daemon stays statically linked and cgo-free. JSON1 is enabled by default; documents are stored as `json` blobs in a single column per collection.

This is the **default** backend when `database.type` is unset, or set to either `sqlite` or the legacy `file` alias.

## When to use it

- Single-node deployments. SQLite is single-writer; you cannot run more than one `snooze-server` replica against the same file.
- Laptop / CI / demo environments where you don't want to provision a Mongo replica set or a Postgres cluster.
- Edge deployments where one `snooze-server` Helm release per cluster (StatefulSet + PVC) is acceptable.

For production multi-replica deployments, prefer MongoDB or Postgres.

## Configuration

``` yaml
# /etc/snooze/server-go/core.yaml
database:
  type: sqlite
  path: /var/lib/snooze/db.sqlite
```

Or, via environment variables:

``` console
$ export SNOOZE_DATABASE_TYPE=sqlite
$ export SNOOZE_DATABASE_PATH=/var/lib/snooze/db.sqlite
$ snooze-server
```

## Schema and shape

Each logical collection becomes a table:

``` sql
CREATE TABLE snooze_<collection> (
    uid        TEXT PRIMARY KEY,
    data       TEXT NOT NULL,           -- JSON document
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_<table>_updated_at ON snooze_<collection>(updated_at);
```

Tables are created lazily on first write. Queries use the JSON1 `json_extract()` function on the `data` column.

## Backups and migration

The simplest backup is to `cp` the database file while the daemon is stopped, or use the SQLite `.backup` command online:

``` console
$ sqlite3 /var/lib/snooze/db.sqlite ".backup '/var/lib/snooze/db.sqlite.bak'"
```

To migrate **off** SQLite onto Mongo or Postgres, dump each collection through the REST API and re-POST it against the new backend; UIDs survive.

## Helm

The Helm chart's `database.kind: sqlite` mode renders a StatefulSet with a single replica and a persistent volume mounted at `/var/lib/snooze`. See `packaging/helm/values.yaml`.

