# AGENTS.md

> Read by every agent that touches this repo (Claude, Cursor, Copilot, Codex…).
> Snooze is a **Go codebase** since v2.0.0. Anything you remember about the
> Python 1.x server is stale.

---

## Project at a glance

* **What**: Clustered log-aggregation and alerting server.
* **Repo**: `github.com/japannext/snooze`. Module path matches the import path.
* **Language floor**: Go 1.25 (`go.mod`). Build flags: `-trimpath -tags 'osusergo,netgo'`,
  `CGO_ENABLED=0`. Container images are distroless.
* **Frontend**: Vue 3 SPA under `web/` (untouched by the Go rewrite; still
  Vue CLI 4 / Options API).
* **Versioning**: `git tag vX.Y.Z` on `master`. The user manages all git
  operations — never `commit`, `push`, or `tag` without explicit instruction.

---

## Repo map

```
snooze/
├── cmd/                          # One subdirectory per binary (main package)
│   ├── snooze-server/           # API + UI + pipeline workers
│   ├── snooze/                  # Operator CLI
│   ├── snooze-syslog/           # Syslog input
│   ├── snooze-snmptrap/         # SNMP trap input
│   ├── snooze-relp/             # RELP input
│   ├── snooze-smtp/             # SMTP input
│   ├── snooze-googlechat/       # Output notifier
│   ├── snooze-mattermost/       # Output notifier
│   ├── snooze-teams/            # Output notifier
│   └── snooze-pacemaker/        # Pacemaker integration helper
├── internal/                     # Private application packages
│   ├── api/                     # chi router, middleware, REST handlers, admin socket
│   ├── auth/                    # JWT, LDAP, local auth
│   ├── cli/                     # Cobra commands powering cmd/snooze
│   ├── components/              # Shared bits used by the input/output binaries
│   ├── condition/               # AST + evaluator for the `["=", "host", "foo"]` DSL
│   ├── config/                  # Two-tier config: file + DB-backed runtime overrides
│   │   └── schema/             # JSON schemas validated at boot
│   ├── core/                    # Boot, supervisor, pipeline, alertprocessor
│   ├── db/                      # Backend abstraction
│   │   ├── mongo/              #  - MongoDB
│   │   ├── postgres/           #  - PostgreSQL (jsonb)
│   │   ├── sqlite/             #  - SQLite (single-writer)
│   │   ├── sql/                # Shared SQL helpers
│   │   ├── dbtest/             # Cross-backend table-test harness
│   │   └── asyncwriter/        # Batched bulk-write coalescer
│   ├── housekeeper/             # TTL / retention worker
│   ├── modification/            # Field mutation engine (set/delete/regex_sub/…)
│   ├── mq/                      # In-process broker (Kombu replacement)
│   ├── plugins/                 # Plugin interfaces, registry, CRUD mounter, cache
│   ├── pluginimpl/              # Concrete plugins (one package each, ~27 total)
│   │   └── all/                # Blank-import aggregator for snooze-server
│   ├── runtime/                 # Process bootstrap shared by every binary
│   ├── syncer/                  # DB-level config sync between cluster members
│   ├── telemetry/               # OpenTelemetry tracer/meter setup
│   ├── timeconstraints/         # Time-window matching (weekdays, dates, periods)
│   └── version/                 # Build-time -ldflags version metadata
├── pkg/                          # Public types used by external tooling
│   ├── snoozeclient/            # Lightweight HTTP client
│   ├── snoozecondition/         # Re-exports of the condition AST
│   └── snoozetypes/             # Alert/Record/User/etc. struct definitions
├── web/                          # Vue 3 SPA (unchanged from 1.x)
├── api/openapi.yaml              # v1 HTTP contract (single source of truth)
├── docs/                         # Sphinx documentation (regenerated for Go)
├── examples/                     # Example configs (one per DB backend)
├── packaging/
│   ├── Dockerfile.golang        # Multi-stage build: web + binaries, distroless runtime
│   ├── helm/                    # Kubernetes chart
│   ├── systemd/                 # Unit files + tmpfiles
│   ├── debian/                  # .deb metadata
│   └── rpm/                     # .rpm spec
├── components/                   # Standalone Python adapters (legacy, OUT of Go scope)
├── docker-compose.yaml           # Mongo / Postgres / SQLite profiles
├── Taskfile.yaml                 # Root task runner (go:*, docker:*, goreleaser:*)
├── .goreleaser.yaml              # Cross-arch release config
├── .golangci.yml                 # Linter config
└── go.mod / go.sum
```

