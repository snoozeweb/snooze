# AGENTS.md

> This file is read by all agents (Claude, Cursor, Copilot, Codex…).
> XML format for better LLM parsing.

---

<project_overview>

## Project Overview

**Project**: Snooze (snooze-server) — https://snoozeweb.net

**Objective**: Monitoring tool for log aggregation and alerting. Ingests alerts from many input sources, applies aggregation/rule/snooze/notification pipelines, and ships them to chat/email/webhook destinations. Ships with a clustered Python backend, a Vue web UI, and an extensible plugin system.

**Context**: Team (japannext + original authors) | Backend service + SPA web UI + auxiliary input/output components | Python 3.8 (Falcon + MongoDB + Pydantic v1) backend / Vue 3 (CoreUI) frontend / Docker + Helm packaging

**Current Status**: Stable, version `1.6.4` (see `pyproject.toml` and `packaging/helm/Chart.yaml`). Recent focus per `CHANGELOG.md`: removed in-process clustering in favour of DB-level sync (v1.6.0), OpenTelemetry instrumentation, Grafana v8.5+ + AlertManager webhook support, deprecation cleanup.

**Versioning**:
- Server version: `pyproject.toml` (`[project] version`)
- Helm chart version: `packaging/helm/Chart.yaml`
- Component versions: each `components/*/pyproject.toml`
- Tags: `vX.Y.Z` on the server; release CI publishes to PyPI + GHCR
- Git operations are managed manually by the user (do not commit/push/tag without explicit instruction)

**Related Repos / External**: none vendored; each `components/*` is a self-contained Python project published independently.

</project_overview>

---

<hard_rules>

## Hard Rules

> These rules are NON-NEGOTIABLE. Apply them SYSTEMATICALLY at every interaction.

1. **No secrets in code**: Never commit API keys, tokens, passwords, MongoDB connection strings with credentials, or LDAP bind passwords. Use environment variables or `.env.local` (gitignored).
2. **Validate before commit**: `task py:lint` + `task py:test` (backend) and `npm run lint` + `npm run test:unit` (web) must pass before any commit is proposed.
3. **Do not commit git operations on your own**: The user manages `git commit`, `git push`, tags, and releases manually. Propose diffs, then wait.
   - When you are about to commit, **do not add `Co-Authored-By: Claude` (or any agent attribution) to the commit message**. Commits should look like the user authored them.
