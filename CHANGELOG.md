## [Unreleased]

### Changed

* Web UI completely rewritten in React 19 + Vite 6 + TypeScript.
  Replaces the Vue 3 + CoreUI codebase. Feature parity preserved; IA
  reorganised into Operate / Configure / Admin sidebar groups.
* Rules + Aggregates merged into a single page with two tabs.
* Notifications + Actions merged into a single page with two tabs.
* Dashboard charts switched from CoreUI's chart wrappers to in-house
  Chart.js wrappers (Line/Bar/Donut) reading colours from CSS tokens
  so the theme toggle works on every chart.

### Added

* Dark and light themes with a per-user toggle (defaults to dark).
* Command palette (⌘K / Ctrl+K) for jump-to navigation.
* Cross-tab auth sync: logging out in one tab logs out the others.
* Auto-refresh on the Alerts page with per-user opt-out.
* In-house SVG icon sprite (45 Lucide-derived glyphs) served as one
  cached asset.

### Backend

* `snooze-server` learned a `-web-dir` flag (defaults to
  `/var/lib/snooze/web`, matching the existing Dockerfile copy path).
  This wires `Router.WebFS` — declared since v2.0.0 but never
  populated — and is the one Go-side change the rewrite required.

### Removed

* The Vue 3 SPA under `web/src/` and its `vue.config.js`,
  `babel.config.js`, and `jest.config.js`. The previous Node-14
  `.node-version` pin also went; we now require Node 22.

---

## v2.0.0

v2.0.0 is a ground-up rewrite of snooze-server from Python to Go. The wire
contract is intentionally close to the Python API but a number of legacy
shapes have been retired; see `docs/migration/python-to-go.md` for the
field-by-field mapping and operational steps.

### BREAKING — language / runtime
* **Full Python → Go rewrite.** Python 3 / Falcon / Pydantic v1 / Kombu are
  gone. The server, the CLI, and the eight auxiliary daemons
  (`snooze-relp`, `snooze-syslog`, `snooze-snmptrap`, `snooze-smtp`,
  `snooze-mattermost`, `snooze-googlechat`, `snooze-teams`,
  `snooze-pacemaker`) are now ten statically-linked Go binaries
  published as `ghcr.io/japannext/snooze-<binary>` distroless images.
* The plugin loader no longer accepts Python modules. Built-in plugins are
  compiled in via `internal/pluginimpl/all`; out-of-tree plugins must be
  forked into the Go tree (third-party Python plugins from
  `snoozeweb/snooze_plugins` are not loadable at runtime).

### BREAKING — HTTP API
* **Authorization header**: `Authorization: JWT <token>` is no longer
  accepted. Clients MUST send `Authorization: Bearer <token>`. Tokens
  themselves are still HS256 JWTs (`HS384`/`HS512` selectable in
  `core.yaml`).
* **List envelope**: the bare-array response (`[{…}, {…}]`) is replaced by
  `{"data": [...], "meta": {"count": N, "limit": L, "offset": O, "total": T}}`
  for every paginated endpoint.
* **List URL shape**: the positional
  `/{search}/{perpage}/{pagenb}/{orderby}/{asc}` URL fragment is replaced
  by the query-string form
  `GET /api/v1/{plugin}?q=<base64url-json>&offset=&limit=&orderby=&asc=`
  plus a richer `POST /api/v1/{plugin}/search` for queries that don't fit
  in a URL.
* **Error envelope** is now `{"error": {"code", "message", "details",
  "request_id", "trace_id"}}`. The string `code` values are stable and
  machine-readable (`bad_request`, `unauthorized`, `forbidden`,
  `not_found`, `conflict`, `validation_error`, `unavailable`, `internal`).
* **CRUD verbs**: `POST /api/v1/{plugin}` for create, `PUT
  /api/v1/{plugin}/{uid}` for full replace, `PATCH /api/v1/{plugin}/{uid}`
  for partial update, `DELETE /api/v1/{plugin}` (with `?q=`) for bulk
  delete. The Python `replace=true` query parameter on `POST` is gone.

### BREAKING — configuration
* **No more hot-reload of YAML.** `WritableConfig`, the `filelock` dance,
  and the on-disk-rewriting WebUI form are removed. Runtime-editable
  settings now live in the database via the new `settings` plugin (see
  `docs/configuration/`).