Files to consult first when starting a change:

| File / dir                                    | Why                                                      |
|-----------------------------------------------|----------------------------------------------------------|
| `internal/core/boot.go`, `supervisor.go`      | Boot order, plugin loading, lifecycle                    |
| `internal/api/router.go`                      | Where every HTTP route is registered                     |
| `internal/plugins/plugin.go`                  | The interfaces every plugin satisfies                    |
| `internal/pluginimpl/record/plugin.go`        | A minimal, well-typed plugin example                     |
| `internal/db/db.go` + each backend subpackage | The DB abstraction surface                               |
| `internal/config/load.go`                     | File-config loader (the only place env defaults live)    |
| `pkg/snoozetypes/`                            | Canonical Alert / Record / User shapes                   |
| `api/openapi.yaml`                            | Authoritative HTTP contract — keep this in lockstep      |
| `Taskfile.yaml`                               | Every command an agent should ever need to run           |

---

## Hard rules (non-negotiable)

1. **No secrets in code.** API keys, tokens, LDAP binds, DB DSNs with
   credentials must come from env vars, files at well-known paths, or
   the admin unix socket. `.env.local` is gitignored — use it for dev.
2. **Validate before commit.**
   `task go:vet && task go:test && task go:lint` must be green.
   The user (not you) runs `git commit`.
3. **Never commit on your own.** No `git commit`, no `git push`, no
   `git tag` unless the user explicitly asks. The user will tag
   releases manually. **Do not** add `Co-Authored-By: Claude` (or any
   other agent attribution) to commit messages.
4. **Do not touch `web/`** except for genuine UI bugs the user asks for.
   The Vue codebase is Options API + Vue CLI 4 — match the surrounding
   style if you must edit it.
5. **Do not edit `components/`** as part of Go work. Each `components/<x>/`
   is a standalone Python project published independently; it is *not*
   part of the Go module.
6. **Do not bypass the config schema.** Every config field is declared
   in `internal/config/schema/*.json` and validated at load. Add a
   field there before reading it in code.
7. **Do not invent a fourth DB backend.** Snooze supports exactly three:
   Mongo, Postgres, SQLite (see `<architecture_decisions>` below). The
   abstraction is `internal/db.DB` — go through it, never sneak driver
   calls into plugin code.
8. **No `package_test` cross-imports.** Tests live next to the code they
   exercise (`foo_test.go` in the same package), or in a dedicated
   harness like `internal/db/dbtest/`. Avoid `package foo_test` unless
   you specifically need black-box isolation.

### Permissions

* **Allowed without confirmation**: `git status`, `git diff`, `git log`,
  `task go:*`, `task chart:*`, `go run ./cmd/...` against a throw-away
  config, read-only queries against a local Mongo/Postgres/SQLite.
* **Confirm first**: anything pushing images to `ghcr.io/japannext`,
  changes to `packaging/helm/values.yaml` defaults or `values.schema.json`,
  DB schema changes, modifications under `web/`.
* **Forbidden**: force-pushing to `master`/`release*`, dropping DB
  collections/tables on shared dev hosts, committing `.env.local` or
  `.ca-bundle/`.

---

## How to work

### Plan → Explore → TDD

0. **Clarify** ambiguous asks ("add a plugin" — what kind?
   data-model? notifier? webhook receiver?).