4. **Respect the `src/` layout**: Python source lives in `src/snooze/`. (An older layout placed it at top-level `snooze/`; that directory was removed during the 3.13 migration. If it ever reappears, it is stale — never edit it.)
5. **Pydantic v1 only**: The project pins `pydantic>=1.10.18,<2`. Do not write v2-only syntax (`model_config`, `field_validator`, etc.). Use `Config` inner class, `validator`, `Extra.forbid`, etc. v1.10.18+ is required for Python 3.13.
6. **Python 3.13 floor**: `requires-python = ">=3.13"`. `match` statements, PEP 604 `X | Y` unions, and PEP 585 built-in generics (`dict[str, int]`, `list[T]`) are all available — feel free to use them in new code. (Older modules still use `typing.Dict` / `Optional[X]`; don't churn untouched files.)
7. **Plugin discovery is directory-based**: Adding a plugin means adding a directory under `src/snooze/plugins/core/<name>/` with a `plugin.py`. External plugins register via the `snooze.plugins.core` entry-point group (loaded with `importlib.metadata.entry_points`, not `pkg_resources`).
8. **Token efficiency for file operations**:
   - Use `cp` to duplicate files rather than reading + regenerating.
   - Use `sed` / Edit for small substitutions rather than full-file rewrites.

### Permissions / Security

- **Allowed without confirmation**: `git status`, `git diff`, `git log`, `task py:lint`, `task py:test`, `npm run lint`, `npm run test:unit`, `uv sync`, read-only Mongo queries against a local test instance.
- **Require confirmation**: `task docker:release`, `task docker:develop`, anything pushing to GHCR or PyPI, modifying `packaging/helm/values.yaml` defaults, schema-breaking DB migrations.
- **Forbidden**: Force-pushing to `master`/`release*`, editing the stale top-level `snooze/` directory, dropping or renaming MongoDB collections in production-like configs, committing `.env.local` or `.ca-bundle/`.

</hard_rules>

---

<repo_structure>

## Repo Structure

### Directory Map

```
snooze/
├── src/snooze/                # Python backend source (ACTUAL location)
│   ├── __main__.py           # `snooze-server` entry point
│   ├── core.py               # Core class — orchestrates plugins, DB, threads
│   ├── api/                  # Falcon WSGI app, middleware, routes
│   ├── cli/                  # `snooze` CLI (login, root_token, record, snooze)
│   ├── db/                   # DB abstraction (mongo/ + file/ backends)
│   ├── plugins/core/         # ~27 plugins (record, rule, snooze, mail, webhook…)
│   ├── utils/                # config, threading, housekeeper, mq, parser, condition
│   ├── alerta/               # Alerta integration
│   ├── defaults/             # Default YAML configs (logging, auth)
│   ├── token.py              # JWT auth
│   ├── tracing.py            # OpenTelemetry setup
│   └── logging.py            # Logging configuration
├── tests/                     # pytest suite (test_api, test_audit, test_auth, test_core, test_token)
│   └── conftest.py           # mongomock fixtures, Falcon TestClient, JWT root token
├── web/                       # Vue 3 SPA (frontend)
│   ├── src/
│   │   ├── views/            # Route pages (Record, Rule, Snooze, Dashboard…)
│   │   ├── components/       # SDataTable, List, Card, charts, forms
│   │   ├── containers/       # DefaultLayout
│   │   ├── router/           # Vue Router (hash mode)
│   │   ├── store.js          # Vuex root (UI-only state)
│   │   ├── api.js            # Axios instance + JWT/401 interceptors
│   │   ├── _nav.js           # Sidebar menu config
│   │   └── utils/api.js      # get_data() with localStorage schema cache
│   ├── public/
│   ├── package.json
│   ├── Taskfile.yaml
│   └── .env.development.local  # `VUE_APP_API=http://localhost:5200/api`
├── components/                # Independent Python packages (input/output adapters)
│   ├── client/               # Shared CLI helper used by other components
│   ├── syslog/, snmptrap/, smtp/, relp/   # Input adapters
│   ├── googlechat/, mattermost/, teams/    # Output adapters
│   └── pacemaker/            # Pacemaker cluster helper
├── packaging/                 # Distribution
│   ├── Dockerfile, Dockerfile-render
│   ├── debian/, rpm/         # Native packages
│   └── helm/                 # Kubernetes chart (v1.6.4, values.schema.json)
├── docs/                      # Sphinx documentation source
│   ├── conf.py, index.rst
│   ├── getting_started/, general/, configuration/
│   └── _build/, _static/
├── examples/                  # Example configs / payloads
├── tasks/                     # Invoke-style task scripts
├── .github/workflows/        # tests.yml, build.yml, release.yml
├── docker-compose.yaml       # 3-node clustered dev setup (nginx + 3× snooze + 3× mongo)
├── nginx.conf                # LB config for compose
├── Taskfile.yaml             # Root task runner (delegates to web/, components/, helm/)
├── pyproject.toml            # uv + hatchling
├── mise.toml                 # python=3.8, node=14, ruff, uv, task pins
├── CHANGELOG.md, README.md, ROADMAP.md, CONTRIBUTE.md
└── AGENTS.md                 # ← this file
```

### Key Files

| File | Description | When to consult |
|------|-------------|-----------------|
| `AGENTS.md` | Agent instructions | Start of each session |
| `src/snooze/core.py` | Plugin loader, threading, init sequence | Before changing plugin lifecycle or boot order |
| `src/snooze/api/__init__.py` | Falcon app, middleware, route registration | Before adding/removing HTTP endpoints |
| `src/snooze/plugins/core/<plugin>/plugin.py` | Plugin behaviour + metadata.yaml | Adding/modifying a plugin |
| `tests/conftest.py` | Test fixtures (mongomock, client, core, config) | Before writing new tests |
| `pyproject.toml` | Deps, Pydantic v1 pin, ruff/pyright/pylint config | Dependency or tooling changes |
| `Taskfile.yaml` (root + web + components) | Build/test/lint commands | Running anything |
| `packaging/helm/values.yaml` + `values.schema.json` | Production deploy surface | Helm/k8s changes |
| `docs/index.rst` | Public documentation tree | Documenting user-facing features |
| `CHANGELOG.md` | Release history + recent focus | Understanding context of recent changes |

</repo_structure>

---

<how_to_work>

## How to Work

### Plan

0. **Clarify** — Ask if the request is ambiguous (e.g. "add a plugin" — which kind: data model? processor? notification?).
1. **Explore** — Read the relevant code before planning. For backend, start at `src/snooze/core.py` and the relevant plugin under `src/snooze/plugins/core/`. For web, start at the relevant view in `web/src/views/` and trace through the components it imports.
2. **Define phases** — Each phase ships with tests (pytest for backend, jest for web) and a "how I verified it" step.
3. **Significant features** go in `docs/` (Sphinx) if user-facing; technical notes can live in code docstrings or commit messages. There is no separate `documentation/features/active/` folder in this repo — keep planning in the chat / PR description.

### Build & Run

**Backend** (from repo root):
```bash
uv sync                                  # Install Python deps
PYTHONPATH=src uv run snooze-server      # Run the server (dev)
PYTHONPATH=src uv run snooze --help      # CLI client
```

**Web** (from `web/`):
```bash
npm ci                  # Install (uses package-lock.json)
npm run serve           # Dev server with HMR — expects backend on :5200
npm run build           # Production bundle into web/dist/
```

**Clustered local stack** (Docker):
```bash
docker compose up       # 3× snooze + 3× mongo + nginx LB on :80
```

**Dependencies**:
- Python: edit `pyproject.toml` → `uv lock` → commit both `pyproject.toml` and `uv.lock`.
- Node: `npm install <pkg> --save[-dev]` in `web/` → commit `package.json` + `package-lock.json`.

### Build Feature — TDD Workflow

**TDD is required** for new features, bug fixes, and refactors. Minimal exceptions: typo fixes, docs-only edits, config-only edits.

```
1. RED      → Write a failing test first
2. GREEN    → Minimal implementation to pass
3. REFACTOR → Clean up while keeping green
```

- Backend tests go in `tests/test_<area>.py`. Use the existing `conftest.py` fixtures (`client`, `core`, `db`, `config`).
- Backend tests use **mongomock** — no real MongoDB needed. Inject fixture data via the `pytest-data` plugin (`data` class attribute or `get_data(request, 'data')`).
- Web unit tests use Jest (`vue-cli-service test:unit`). E2E uses Nightwatch (`test:e2e`).

### Run & Test

```bash
# Backend lint
task py:lint                                # ruff check

# Backend tests (all)
task py:test                                # = PYTHONPATH=src uv run pytest

# Backend tests (single file)
PYTHONPATH=src uv run pytest tests/test_api.py

# Backend tests (single test)
PYTHONPATH=src uv run pytest tests/test_api.py::TestAlertRoute::test_post_alert

# Type check (strict mode)
uv run pyright src/snooze

# Security scan
uv run bandit -r src/snooze

# Web (from web/)
npm run lint
npm run test:unit
npm run test:e2e          # requires running stack
npm run test:coverage
```

**Testing strategy**:
- Unit tests for plugin logic, condition parser, modification engine.
- API tests via Falcon `TestClient` (see `tests/test_api.py`).
- E2E for the web stack via Nightwatch.
- Coverage is captured by `pytest-cov` / `--coverage`; no enforced threshold today.
- Manual / browser testing is a supplement, not a replacement.

### Service Logs (Development)

> The repo does NOT ship a `scripts/run-service.sh` helper. Use the patterns below directly, or `docker compose logs -f <service>` when running the compose stack.

```bash
# Real-time capture
PYTHONUNBUFFERED=1 PYTHONPATH=src uv run snooze-server 2>&1 | tee logs/snooze.log

# Web dev server
(cd web && npm run serve) 2>&1 | tee logs/web.log

# Docker compose
docker compose logs -f snooze1
```

If you create a `logs/` directory, gitignore it locally — it is not currently in `.gitignore`.

### Debug & Analyse

**Investigation process**:
1. Reproduce against a minimal config (`PYTHONPATH=src uv run snooze-server` with a fresh `mongomock`-style fixture, or `docker compose up mongo1 snooze1`).
2. Check the running server's stdout/JSON logs. Snooze uses `python-json-logger`; OpenTelemetry traces ship via OTLP if `OTEL_EXPORTER_OTLP_ENDPOINT` is set.
3. For DB issues: connect to the mongo replica set (`mongo1:27017,mongo2:27017,mongo3:27017` in compose) and inspect the `snooze` database.
4. For plugin issues: each plugin has its own logger named after the plugin; enable `DEBUG` in `defaults/logging.yaml`.
5. Isolate, fix, add a regression test.

**Known landmines** — see `<architecture_decisions>` and `<code_style>` below.

### Review

1. `git diff` the staged changes.
2. Verify tests:
   - All `task py:test` green.
   - New tests added for new code (or explicit waiver if test infra cannot reach it).
   - `task py:lint` clean.
   - `pyright` clean for changed files (project is `strict` mode).
3. Self-review:
   - [ ] No edits made to the stale top-level `snooze/` directory.
   - [ ] Pydantic v1 syntax only.
   - [ ] No Python 3.9+ syntax sneaking in.
   - [ ] No secrets in YAML/JSON test fixtures.
   - [ ] Docstrings updated on public functions changed.
4. Update `docs/` (Sphinx) if a user-facing config/option changed.
5. Update `CHANGELOG.md` (top of file) for anything user-visible.

### Git Workflow

Git operations (commit, push, tag, release) are **manual**. CI on tags publishes to PyPI + GHCR — never tag without user instruction.

**Deployment**:
- Helm chart: `packaging/helm/` (values schema enforced).
- Docker images: `ghcr.io/japannext/snooze-server:<version>` (built by `task docker:release`).
- Native: `.rpm` / `.deb` from `packaging/rpm/` and `packaging/debian/`.

</how_to_work>

---

<communication>

## Communication

### When to ask for confirmation

**Always ask BEFORE**:
- Touching anything in `packaging/helm/values.yaml` or `values.schema.json` defaults.
- Changing the plugin loader, the API middleware order, or the threading model in `core.py`.
- Pulling in a new top-level dependency (especially anything pinned by version — Pydantic, pymongo, falcon, click).
- Adding a database migration or changing a Mongo schema field name.
- Anything that touches the JWT auth chain.

**Execute directly**:
- Bug fixes with a clear reproduction.
- Adding tests / refactoring tests.
- Web UI tweaks confined to a single component.
- Docs updates (`docs/`, `CHANGELOG.md`).
- Adding type hints / cleaning up lint warnings.

### Communication format

- Summary first, details after.
- List changed files (paths from repo root).
- Call out non-obvious decisions and why.
- Indicate next manual step (e.g. "run `task py:test`", "review and commit").

</communication>

---

<code_style>

## Code Style Guidelines

### Python (backend)

- **Version floor**: 3.13.
- **Naming**: `snake_case` functions/vars, `PascalCase` classes, `UPPER_SNAKE` constants.
- **Indentation**: 4 spaces.
- **Line length**: 120 chars (pylint config).
- **Type hints**: used pervasively; pyright is in strict mode.
- **Docstrings**: triple-quoted, both `'''` and `"""` appear; one-line summary on the first line.
- **Pydantic v1 patterns**:
  ```python
  class MyConfig(BaseModel):
      field: str
      class Config:
          extra = Extra.forbid
  ```
- **Imports**: stdlib → third-party → local, separated by blank line.

### JavaScript / Vue (frontend)

- **Vue 3, Options API** (`data()`, `methods`, `computed`, `mounted`, `watch`). Do NOT introduce `<script setup>` / Composition API piecemeal — match surrounding code.
- **Naming**: `camelCase` for vars/methods, `PascalCase` for components; `S*`-prefixed components are the shared/generic table/form primitives (SDataTable, SFormInput, SPagination, etc.).
- **Hash-based routing**: `createWebHashHistory()` — URLs look like `/#/record`.
- **State**: minimal Vuex (UI only — sidebar). Most state is component-local or `localStorage`-backed.
- **HTTP**: `import { API } from '@/api'` (Axios instance with JWT + 401 interceptors). Tokens live at `localStorage.getItem('snooze-token')`.
- **Event bus**: `this.emitter` (mitt) is globally registered on the app for cross-component events.

### Code documentation

- **When to document**: public functions, plugin lifecycle hooks, non-obvious config flags, workarounds (always explain *why*).
- **Style** — Python example:
  ```python
  def process(self, record: Record) -> Record:
      """Apply the rule to a record. Returns the (possibly mutated) record."""
  ```

### Automatic formatting

```bash
# Format / lint Python
task py:lint            # ruff check

# Lint web
(cd web && npm run lint)
```

### Logging Format

Snooze uses `python-json-logger` for structured JSON logs in production. For dev / tests, the human-readable format from `pyproject.toml` is:

```
%(asctime)s %(name)-20s %(levelname)-8s %(message)s
```

OpenTelemetry traces are wired through `src/snooze/tracing.py` and ship via OTLP when configured.

</code_style>

---

<architecture_decisions>

## Architecture Decisions

### Tech Stack

| Component | Technology | Version / Pin |
|-----------|------------|---------------|
| Language (backend) | Python | **3.13** (mise.toml, `.python-version`) |
| Language (frontend) | Node / JS | Node 14 (.node-version, mise.toml) |
| API framework | Falcon | `>=4.0,<5` |
| WSGI server | waitress | `>=3.0,<4` |
| Database | MongoDB | client `pymongo>=4.6,<4.7` (pinned <4.7 — see "What we DON'T do") |
| Auth | JWT (PyJWT 2), LDAP (ldap3), local | `>=2.8,<3` / `>=2.9,<3` |
| Config validation | Pydantic | **v1** (`>=1.10.18,<2`) |
| CLI | Click | `>=8.1,<9` |
| Telemetry | OpenTelemetry (api/sdk/otlp + falcon/logging/pymongo instr.) | `>=1.27,<2` |
| Templating | Jinja2 | `>=3.1,<4` |
| Build (Python) | uv + hatchling | latest |
| Build (web) | Vue CLI 4.5 | — |
| Frontend framework | Vue | `^3.2.33` |
| UI library | @coreui/vue | `4.3.0` |
| State | Vuex | `4.0.2` |
| Router | Vue Router | `4.0.15` |
| Test (backend) | pytest + mongomock + responses + freezegun | pytest `>=8,<9`, mongomock `>=4.3,<5` |
| Test (web) | Jest (vue-cli-plugin-unit-jest) + Nightwatch | — |
| Type check | pyright (strict) | `>=1.1.380,<2` |
| Lint | ruff + pylint (+ pylint-pydantic) | pylint `>=3.2,<4` |
| Security scan | bandit | `>=1.7.9,<2` |
| Local DB store | tinydb | `>=4.5,<4.6` (pinned <4.6 — see "What we DON'T do") |
| Task runner | go-task/task | latest (mise.toml) |
| Toolchain manager | mise | — |

### Adopted Patterns

| Pattern | Where | Why |
|---------|-------|-----|
| **Plugin discovery by directory** | `src/snooze/plugins/core/<name>/plugin.py` + `metadata.yaml` | Drop-in extension without editing the loader; falls back to `Basic` class if no `plugin.py` |
| **Falcon Resource per route** | `src/snooze/api/` and each plugin's `falcon/route.py` | Class-per-endpoint with `on_get` / `on_post` / `on_delete` |
| **JWT auth via middleware** | `src/snooze/api/__init__.py:57-62`, `token.py` | Stateless auth; root token from unix socket for admin ops |
| **DB abstraction** | `src/snooze/db/database.py` (`Database` ABC, `get_database()` factory, `AsyncDatabase` wrapper) | Swap mongo/file backends; batch counter increments to avoid write storms |
| **SurvivingThread** | `src/snooze/utils/threading.py` | One thread per subsystem (housekeeper, syncer, TCP) with restart semantics |
| **Custom condition DSL** | `src/snooze/utils/condition.py`, `utils/parser.py` | List-form ASTs like `['=', 'host', 'foo']` used in both API queries and rule definitions |
| **`mitt` global event bus (web)** | `web/src/main.js` (registered as `emitter`) | Cross-component signalling without prop drilling |
| **localStorage schema cache (web)** | `web/src/utils/api.js` `get_data()` | Cache form schemas keyed by `{endpoint}_json` with checksum invalidation |
| **src/ package layout** | `src/snooze/`, `packages = ["src/snooze"]` (hatchling), `pythonpath = ["src", "."]` for tests | Keeps the importable package name `snooze` while sources sit in `src/snooze/` |

### What We DON'T Do

| Anti-pattern | Why we avoid it |
|--------------|-----------------|
| Recreate top-level `snooze/` directory | Older layout placed code there. It was removed during the 3.13 migration. If it reappears it is stale and confuses imports. |
| Pydantic v2 syntax | Dep is pinned `<2`. Code will not start. |
| Reintroduce `poetry` | Removed entirely. The project uses `uv` (Astral) — locked via `uv.lock`, CI via `astral-sh/setup-uv@v8`, packaging containers install uv. Invoke tasks call `uv build` / `uv version`, not `poetry build` / `poetry version`. |
| Bump `pymongo>=4.7` | `pymongo` 4.7 added a `sort` kwarg to `UpdateOne._add_to_bulk()` that `mongomock` 4.3.0 (latest released) does not accept — `bulk_increment` tests fail. Stay on `>=4.6,<4.7` until mongomock releases the fix. |
| Bump `tinydb>=4.6` | `tinydb` 4.6 removed implicit integer-index path traversal (`Query()['a'][1] == 2`). `dig()` in the file backend relies on it. Stay on `>=4.5,<4.6` or rewrite `dig()` to emit callable path parts. |
| Use `pkg_resources` | Removed in favour of `importlib.metadata.entry_points(group=…)`. `pkg_resources` is deprecated and slow on import. |
| `datetime.utcnow()` in NEW code | Deprecated in 3.12+. Use `datetime.now(timezone.utc)`. Existing call sites still emit warnings — fix opportunistically. |
| Composition API in new Vue components | Existing UI is uniformly Options API — mixing styles fractures patterns. |
| New top-level state in Vuex | UI is mostly localStorage + component state; growing Vuex un-asked adds friction. |
| Run pytest without `PYTHONPATH=src` | The package is in `src/snooze/`; without `PYTHONPATH=src` (or `task py:test`) imports fail. |
| Bypass JWT middleware for "admin" endpoints | Root operations go through the unix-socket route, not by skipping auth. |
| Bake config into Helm template defaults | `values.schema.json` validates user input; defaults live in `values.yaml`. |

</architecture_decisions>

---

<learning_loop>

## Learning Loop

> "Each unit of work should make the next ones easier."

### At Each Session

| Question | If YES → Action | File |
|----------|-----------------|------|
| New problem solved? | Document symptom + cause + fix | `CHANGELOG.md` (user-visible) or commit body |
| New architecture decision? | Add to `<architecture_decisions>` here or to `docs/general/architecture.rst` | this file / Sphinx |
| New non-obvious tip discovered? | Add to `<code_style>` or `<architecture_decisions>` "What we DON'T do" table | this file |
| Recurring code pattern? | Add to `<code_style>` | this file |
| New dependency / tool? | Update `pyproject.toml` (Python) or `web/package.json` + lockfile | repo |
| User-facing config change? | Update Sphinx config docs | `docs/configuration/` |
| Release-worthy change? | Add entry under the next `[Unreleased]` section of `CHANGELOG.md` | `CHANGELOG.md` |

### Session Handoff

**BEFORE ending a session**:
1. Make sure work-in-progress changes leave the tree in a buildable state, OR mark explicitly which files are mid-edit.
2. Summarise what was done, what is still open, and the next concrete command.
3. Inform user of pending changes to commit (user manages git manually).
4. If the session was long enough that context may compact, restate the goal + the next step in the summary so a follow-up session can resume.

### Process Improvements

When you identify friction during a session, update *this* file (the appropriate section). Prefer:
- `<hard_rules>` for non-negotiable constraints.
- `<code_style>` for coding conventions.
- `<how_to_work>` for process / commands.
- `<architecture_decisions>` for design rationale.

</learning_loop>

---

## Quick Reference

```bash
# Setup
uv sync                                            # Python deps
(cd web && npm ci)                                 # Web deps
mise install                                       # Tooling (python 3.8, node 14, task, uv, ruff)

# Run (dev)
PYTHONPATH=src uv run snooze-server                # Backend on :5200
(cd web && npm run serve)                          # Web on :8080, talks to :5200
docker compose up                                  # Full clustered stack on :80

# Test & Lint
task py:lint                                       # ruff check
task py:test                                       # PYTHONPATH=src uv run pytest
uv run pyright src/snooze                          # strict type check
(cd web && npm run lint && npm run test:unit)      # Web

# Build
task py:build                                      # uv build (wheel + sdist)
(cd web && npm run build)                          # Web → web/dist/
task docker:release                                # Push image to GHCR (confirm first!)
```

**Files to consult first**:
1. `src/snooze/core.py` — boot sequence & plugin loader
2. `src/snooze/api/__init__.py` — HTTP surface
3. `tests/conftest.py` — how to write a test
4. `docs/index.rst` — user-facing feature inventory
5. `CHANGELOG.md` — recent direction of travel