* **Bootstrap config is YAML only.** Files in `/etc/snooze/server-go/`
  (`core.yaml`, `general.yaml`, `ldap.yaml`, `housekeeper.yaml`,
  `notification.yaml`, `syncer.yaml`, `web.yaml`, `auth.yaml`). Legacy
  Python file names continue to load: the koanf provider treats the old
  `/etc/snooze/server/*.yaml` layout as a drop-in directory.
* **Environment variables** are now `SNOOZE_<SECTION>_<KEY>` (uppercase
  with `_` separator) — for example `SNOOZE_CORE_PORT=5201`. The flat
  `DATABASE_URL` shortcut is preserved.

### BREAKING — authentication
* The Python bootstrap secret was `sha256("root")` written into the local
  user document. The Go bootstrap now generates a 24-byte random password,
  bcrypt-hashes it, and prints the plaintext **once** to stderr on the
  first start. There is no longer a known default `root:root` credential.
* Operators upgrading an existing database keep their existing local users.
  See `docs/migration/python-to-go.md#root-user-rotation` for how to
  re-bootstrap a fresh root via the admin Unix socket.
* `JWT` is no longer accepted as a method name in the `Authorization`
  header or in the audit log; the canonical wire name is `bearer`.

### New
* **SQLite backend** via `modernc.org/sqlite` (pure-Go, no cgo) with JSON1.
  Single-binary, single-file deployments are now possible and are the
  default for `database.type: sqlite` (the legacy alias `file` still maps
  to SQLite). See `docs/configuration/sqlite.rst`.
* **Three backend-native message buses**: `inproc` (single-process fast
  path), Postgres `LISTEN/NOTIFY`, and Mongo change streams. The Kombu /
  amqp-on-mongo bridge is retired and the `snooze_kombu_*` collections
  are no longer touched.
* **Backend-native syncer**: cluster cache invalidation rides on the same
  Postgres NOTIFY / Mongo change-stream channels; SQLite uses the inproc
  bus. The standalone 1Hz polling loop is gone, lowering DB load.
* **Telemetry**: structured `log/slog` JSON loggers (one each for `api`,
  `audit`, `core`); first-class OpenTelemetry SDK + OTLP gRPC exporter
  (`--otel-endpoint`), and a Prometheus registry served at `/metrics`.
* **Packaging**: GoReleaser-driven multi-target releases, signed distroless
  images at `ghcr.io/japannext/snooze-<binary>`, `.deb` + `.rpm` via
  nfpm, systemd units per-binary, a refreshed Helm chart with
  `database.kind: mongo | postgres | sqlite` (SQLite mode renders a
  StatefulSet + persistent volume; Postgres mode keeps the CloudNativePG
  hand-off introduced in 1.6).
* **OpenAPI**: a hand-curated `api/openapi.yaml` describing the v1 surface
  ships in the repository.

### Dropped
* TinyDB (replaced by SQLite/JSON1).
* `WritableConfig` and the filelock-based YAML mutator.
* Kombu (`kombu[mongodb]`) and the `snooze_kombu_*` MongoDB collections.
* Dynamic Python plugin module loading and the `snooze.plugins.core`
  entry-point group.
* Sphinx-based Python API doc generation (`sphinx.ext.autodoc`,
  `sphinxcontrib.apidoc`). The narrative docs remain; the rendered Python
  API is no longer published.
* Falcon, Pydantic v1, Waitress, the in-process clustering helper.

### Removed configuration knobs
* `core.cluster_*` (replaced by the syncer, which is enabled by default).
* `core.bootstrap` legacy keys that mapped to `WritableConfig` (`general`
  is now seeded by the `settings` plugin).
* `web.host_static` (the web UI is now served directly by the Go binary or
  not at all; nginx is no longer required for the SPA).

## v1.7.0

### Changes
* Components: `snooze-jira` ported from the standalone Python plugin in
  `components/jira` to a native Go daemon under `internal/components/jira`
  (binary `cmd/snooze-jira`). It exposes the same `POST /alert` webhook
  surface and YAML config keys, plus a bidirectional JIRA poller that
  closes Snooze records when their JIRA ticket transitions to Done.
* Core: PostgreSQL backend (experimental). Set `database.type: postgres`
  in `core.yaml` to opt in; install the driver with
  `uv sync --extra postgres`. Documents are stored one-table-per-collection
  in a single `jsonb` column so the schemaless plugin contract is
  preserved. See `docs/configuration/postgres.rst` for the full config
  surface and trade-offs versus MongoDB.