1. **Read first**. For backend changes, start at the entry in the table
   above. For a plugin, copy an existing one of the right shape and
   adapt — don't invent the plumbing from scratch.
2. **TDD is the default** for new features, fixes and refactors.
   Minimal exceptions: typo / docs / config-only edits.
   - RED: write the failing `*_test.go` first (`go test -run …`).
   - GREEN: minimum implementation to pass.
   - REFACTOR: tidy while staying green.
3. Use the `dbtest` harness when a change spans more than one backend.
   Tests parameterised over Mongo+Postgres+SQLite live there.
4. Significant user-facing features go in `docs/` (Sphinx); update
   `api/openapi.yaml` if you touch an HTTP route; add an entry under
   `[Unreleased]` in `CHANGELOG.md`.

### Build & run

```bash
# Toolchain: Go >= 1.25, Task >= 3. Node 18+ to rebuild the web bundle.
task go:build                            # ./bin/snooze-*
task go:test                             # go test -race -shuffle=on -count=1 ./...
task go:lint                             # golangci-lint
task go:vuln                             # govulncheck ./...
task goreleaser:snapshot                 # local cross-arch build
```

Running locally (SQLite, fastest dev loop):

```bash
SNOOZE_DATABASE_TYPE=sqlite \
SNOOZE_DATABASE_PATH=/tmp/snooze.db \
./bin/snooze-server --config examples/default_config.yaml
```

The full clustered stack:

```bash
docker compose --profile mongo    up   # 3× snooze + 3× mongo + nginx
docker compose --profile postgres up
docker compose --profile sqlite   up
```

### Adding a plugin

A plugin is one directory under `internal/pluginimpl/<name>/` with:

```
internal/pluginimpl/<name>/
├── plugin.go        # registers an init() that calls plugins.Register
├── plugin_test.go   # at minimum: round-trip CRUD + Metadata()
└── metadata.yaml    # static UI metadata (embedded via //go:embed)
```

Pick the right interface(s) to implement from `internal/plugins/plugin.go`:

| Interface          | Implement when…                                            |
|--------------------|------------------------------------------------------------|
| `Plugin`           | Always (returns Name, Metadata).                           |
| `DataModel`        | Plugin has CRUD-able records (most do).                    |
| `Processor`        | Plugin transforms or filters alerts in the pipeline.       |
| `Notifier`         | Plugin emits to an external destination (mail, chat, …).   |
| `Action`           | Plugin exposes a user-triggered button in the UI.          |
| `WebhookReceiver`  | Plugin accepts inbound HTTP (Grafana, AlertManager, …).    |
| `RouteProvider`    | Plugin mounts custom routes onto the chi router.           |
| `LifecycleHook`    | Plugin needs Start/Stop hooks (background workers).        |

Then blank-import the new package from `internal/pluginimpl/all/all.go`
so `snooze-server` picks it up at boot. Binaries that don't want every
plugin (the CLI, the component daemons) simply don't import `all`.

### Tests

```bash
go test -short -race -count=1 ./...        # the fast subset
go test -race -shuffle=on -count=1 ./...   # full suite (default in CI)
go test ./internal/pluginimpl/record -run TestRecordCRUD -v
```

Coverage:

```bash
go test -race -coverprofile=cover.out ./...
go tool cover -html=cover.out
```

Use `-short` to skip the docker/Mongo/Postgres-backed integration paths
in `internal/db/*`. They are gated by `testing.Short()`.

### Review checklist

1. `git diff` the staged set.
2. `task go:vet`, `task go:test`, `task go:lint` all green.
3. New code has tests (or an explicit, justified waiver).
4. `api/openapi.yaml` and `docs/` updated if the change is user-visible.
5. `CHANGELOG.md` entry under `[Unreleased]` if user-visible.
6. No new top-level dependency without a note explaining why (`go mod tidy`
   should be the only mutation to `go.sum`).

---

