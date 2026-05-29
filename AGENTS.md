# AGENTS.md

> Read by every agent that touches this repo (Claude, Cursor, Copilot, Codex…).
> Snooze is a **Go codebase** since v2.0.0. Anything you remember about the
> Python 1.x server is stale.

---

## Project at a glance

* **What**: Clustered log-aggregation and alerting server.
* **Repo**: `github.com/snoozeweb/snooze`. Module path matches the import path.
* **Language floor**: Go 1.25 (`go.mod`). Build flags: `-trimpath -tags 'osusergo,netgo'`,
  `CGO_ENABLED=0`. Container images are distroless.
* **Frontend**: React 19 SPA under `web/` (Vite 6, TypeScript strict, TanStack Router + Query, Radix UI primitives, Chart.js wrappers). The Vue 3 codebase was replaced in the M0-M8 rewrite.
* **Versioning**: `git tag vX.Y.Z` on `master`. The user manages all git
  operations — never `commit`, `push`, or `tag` without explicit instruction.

---

## Repo map

```
snooze/
├── cmd/                          # One subdirectory per binary (main package)
│   ├── snooze-server/           # API + UI + pipeline workers (imports pluginimpl/all)
│   ├── snooze/                  # Operator CLI (Cobra)
│   ├── snooze-syslog/           # Input: syslog
│   ├── snooze-snmptrap/         # Input: SNMP traps
│   ├── snooze-relp/             # Input: RELP
│   ├── snooze-smtp/             # Input: SMTP
│   ├── snooze-otlp/             # Input: OTLP/HTTP JSON logs
│   ├── snooze-k8s-events/       # Input: Kubernetes Event API watch
│   ├── snooze-googlechat/       # Output notifier: Google Chat
│   ├── snooze-mattermost/       # Output notifier: Mattermost
│   ├── snooze-teams/            # Output notifier: MS Teams
│   ├── snooze-jira/             # Output notifier: Jira issues
│   ├── snooze-mcp/              # MCP server (exposes Snooze to AI agents)
│   └── snooze-pacemaker/        # Pacemaker HA integration helper
├── internal/                     # Private application packages
│   ├── api/                     # chi router, middleware, REST handlers, admin socket
│   ├── auth/                    # JWT, LDAP, local auth
│   ├── cli/                     # Cobra commands powering cmd/snooze
│   ├── components/              # Daemon bodies behind the cmd/snooze-* input & bidirectional binaries
│   ├── condition/               # AST + evaluator for the `["=", "host", "foo"]` DSL
│   ├── config/                  # Two-tier config: file + DB-backed runtime overrides
│   │   └── schema/             # Per-section Go structs (koanf tags) + Config.Validate()
│   ├── core/                    # Boot, supervisor, pipeline, alertprocessor
│   ├── daemon/                  # Shared entry-point harness for the cmd/snooze-* binaries
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
│   ├── pluginimpl/              # Concrete plugins (40+, one per package; set in all/)
│   │   └── all/                # Blank-import aggregator for snooze-server
│   ├── syncer/                  # DB-level config sync between cluster members
│   ├── telemetry/               # OpenTelemetry tracer/meter setup
│   ├── timeconstraints/         # Time-window matching (weekdays, dates, periods)
│   └── version/                 # Build-time -ldflags version metadata
├── pkg/                          # Public types used by external tooling
│   ├── snoozeclient/            # Lightweight HTTP client
│   └── snoozetypes/             # Alert/Record/User/etc. struct definitions
├── web/                          # React 19 SPA — working rules in web/AGENTS.md
├── api/openapi.yaml              # v1 HTTP contract (single source of truth)
├── docs/                         # Docusaurus site — authoring rules in docs/AGENTS.md (content/ holds the Markdown)
├── examples/                     # Example configs (one per DB backend)
├── packaging/
│   ├── Dockerfile.golang        # Multi-stage build: web + binaries, distroless runtime
│   ├── helm/                    # Kubernetes chart
│   ├── systemd/                 # Unit files + tmpfiles
│   ├── debian/                  # .deb metadata
│   └── rpm/                     # .rpm spec
├── docker-compose.yaml           # Mongo / Postgres / SQLite profiles
├── Taskfile.yaml                 # Root task runner (go:*, docs:*, chart:*, docker:*, goreleaser:*)
├── .goreleaser.yaml              # Cross-arch release config
├── .golangci.yml                 # Linter config
└── go.mod / go.sum
```