* Tests: the suite is now parametrised over both backends. CI on
  `ubuntu-latest` uses testcontainers to spin up a real
  `postgres:16-alpine` for the Postgres branch; the Mongo branch
  continues to run against mongomock.
* Helm: new `database.kind: mongo | postgres` selector (default
  `mongo`, backwards-compatible). When set to `postgres`, the chart
  provisions a CloudNativePG `Cluster` instead of a MongoDBCommunity
  replica set; snooze-server reads `DATABASE_URL` from the CNPG
  app secret. The CNPG operator must be installed in the cluster.
* Config: `DATABASE_URL` now accepts `postgres://` and `postgresql://`
  URIs (psycopg-compatible) in addition to `mongodb://`.

## v1.6.3

### Bug fixes
* Fixing a syntax issue which happened when a custom snooze action was trigerred.

## v1.6.2

### Bug fixes
* Locking the requests dependency to avoid the lack of support for urllib3.
  See: https://github.com/psf/requests/issues/6432

## v1.6.1

### Changes
* Core: Support for AlertManager webhook

### Bug fixes
* Core: Properly prevent out-of-path access
* Core: Allow usrs to properly configure CORS policy

## v1.6.0

### Changes
* Core: Updated grafana webhook for v8.5+
* Core: Supporting Opentelemetry
* Core: Simpler logging configuration, and refactored logs
* Core: Removed the clustering feature, and opted for a regular sync job from the database
* Core: Better support of environment variables for lists and nested objects

### Bug fixes
* Web: Updating some deprecated libraries
* Web: Searching will now reset the current page to the first page
* Core: Fixed issue regarding regex options for Mongo
* Core: Nb of arguments mismatch in Modifications WebUI vs Backend
* Core: Preventing the crash of the delayed action thread in certain cases
* Core: Fixing processing of nested rules
* Core: OK for snoozed alerts are now correctly removed from batch send
* Core: Would not get the username when writing a comment with no Display name

## v1.5.0

### New features
* Web: Cliking on the main graph in Dashboard redirects to the corresponding alerts
* Web: Alerts preview when writing a condition
* Web: Can set modifications when re-opening an alert
* Web: New treeview for Rules. Drag&Drop support
* Web: Drag&Drop support for Environments
* Web: New Environment bar. Can select multiple ones at the same time
* Core: Support for Grafana 8.5+ (same webhook)
* Core: Housekeeper: cleanup rule orphans

### Changes
* Web: Updated all web packages + NodeJS (10->12)
* Web: Enabled/Disabled labels replaced with Checkmark/Crossmark

### Bug fixes
* Core: DB query typo in Actions
* Core: Batch form would not being displayed if no action was previously created
* Core: Fixed issue preventing the flapping counter from being reset
* Core: Fixed duplicate alerts issue in case of burst

## v1.4.1

### New features
* Web: Added a frequency display in Notifications
* Web: Added a batch display in Actions
* Core: Monitoring endpoint at `/api/health`
* Core: Nagios/Icinga compatible check script (`check_snooze_server`)

### Changes
* Core: Code linting and adding type hints
* Core: Now pre-catching all database errors to give more information about what
  was the query before throwing an exception
* Core: Backups can now fail independently on a per-collection basis

### Bug fixes
* Web: Bad display for Sunday
* Web: Sort weekdays
* Web: Could not reset Conditions right member correctly
* Core: Improving the thread management to prevent rogue threads dying without causing Snooze
  to die as well.
* Core: Fixing an issue related to the URL character limit when passing the connection string
  to kombu. Now it is using a patched transport backend that passes MongoClient()[database]
  directly.
* Core: Making sure batched actions are not out to date
* Core: TinyDB Audit was broken
* Core: Increasing log file size from 1MB to 100MB
* Core: Catching issues better within Action thread
* Core: Pretty big typo in Action class

## v1.4.0

