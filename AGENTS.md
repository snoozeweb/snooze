# AGENTS.md

> Read by every agent that touches this repo (Claude, Cursor, Copilot, Codex…).
> Snooze is a **Go codebase**. The Python 1.x server is not in this repo.

---

## Project at a glance

* **What**: Clustered log-aggregation and alerting server.
* **Repo**: `github.com/snoozeweb/snooze`. Module path matches the import path.
* **Language floor**: Go 1.25 (`go.mod`). Build flags: `-trimpath -tags 'osusergo,netgo'`,
  `CGO_ENABLED=0`. Container images are distroless.
* **Frontend**: React 19 SPA under `web/` (Vite 6, TypeScript strict, TanStack Router + Query, Radix UI primitives, Chart.js wrappers).
* **Versioning**: `git tag vX.Y.Z` on `main` (the default branch). Pushing a `v*`
  tag triggers the GoReleaser release workflow (see **Releasing** under **How to
  work**). The user manages all git operations — never `commit`, `push`, or `tag`
  without explicit instruction.

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
│   ├── snooze-googlechat/       # Bridge (in+out): Google Chat (STUB — rewrite in progress; only `version` works)
│   ├── snooze-mattermost/       # Bridge (in+out): Mattermost slash-commands ⇄ records
│   ├── snooze-teams/            # Bridge (in+out): MS Teams (Graph poll + /alert listener)
│   ├── snooze-jira/             # Bridge (in+out): Jira webhook + bidirectional poller
│   ├── snooze-mcp/              # MCP server (exposes Snooze to AI agents)
│   └── snooze-pacemaker/        # Pacemaker HA integration helper (one-shot, no Run loop)
├── internal/                     # Private application packages
│   ├── api/                     # chi router, middleware, REST handlers, admin socket
│   ├── auth/                    # Pluggable providers (local/LDAP/anon), JWT, RBAC resolver, refresh-token store
│   ├── cli/                     # Cobra commands powering cmd/snooze
│   ├── components/              # Daemon bodies behind the cmd/snooze-* input & bidirectional binaries
│   ├── condition/               # AST + evaluator + string query DSL (legacy-list, object, frontend & `host = foo AND …` shapes)
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
│   ├── mq/                      # Message queue: inproc / Postgres / Mongo bus, picked by DB type (see note)
│   ├── plugins/                 # Plugin interfaces, registry, CRUD mounter, cache
│   ├── pluginimpl/              # Concrete plugins (40+, one per package; set in all/)
│   │   └── all/                # Blank-import aggregator for snooze-server
│   ├── syncer/                  # DB-level config sync between cluster members
│   ├── telemetry/               # OpenTelemetry tracing + Prometheus metrics registry (no OTel meter)
│   ├── timeconstraints/         # Time-window matching (weekdays, dates, periods)
│   └── version/                 # Build-time -ldflags version metadata
├── pkg/                          # Public types used by external tooling
│   ├── snoozeclient/            # Lightweight HTTP client
│   └── snoozetypes/             # Alert/Record/User/etc. struct definitions
├── web/                          # React 19 SPA — working rules in web/AGENTS.md
├── api/openapi.yaml              # v1 HTTP contract (single source of truth)
├── docs/                         # Docusaurus site — authoring rules in docs/AGENTS.md (content/ holds the Markdown)
├── examples/                     # Example DB configs (legacy 1.x single-file layout; `--config` wants a directory of section files)
├── packaging/
│   ├── Dockerfile.golang        # Multi-stage build: web + binaries, distroless runtime
│   ├── files/                   # 1.x-style example config; partly stale vs the Go schema (the rpm/deb ship an EMPTY /etc/snooze/server instead — see Releasing)
│   ├── helm/                    # Kubernetes chart
│   ├── systemd/                 # Unit files (one per binary) + README
│   ├── nfpm/                    # deb/rpm postinstall/postremove scriptlets (consumed by GoReleaser nfpm)
│   ├── debian/                  # LEGACY Python .deb metadata (dpkg-buildpackage) — the Go deb/rpm come from GoReleaser/nfpm, NOT this
│   └── rpm/                     # LEGACY Python .rpm spec — see above
├── scripts/                      # render-deploy.sh and other dev/release helpers
├── CHANGELOG.md                  # Keep-a-Changelog; add an [Unreleased] entry per user-visible change
├── docker-compose.yaml           # Mongo / Postgres / SQLite profiles
├── Taskfile.yaml                 # Root task runner (go:*, web:*, docs:*, chart:*, docker:build, goreleaser:*, render:deploy)
├── .goreleaser.yaml              # GoReleaser: cross-arch binaries + per-binary tar.gz + deb/rpm (nfpm) + checksums
├── .github/workflows/            # CI: go-tests (lint/test/build), docs (Pages deploy from main), release (v* tag → GoReleaser), docker (v* tag → Docker Hub)
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
| `internal/config/load.go`                     | File-config loader: directory of section YAMLs + `SNOOZE_SERVER_*` env overrides |
| `internal/config/schema/*.go`                 | Typed file-config sections (koanf) — add fields here     |
| `internal/daemon/daemon.go`                   | Entry-point harness every `cmd/snooze-*` aux binary wires through (`daemon.Main`) |
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
* **Forbidden**: force-pushing to `main`/`release*`, dropping DB
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
3. When a change spans more than one backend, test all three. A shared
   `internal/db/dbtest` holds a shared driver suite (`RunDriverSuite`), now
   wired into every backend's `driver_test.go` via `TestDriverSuite`; some
   known cross-backend divergences remain (see `internal/db/AGENTS.md`).
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
# `--config` takes a DIRECTORY of section files (core.yaml, general.yaml, …),
# NOT a single file — default /etc/snooze/server-go. A missing/empty dir just
# means "defaults + env". File-config env overrides use the prefix
# SNOOZE_SERVER_<SECTION>_<KEY>; `file` is the legacy alias for SQLite (default).
mkdir -p /tmp/snooze-conf
SNOOZE_SERVER_CORE_DATABASE_TYPE=file \
SNOOZE_SERVER_CORE_DATABASE_PATH=/tmp/snooze.db \
./bin/snooze-server --config /tmp/snooze-conf
```

> Note: `examples/*.yaml` are the legacy 1.x single-file layout (a top-level
> `database:`/`api:` block); they are **not** picked up by `--config`, which
> only reads the section basenames in `internal/config/load.go`'s `sectionFiles`.

The full clustered stack:

```bash
docker compose --profile mongo    up   # 3× snooze + 3× mongo + nginx
docker compose --profile postgres up
docker compose --profile sqlite   up
```

### Releasing

Releases are GoReleaser-driven and **tag-triggered**. The user tags; you don't
(hard rule 3).

1. Land the change set on `main`; move the `CHANGELOG.md` `[Unreleased]` heading
   to `## vX.Y.Z`.
2. The user pushes an annotated `vX.Y.Z` tag (keep the `v` prefix — the historical
   `2.0.0` tag has none and never produced a release). That fires
   `.github/workflows/release.yml`: a `go vet` + race-test gate → `npm run build` of
   the web bundle → `goreleaser release`.
3. GoReleaser publishes a GitHub Release with per-binary `tar.gz` archives, a **deb
   and rpm for linux amd64+arm64**, and `checksums.txt`. The release body is the
   curated `CHANGELOG.md` section for the tag (via `--release-notes`), not
   GoReleaser's commit log.

The deb/rpm ship every binary in `/usr/bin`, the React bundle in
`/var/lib/snooze/web` (the server's `--web-dir` default — the SPA is **not**
embedded, so the bundle is built before GoReleaser runs), all systemd units, and
an **empty** `/etc/snooze/server` (the server boots on defaults + SQLite; the 1.x
example configs in `packaging/files/` are deliberately not shipped — keys like
`ssl.enabled: true` with no certs fail the Go validator). `packaging/nfpm/`
holds the install scriptlets (create the `snooze` user + state dirs).

On the same `v*` tag, `docker.yml` builds the `snooze-server` image from
`packaging/Dockerfile.golang` (target `runtime-server`) and pushes
`snoozeweb/snooze:<version>` + `:latest` to Docker Hub — amd64, server-only
(matching 1.x and 2.0.0). Needs the `DOCKERHUB_USERNAME`/`DOCKERHUB_TOKEN` repo
secrets; can also be run via `workflow_dispatch`.

Gotchas (learned cutting v2.1.0):
- GoReleaser aborts on a dirty tree — anything the workflow generates (e.g. the
  extracted release notes) must live **outside** the work tree, and there is no
  `before: go mod tidy` hook (it mutates `go.mod`).
- `internal/db/postgres` `TestListenNotifyRoundTrip` is flaky under CI load (a 5s
  `pg_notify` round-trip timeout); if the release test gate fails *only* there,
  re-run the job — it's not a regression.

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
| MongoDB  | `go.mongodb.org/mongo-driver/v2/mongo`         | Existing 1.x deployments; replica-set clustering.       |
| Postgres | `github.com/jackc/pgx/v5` (+ pgxpool, jsonb)   | Greenfield deploys; SQL operators; CNPG operator.       |
| SQLite   | `modernc.org/sqlite` (pure-Go, no CGO)         | Single-node / appliance deploys. Single-writer.         |

The default backend is **SQLite**, selected by `type: file` (the legacy 1.x
alias) or `type: sqlite`; `mongo`/`mongodb` and `postgres`/`pg`/`postgresql`
are the other accepted spellings, or set `DATABASE_URL`. The selector is
`core.database.type`.

The `asyncwriter` package batches increments to avoid write storms — it
sits between the pipeline and the driver and is the same for all three
backends. `internal/db/dbtest` holds a shared driver-test suite
(`RunDriverSuite`) meant to run identically across all three; it is wired
into every backend's `driver_test.go` via `TestDriverSuite` (see
`internal/db/AGENTS.md`).

### Two-tier config

* **File config** (`internal/config/load.go`): parsed once at boot from
  YAML (koanf), unmarshalled into the typed structs in
  `internal/config/schema/` and checked by `Config.Validate()`.
  This is the "infra" layer — DSN, listen addresses, TLS, logging.
* **Runtime config** (`internal/config/runtime.go`): editable through
  the API and stored in the DB. Pushed to peers via `internal/syncer/`.
  This is the "ops" layer — rules, snoozes, notifications, retention.

A field belongs in exactly one tier. Adding it requires a schema update.

File config is read from a **directory** of per-section YAML files — one
basename per section (`core.yaml`, `general.yaml`, `housekeeping.yaml`,
`notification.yaml`, `ldap_auth.yaml`, `web.yaml`, `auth.yaml`, `syncer.yaml`)
— and every field can be overridden by an env var named
`SNOOZE_SERVER_<SECTION>_<KEY>` (e.g. `SNOOZE_SERVER_CORE_DATABASE_TYPE`); the
legacy `DATABASE_URL` shortcut is also honoured. A bare `SNOOZE_DATABASE_*`
(no `SNOOZE_SERVER_` prefix) is silently ignored. A new **list-valued** field
must be added to `isListField` in `load.go` so its env value comma-splits.

### Inbound integrations: webhook receivers + opt-in ingest auth

Monitoring systems push alerts in over HTTP. Any plugin implementing
`WebhookReceiver` is auto-mounted under `/api/v1/webhook/{name}` by
`mountWebhooks()` in `internal/api/router.go` (Grafana, AlertManager,
Datadog, CloudWatch, Sentry, New Relic, Azure Monitor, Prometheus,
InfluxDB2, Kapacitor, heartbeat — the 11 `WebhookReceiver`s; `sns` is an
*outbound* notifier, not a receiver). Receivers are **unauthenticated by
default**. The `ingest` section
(`internal/config/schema/ingest.go`) adds opt-in, defense-in-depth
hardening: a shared `Authorization: Bearer`/`?token=`, AWS SNS signature
verification, and Sentry HMAC. Network isolation stays the baseline; these
are belt-and-braces. New integration → new plugin (interface above) +
`docs/content/general/integrations/<name>.md`.

### One process per role; supervisor in `internal/core`

`snooze-server` is the only multi-subsystem binary. Every other binary
(`snooze-syslog`, notifiers, …) is single-purpose, blank-imports only
the packages it needs, and is shipped as its own distroless image.

The supervisor in `internal/core/supervisor.go` runs each subsystem on a
restart-on-panic goroutine behind a backoff policy (default 3 tries / 60s
window, 1s doubling to 30s). Panics are recovered, logged with a stack, and
counted as a failure. A **non-critical** subsystem that exhausts its retries
gives up silently and the server keeps running; a **critical** one propagates
and cancels the whole errgroup, taking the process down. Today only
`asyncwriter` is critical — housekeeper, syncer and the node-heartbeat are not.

### Auxiliary daemons share an entry-point harness

Every `cmd/snooze-*` input/bridge binary (but **not** `snooze-server` or the
`snooze` CLI) wires through `internal/daemon`: implement
`LoadConfig(path) → Config`, a `New(cfg, logger) → (*Daemon, error)`
constructor, and `Daemon.Run(ctx) error`, then call
`daemon.Main(daemon.Config{Name, DefaultConfig, Build, Subcommands})` from
`main`. The harness owns the `version` subcommand, the `-c`/`-debug` flags,
signal-driven shutdown, and fixed exit codes (0 clean incl. `context.Canceled`,
2 usage, 1 runtime). One-shot binaries with no run loop (`snooze-pacemaker`,
the `snooze-googlechat` stub) call `daemon.HandleVersion` and parse their own
flags instead. Three daemons honour a `SNOOZE_<NAME>_CONFIG` env override of
the config path via `daemon.EnvOr` (relp, mattermost, pacemaker). Logs go to
stderr through `daemon.NewLogger` (text handler, debug-gated level). A
constructor-naming wart persists: most packages export `New`, but `smtp` and
`snmptrap` export `NewDaemon` for the same shape.

### First-boot seeding writes secrets and a root password to the DB

On an empty database `internal/core` seeds: an HS256 JWT signing key and a
reload token (64/32 random bytes via `crypto/rand`) in the `secrets`
collection; a bcrypt `root` user whose generated password is logged **once at
WARN to stderr**; the default roles (admin / viewer / notifications) and a
default "Host and Message" aggregate rule — all guarded by an `init_db` marker
doc. The JWT key comes from the DB by default (the seeded HS256 key); if
`auth.token_secret` is set in the file config (or `SNOOZE_SERVER_AUTH_TOKEN_SECRET`),
`bootSecrets` overrides the DB key with it — it must be ≥ `auth.MinSecretBytes`
(32) bytes or boot fails.

### Two buses, easily confused

`*Core.MQ()` returns the **message queue** (`internal/mq`), whose backend
tracks the DB type: `inproc` (buffered channels, used for the SQLite/file
default), `pg` (LISTEN/NOTIFY + an `mq_messages` table) or `mongo`
(change-streams + an `mq_messages` collection), chosen by `mqKindForDatabase`.
`plugins.Host.Bus()` returns something different — the per-driver **syncer**
change-feed (`Driver.Watcher()`) used to propagate config reloads. Plugins get
the syncer bus, never the mq bus. (As of now the mq bus has **no production
publisher or subscriber** — it is built at boot but carries no traffic, and on
pg/mongo it dials a second connection and runs `mq_messages` DDL for nothing.
Treat it as not-yet-wired infrastructure.)

### Plugin discovery is compile-time, by package init

There is no `entry_points` group, no `dlopen`, no Wasm sandbox. A plugin
is registered when its package's `init()` runs, which happens when
something imports the package. `internal/pluginimpl/all/all.go` is the
canonical blank-import set; binaries opt in by importing it.

A registered plugin can still be **disabled by default**: `optionalPlugins`
in `internal/core/boot.go` (currently just `patlite`) is dropped from the live
plugin map — hidden from `/metadata`, the CRUD router, and the notification
dispatcher — unless the operator lists it in `core.enabled_optional_plugins`.
So the ~45 blank-imported plugins are not all served by a default install.

### chi over net/http, slog over logrus

* HTTP router: `github.com/go-chi/chi/v5`. Middleware chain in
  `internal/api/middleware/`.
* Logging: `log/slog`. The auxiliary daemons use a text handler on stderr via
  `internal/daemon.NewLogger`; `snooze-server` configures its own handler at
  boot. Do not import `logrus` or `zap`.

### What we DON'T do

| Anti-pattern                                        | Why we avoid it |
|------------------------------------------------------|-----------------|
| Resurrect any code from `src/snooze/`                | It's deleted — reference only via `git log`. |
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