Files to consult first when starting a change:

| File / dir                                    | Why                                                      |
|-----------------------------------------------|----------------------------------------------------------|
| `internal/core/boot.go`, `supervisor.go`      | Boot order, plugin loading, lifecycle                    |
| `internal/api/router.go`                      | Every HTTP route, incl. `mountWebhooks()` for inbound integrations |
| `internal/plugins/plugin.go`                  | The interfaces every plugin satisfies                    |
| `internal/pluginimpl/record/plugin.go`        | A minimal, well-typed plugin example                     |
| `internal/pluginimpl/AGENTS.md`               | Plugin-authoring recipe, interface menu, taxonomy        |
| `internal/db/db.go` + each backend subpackage | The `db.Driver` abstraction surface                      |
| `internal/db/AGENTS.md`                       | Driver contract, the three backends, the dbtest plan     |
| `internal/config/load.go`                     | File-config loader (the only place env defaults live)    |
| `internal/config/schema/*.go`                 | Typed file-config sections (koanf) — add fields here     |
| `pkg/snoozetypes/`                            | Canonical Alert / Record / User shapes                   |
| `api/openapi.yaml`                            | Authoritative HTTP contract — keep this in lockstep      |
| `Taskfile.yaml`                               | Every command an agent should ever need to run (`task --list`) |
| `docs/AGENTS.md`                              | How to write & verify docs for any user-facing change    |
| `web/AGENTS.md`                               | SPA layout, generated API client, web check set          |

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
4. **Frontend changes follow the standard PR/CI flow** — see `web/AGENTS.md`
   for the SPA's structure, the generated-client boundary, and the full check
   set (`task web:{lint,typecheck,test,format:check,build}`).
5. **The legacy Python adapters are not in this repo.** They live in the
   sibling `snooze_plugins` repo on their own release cadence; this Go module
   reimplements those inputs/outputs natively under `cmd/snooze-*`. Don't edit
   `snooze_plugins` as part of Go work here. (`internal/components/` is
   unrelated Go code — the daemon bodies behind those binaries.)
6. **Do not bypass the config schema.** Each file-config field is a typed
   Go field in `internal/config/schema/*.go` (`koanf:"…"` tags), wired into
   the aggregate `config.Config` and checked by `Config.Validate()` at boot
   (loaded via knadh/koanf v2). Add it there — with a sane `Default*()` —
   before reading it. koanf silently drops keys with no struct member.
7. **Do not invent a fourth DB backend.** Snooze supports exactly three:
   Mongo, Postgres, SQLite (see **Architecture decisions** below). The
   abstraction is `internal/db.Driver` (plugins obtain it from
   `plugins.Host.DB()`) — go through it, never sneak concrete-driver calls
   into plugin code.
8. **No `package_test` cross-imports.** Tests live next to the code they
   exercise (`foo_test.go` in the same package), or in a dedicated
   harness like `internal/db/dbtest/`. Avoid `package foo_test` unless
   you specifically need black-box isolation.

### Permissions

* **Allowed without confirmation**: `git status`, `git diff`, `git log`,
  `task go:*`, `task chart:*`, `go run ./cmd/...` against a throw-away
  config, read-only queries against a local Mongo/Postgres/SQLite.
* **Confirm first**: anything pushing images to `snoozeweb` on Docker Hub,
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
3. When a change spans more than one backend, test all three. The shared
   `internal/db/dbtest` suite is being adopted for this; today the driver
   tests are still hand-rolled per backend (see `internal/db/AGENTS.md`).
4. **Every user-facing change ships its docs.** A new flag, route, plugin,
   or integration is not "done" until it has a page under `docs/content/`
   — `docs/AGENTS.md` says where each kind goes and how to verify
   (`task docs:build`). Touch an HTTP route → also update `api/openapi.yaml`
   (the contract the API reference renders from). Always add an
   `[Unreleased]` entry to `CHANGELOG.md`.

### Build & run