### New features
* Web: Custom message for no alerts
* Web: Show current version in Status
* Core: Supports batched actions
* Core: Audit logs
* Core: Supports time constraints over midnight
* Core: Added daily backups
* Core: Prevent alerts flapping
* Env: Switched from pyenv to poetry
### Bug fixes
* Web: Removed CoreUI Collapse component
* Web: Resets current page number when changing tabs
* Web: Sunday was numbered as 7 instead of 6
* Web: Trim tags
* Web: Time related filters correctly updated on refresh
* Web: Fixed datetime on keyboard input
* Web: Fixed modals bouncing unexpectedly
* Core: (!=) Condition will not assume the field exists
* Core: Properly delete discarded logs
* Core: Fixed a concurrency issue when reloading plugins
* Core: Fixed an issue with IN operator for TinyDB
* Core: Prevent rejecting all PUT and POST data if only one is failing

## v1.3.0

### New features
* Core/Web: Better handling of strings in conditions and modifications
* Core/Web: Supports AND/OR condititions with more than 2 arguments
* Core: New Key-values modification (add fields to an alert based on matching a dictionary)
* Core: Added rotating logs in /var/log/snooze/snooze-server.log
* Core: Added `notification_from` field to Alerts when they get re-escalated
* Core: Resend failed notifications (configurable in Settings)
* Core: Supports prometheus-client 13.x
### Bug fixes
* Web: Fixed a display error when deleting part of a condition
* Web: Active and Upcoming Snooze filters/Notifications were sometimes wrong
* Web: Supports history for sorting and paging
* Core: Avoid loading in memory unnecessary plugin data
* Core: Fixed an issue with duplicate policies using Replace (lost UID)
* Core: Better handling of crashed conditions and modifications
* Core: Fixed a Time Constraints issue with exact matches
* Core: Triggered notifications in an alert were capped at one item
* Core: Metrics api endpoint failed to return sometimes
* Core: Do not retry all actions if only one fails
* Core: Fixed memory issue with comment related queries

## v1.2.0

### New features
* Web: Better display for some tables
* Web: Better display for Modifications
* Web: Set tables to a busy state for each request
* Core: RegexSub (useful for improving aggregation or scrapping secrets)
* Core: Prometheus webhook added
### Bug fixes
* Web: Could not clear search if the bar was empty
* Web: Improved Widget + Environment bar display
* Web: Few display issues
* Web: Modals and Toasts were not disappearing once faded out
* Web: Time in Time Constaints was reset when updating
* Web: Snooze filters Retro apply modal was not showing up
* Core: Conditions refactoring

## v1.1.2

### New features
* Web: Added Copy selection in tables context menu
* Web: Added Search selection in tables context menu
### Bug fixes
* Web: Values in Modifications were not correctly retrieved in edit mode
* Web: Mail and Grafana Infos wre not correctly ported to CoreUI 4.x
* Core: Grafana webhook did not work correctly if tags were empty
* Core: Conditions were not working if they were null
* Core: Receiving multiple OK for the same alert now processes the first one only

## v1.1.1

### Bug fixes
* Core: Forced Pymongo < 4.0

## v1.1.0

### New features
* Web: Updated from Vue 2.x to 3.x
* Web: Updated CoreUI from 3.x to 4.x
* Web: Removed Bootstrap dependency
* Web: Converted Radio buttons to Switches
* Web: Added row selector to Tables
* Core: Added alert_closed metric
* Core: Added SNOOZE_CLUSTER env variable
* Core: Separated alerts and comments housekeeping
* Core: New modification: Regex Parse
* Added full container deployment (docker-compose.yaml)
### Bug fixes
* Core: Could get duplicates if multiple servers were bootstraped at the same time

## v1.0.17

### New features
* WebUI Settings: configure severity levels that automatically close alerts
* Can now configure WebUI tables directly from config files
* Housekeeper: Also cleanup expired notifications
### Bug fixes
* JWT Tokens were not functioning properly
* Retro actively apply Snooze filters were throwing error messages if no change was made
* CONTAINS and IN conditions were not working properly if an alert value was empty
* Stats dashboard stored in TinyDB had chances to lock the DB when being displayed

## v1.0.16

### Bug fixes
* TinyDB was broken since v1.0.11
* Date was handled incorrectly for TinyDB metric features
* Github CI fix

## v1.0.15

### New features
* Storing metrics locally and displaying a dashboard
* Can configure a default landing page in preferences
* Keeping track of Last login for all users
* InfluxDB 2.0 webhook added
### Bug fixes
* Do no crash whenever a plugin fails to load
* Widgets pretty print was not working properly
* Failed webhook actions did not register as failed properly

