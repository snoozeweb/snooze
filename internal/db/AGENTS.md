# AGENTS.md — storage

> Scope: the storage layer under `internal/db/`. For repo-wide rules and
> architecture read `../../AGENTS.md` first — it wins on any conflict.

Snooze supports **exactly three** backends behind one interface. The cardinal
rule (root hard rule #7): everything goes through `db.Driver`; never add a
fourth backend, and never sneak a concrete-driver call into the layers above.

---

## The contract: `db.Driver` (`db.go`)

One interface, implemented by all three backends — identically except for a few
documented divergences (see "Known cross-backend divergences"). Methods group as:

* **Query** — `Search`, `GetOne`, `Convert` (compiles a `condition.Cond` into
  an opaque per-driver `DriverQuery`).
* **CRUD** — `Write` (upsert governed by `WriteOptions`), `ReplaceOne`,
  `UpdateOne`, `Delete`.
* **Bulk** — `BulkIncrement`, `IncMany`, `SetFields`, `UnsetFields` (deletes
  named top-level keys so `EXISTS field` stops matching — distinct from a merge
  write that preserves unmentioned keys), `AppendList`/`PrependList`/`RemoveList`.
* **Maintenance** — `CreateIndex`, `ListCollections`, `Drop`, `Backup`, the
  `Cleanup*` retention sweeps, `ComputeStats`.
* **`Watcher() syncer.Bus`** — the change feed the syncer rides (see below).
* **`Close()`** — idempotent.

Records are free-form `db.Document` (`map[string]any`); typed views
(`snoozetypes.Record`) are applied at the plugin/API boundary, not here.

**Adding a method to `Driver`** means implementing it in **all three** drivers
(the interface won't compile otherwise) and covering it for each. If a feature
is genuinely backend-specific, model it as an **optional capability** probed by
type assertion with an in-Go fallback — `RecordAggregator` is the pattern: the
`stats` plugin uses it when present and falls back to `Search`+reduce when not.

---

## The three backends

| Dir         | Driver / library                  | Notes                                   |
|-------------|-----------------------------------|-----------------------------------------|
| `mongo/`    | `go.mongodb.org/mongo-driver/v2`  | Change-stream `Watcher`; replica sets.  |
| `postgres/` | `jackc/pgx/v5` (+ pgxpool, jsonb) | `LISTEN/NOTIFY` `Watcher`.              |
| `sqlite/`   | `modernc.org/sqlite` (pure Go)    | Single-writer; in-process `Watcher`.    |

Per-backend file shape is consistent: `driver.go` (the `Driver` impl — the big
one), `convert.go` (`condition.Cond` → query) plus a `dialect.go` for the two
SQL backends (the per-dialect leaf SQL behind the shared builder), `cleanup.go`
(retention), `bulk.go`, plus the watcher (`mongo/watch.go`, `postgres/listen.go`,
`sqlite/bus.go`). The two SQL backends additionally carry `schema.go`
(table/index DDL); Mongo, being schemaless, has none.

`sql/` (builder.go, dialect.go, maintenance.go) is the **shared Cond → WHERE
builder** for the two SQL backends. `Builder.Convert` owns the boolean tree walk
(AlwaysTrue / AND / OR / NOT, paren grouping, empty-set literals, sub-condition
recursion, placeholder bookkeeping); every per-dialect leaf fragment (JSON path
extraction, type-aware casts, regex op, array/scalar two-branch unions,
full-scan SEARCH) lives behind the `Dialect` interface, implemented in
`postgres/dialect.go` and `sqlite/dialect.go`. Each backend's `convert.go` wires
its `dialect` into the shared `Builder` and keeps only the non-WHERE pieces
(ORDER BY, LIMIT/OFFSET, query assembly) plus its JSON-path helpers. The
builder full-scans an empty-`searchFields` SEARCH (matching the live backends),
so the historical "`FALSE`" drift is gone. Mongo keeps its own hand-rolled
translator (it is not SQL).

`asyncwriter/` sits between the pipeline and the driver, coalescing increment
storms into batched `BulkIncrement` calls — identical for all three backends.
It takes an injected clock (`asyncwriter/clock.go`) so tests fake time. Its
coalescing key is `hashSearch(search)` — the sorted keys joined with
`fmt.Sprintf("%v")` of each value — so two searches whose values *stringify*
identically (e.g. `int64(1)` vs `float64(1)`) merge into one op.

---

## Known cross-backend divergences

A few intentional/known splits remain (the shared `dbtest` suite — now wired
into all three driver tests — guards against new ones). The `CleanupTimeout`
and `CleanupAuditLogs` divergences noted in earlier revisions are now
reconciled across backends.

* **SEARCH with no registered fields.** Mongo matches *nothing*; the two SQL
  backends fall back to a full-row regex scan (`data::text ~* needle` /
  `regexp(needle, data)`). Register `search_fields` (plugin metadata) so a
  bare-word search is well-defined and indexed.
* **IN with a nested sub-condition.** Mongo (`$elemMatch`) and Postgres
  (`jsonb_array_elements` recursion) support a `Cond` sub-form; SQLite does
  **not** — a `Cond` value degrades to literal `fmt.Sprint` membership. Postgres
  additionally errors on the legacy *list*-form nested cond.
* **Cleanup change-feed events.** Mongo cleanup deletes surface in the change
  stream; Postgres emits a notify only from `CleanupTimeout`; SQLite publishes
  nothing. Peer caches may not invalidate after a retention sweep on the SQL
  backends.
* **`RecordAggregator`** (the optional probed capability) is implemented **only
  by SQLite** (`RecordStats`). Postgres and Mongo have no fast aggregator, so the
  `stats` plugin falls back to `Search`+reduce there.

---

## Tests

Backend tests are **integration tests**: `mongo`/`postgres` spin up a real
server via testcontainers (Docker required); `sqlite` uses a fresh in-memory DB
per test. They're gated by `testing.Short()` — `go test -short` skips them.

`dbtest/` is the **shared cross-backend conformance suite**. `RunDriverSuite(t,
name, factory)` runs the backend-agnostic cases, and each backend's
`driver_test.go` wires it via `TestDriverSuite` (sqlite uses a fresh in-memory
DB; postgres/mongo share one testcontainer per suite, dropping collections
between cases). **When you touch cross-backend behaviour, add the case there**
rather than copy-pasting a per-backend variant; the hand-rolled per-backend
tests now cover only backend-specific cases (ping, backup, dialect quirks).

```bash
go test -short ./internal/db/...           # skip the container-backed paths
go test ./internal/db/sqlite/...           # full, no Docker needed
go test ./internal/db/postgres/...         # full, needs Docker
```

---

## Don't

* Add a fourth backend, or branch on `*sql.DB` vs Mongo above the driver line.
* Import `internal/db/{mongo,postgres,sqlite}` from anywhere but their own tests
  and the boot wiring — callers depend on `db.Driver`.
* Hand-build a backend query string in a plugin — go through `Convert` so the
  condition DSL stays the single query surface.