## Architecture decisions

### Three database backends, one abstraction

`internal/db.DB` is the only interface plugins talk to.

| Backend  | Driver / library                               | When operators pick it                                 |
|----------|------------------------------------------------|--------------------------------------------------------|
| MongoDB  | `go.mongodb.org/mongo-driver/mongo`            | Existing 1.x deployments; replica-set clustering.       |
| Postgres | `github.com/jackc/pgx/v5` (+ pgxpool, jsonb)   | Greenfield deploys; SQL operators; CNPG operator.       |
| SQLite   | `modernc.org/sqlite` (pure-Go, no CGO)         | Single-node / appliance deploys. Single-writer.         |

The `asyncwriter` package batches increments to avoid write storms — it
sits between the pipeline and the driver and is the same for all three
backends. `dbtest` parameterises the same test bodies across all three.

### Two-tier config

* **File config** (`internal/config/load.go`): parsed once at boot from
  YAML, validated against the JSON schemas in `internal/config/schema/`.
  This is the "infra" layer — DSN, listen addresses, TLS, logging.
* **Runtime config** (`internal/config/runtime.go`): editable through
  the API and stored in the DB. Pushed to peers via `internal/syncer/`.
  This is the "ops" layer — rules, snoozes, notifications, retention.

A field belongs in exactly one tier. Adding it requires a schema update.

### One process per role; supervisor in `internal/core`

`snooze-server` is the only multi-subsystem binary. Every other binary
(`snooze-syslog`, notifiers, …) is single-purpose, blank-imports only
the packages it needs, and is shipped as its own distroless image.

The supervisor in `internal/core/supervisor.go` runs each subsystem on
a restart-on-panic goroutine, with a context cancel for shutdown.

### Plugin discovery is compile-time, by package init

There is no `entry_points` group, no `dlopen`, no Wasm sandbox. A plugin
is registered when its package's `init()` runs, which happens when
something imports the package. `internal/pluginimpl/all/all.go` is the
canonical blank-import set; binaries opt in by importing it.

### chi over net/http, slog over logrus

* HTTP router: `github.com/go-chi/chi/v5`. Middleware chain in
  `internal/api/middleware/`.
* Logging: `log/slog` with JSON handler in production, text in dev.
  Set via `internal/runtime`. Do not import `logrus` or `zap`.

### What we DON'T do

| Anti-pattern                                        | Why we avoid it |
|------------------------------------------------------|-----------------|
| Resurrect any code from `src/snooze/`                | Deleted in Phase 9. Reference only via `git log`. |
| Use the Python `components/<x>/` adapters from Go    | They're standalone projects on a separate release cadence; Go has its own listeners under `cmd/snooze-<input>/`. |
| Add a fourth DB backend                              | Three backends is already a heavy test matrix. New storage goes through one of them or via a plugin. |
| Couple plugins to a specific DB driver               | Plugins go through `internal/db.DB`; never import `mongo`, `pgx`, or `sqlite` directly from `internal/pluginimpl/`. |
| Write to `time.Now()` directly in plugin core paths  | Use the clock injected on the host so tests can fake time. |
| Skip the JSON schema when adding a config field      | The schema is the contract operators rely on; un-schema'd fields silently get ignored. |
| Use `panic` for recoverable errors                   | Return an `error`. `panic` is reserved for programmer bugs and is caught by the supervisor. |
| Introduce `go-yaml.v2`                               | The repo uses `go-yaml.v3` everywhere. |
| Add a global mutable singleton                       | Pass dependencies through constructors. There is no `var globalDB`. |
| Run `go generate` in CI                              | Anything generated is committed. CI verifies, never regenerates. |

---

## Communication

* Summary first, details after.
* List the absolute paths of changed files.
* Call out non-obvious decisions and why.
* Indicate the next concrete step ("run `task go:test`", "review and commit").
* Don't add agent attribution to commit messages, PR descriptions, or
  inline comments unless asked.