## v1.0.14

### New features
* External core plugins support
* Added a spinner in the webUI when doing a DB query
* Search in Alerts should be faster
* Resized Condition box to get more input space
* Snooze filters can discard alerts
* Retro apply Snooze filters to all alerts
### Bug fixes
* Going back to wsgiref. It was working fine. Waitress is just having issues with TLS

## v1.0.13

### Bug fixes
* Fixed issues from previous version about Waitress
* Fixed CI to account for pypi delay before building docker image

## v1.0.12

### New features
* Kapacitor webhook added
* LDAP: Filtering out groups with group_dn or base_dn
* Moving Unix socket management out of the falcon API
* Using Waitress for Unix socket and TCP socket
* Secrets are now bootstrapped using random numbers and are stored in the backend database
* Dedicated middleware for logging
### Bug fixes
* When changing tabs or refreshing, webUI row tables are not flickering anymore
* Throttled alerts generated duplicate entries
* Aggregated alerts now correctly reset their snooze filters fields

## v1.0.11

### New features
* Environmnents support! Can be used to create search filters that can be applied on top of any search
### Bug fixes
* Wrong version of PyJWT broke LDAP auth
* Recent change in plugin loading broke plugin processing order

## v1.0.10

### New features
* Config option to disable authentication. People will be automatically logged in as root
* Anonymous login backend. Can be enabled in Settings (or general.yaml config file)
* Debian package export
* Webhooks support
* Grafana webhook added
* Copy content from any row in the WebUI
### Changes
* Plugin refactor. Now even actions are considered core plugins. Scanning snooze/plugins/core folder instead of declaring plugins in core.yaml
* Moved Patlite plugin to [snooze\_plugins](https://github.com/snoozeweb/snooze_plugins) repository
### Bug fixes
* Default authentication backend display order not being respected since 2021-06-30

## v1.0.9 (2021-09-04)

* Admins can use the webUI to manually trigger alerts
* Added a toggleable button to automatically refresh Alerts display
* Log in back to the webUI now keeps the initial query

## v1.0.8 (2021-08-27)

* Advanced schedule support for Notifications (number of notifications sent, frequency, delay)
* More environment variables supported (documentation to come later)
* Can now pass full Record to webhooks using {{ __self__ }} (Jinja template)
* New Search bar for the WebUI with a powerful [query language](https://github.com/snoozeweb/snooze/blob/master/doc/14_Query_language.md) supported
* Dockerfile added. Snooze image to come very soon!
* When re-escalating an alert, can now trigger Modifications. Any actual change to a Record will trigger Notifications again
* Can now use Jinja templates in Modifications (Rules, Re-escalations)
* Housekeeper will auto cleanup expired Snooze filters. Parameters supported
* New view for the Alert Infos tab

## v1.0.7 (2021-08-05)

* New feature: Time constraint for notifications. Same as for Snooze filters
* New feature: Delay for notifications. If an alert gets acknowledged or closed before the delay ends, it does not get notified.
* New feature: Watchlist for aggregate rules. Bypass aggregation if a specified field gets updated
* New feature: Webhooks now support CA bundles

## v1.0.6 (2021-07-29)

* Webhook fixes
* Added a new feature to webhooks: can now inject HTTP Response to a Record
* Fixes issue with Conditions NOT and EXISTS not being properly displayed

## v1.0.5 (2021-07-27)

* Fixed bugs with aggregates from previous release
* Reworked alerts lifecycle. Alerts first show up without a state. "open" state can now be entered only whenever reopening a closed alert by user interaction or automatically whenever a closed alert receives a new aggregation
* New action: Webhook! Can be used by Notification to call a URL. Documentation will come soon

## v1.0.4 (2021-07-26)

Transferred Aggregates logic to Records, meaning there is one less collection in the DB and one less menu item to care about. As a bonus, now whenever an aggregated record gets alerted, if the aggregate state was "open" or "ack", it will get automatically re-escalated (before it was creating a new alert)

## v1.0.3 (2021-07-20)

* Widgets
* Records lifecycle (open/close)
* New Snooze filters time constraints (datetime, time, weekdays). Can be mixed together
* Patlite support
* More documentation
* Bugfixes

## v1.0.2 (2021-07-09)

Fixes

## v1.0.0 (2021-07-06)

Initial release