```bash
# Toolchain: Go >= 1.25, Task >= 3. Node 22+ to rebuild the web bundle.
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

Plugins live one-per-package under `internal/pluginimpl/<name>/` and
self-register in `init()`. Copy the nearest existing plugin of the same shape
(data-model, notifier, webhook receiver, processor) instead of inventing the
plumbing, then blank-import it from `internal/pluginimpl/all/all.go`.

**The full recipe is in `internal/pluginimpl/AGENTS.md`** — directory layout,
the `plugins.Register` call, the interface menu
(`Plugin`/`DataModel`/`Notifier`/`WebhookReceiver`/… plus the optional `*Hook`
refinements), `metadata.yaml`, the test floor, and where to register and
document it.

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

`internal/db.Driver` is the storage contract; plugins reach it through
`plugins.Host.DB()`, never the concrete driver packages.

| Backend  | Driver / library                               | When operators pick it                                 |
|----------|------------------------------------------------|--------------------------------------------------------|
| MongoDB  | `go.mongodb.org/mongo-driver/mongo`            | Existing 1.x deployments; replica-set clustering.       |
| Postgres | `github.com/jackc/pgx/v5` (+ pgxpool, jsonb)   | Greenfield deploys; SQL operators; CNPG operator.       |
| SQLite   | `modernc.org/sqlite` (pure-Go, no CGO)         | Single-node / appliance deploys. Single-writer.         |

The `asyncwriter` package batches increments to avoid write storms — it
sits between the pipeline and the driver and is the same for all three
backends. `internal/db/dbtest` holds a shared driver-test suite
(`RunDriverSuite`) meant to run identically across all three; adoption is
in progress (see `internal/db/AGENTS.md`).

### Two-tier config

* **File config** (`internal/config/load.go`): parsed once at boot from
  YAML (koanf), unmarshalled into the typed structs in
  `internal/config/schema/` and checked by `Config.Validate()`.
  This is the "infra" layer — DSN, listen addresses, TLS, logging.
* **Runtime config** (`internal/config/runtime.go`): editable through
  the API and stored in the DB. Pushed to peers via `internal/syncer/`.
  This is the "ops" layer — rules, snoozes, notifications, retention.

A field belongs in exactly one tier. Adding it requires a schema update.

### Inbound integrations: webhook receivers + opt-in ingest auth

Monitoring systems push alerts in over HTTP. Any plugin implementing
`WebhookReceiver` is auto-mounted under `/api/v1/webhook/{name}` by
`mountWebhooks()` in `internal/api/router.go` (Grafana, AlertManager,
Datadog, CloudWatch/SNS, Sentry, New Relic, Azure Monitor, Prometheus,
InfluxDB2, Kapacitor, heartbeat). Receivers are **unauthenticated by
default** (1.5.0 parity). The `ingest` section
(`internal/config/schema/ingest.go`) adds opt-in, defense-in-depth
hardening: a shared `Authorization: Bearer`/`?token=`, AWS SNS signature
verification, and Sentry HMAC. Network isolation stays the baseline; these
are belt-and-braces. New integration → new plugin (interface above) +
`docs/content/general/integrations/<name>.md`.

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
* Logging: `log/slog`. The auxiliary daemons use a text handler on stderr via
  `internal/daemon.NewLogger`; `snooze-server` configures its own handler at
  boot. Do not import `logrus` or `zap`.

### What we DON'T do

| Anti-pattern                                        | Why we avoid it |
|------------------------------------------------------|-----------------|
| Resurrect any code from `src/snooze/`                | Deleted in Phase 9. Reference only via `git log`. |
| Pull in the legacy Python adapters                   | They live in the sibling `snooze_plugins` repo on a separate cadence; this module has native Go listeners under `cmd/snooze-*`. |
| Add a fourth DB backend                              | Three backends is already a heavy test matrix. New storage goes through one of them or via a plugin. |
| Couple plugins to a specific DB driver               | Plugins use the `db.Driver` from `host.DB()`; production plugin code never imports `internal/db/{mongo,postgres,sqlite}` (a plugin's `_test.go` may, to spin up a real backend). |
| Write to `time.Now()` directly in plugin core paths  | Use the clock injected on the host so tests can fake time. |
| Skip the schema struct when adding a config field    | The `internal/config/schema` structs are the operator-facing contract; koanf silently drops fields with no struct member. |
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
