## [Unreleased]

### Added

- **User API keys.** Users can mint/revoke personal API keys (Profile → API
  Keys) carrying a subset of their own permissions and an optional, capped
  expiry; authenticate with `Authorization: Bearer snz_…`. Effective
  permissions are bounded live by the owner's current roles. New `ro_apikey` /
  `rw_apikey` permissions gate a tenant-scoped admin **API Keys** page. New
  config `auth.apikey_max_ttl` (default 365d).
- **Demo seed on first boot.** Set `SNOOZE_SERVER_CORE_SEED_DEMO=true` (or
  `core.seed_demo: true` in `core.yaml`) and the bootstrap phase populates a
  rich demonstration dataset: three environments (production / staging /
  development with colours and conditions), three extra users (alice.martin,
  bob.chen, charlie.ops), three rules (Parse Host Components, Day Shift, Night
  Shift), two actions, two notifications, three snooze filters, 17 alert records
  in mixed states enriched as if they passed through the full pipeline, five
  comments, and 14 days of hourly stats counters (alert_hit, alert_snoozed,
  notification_sent) so the dashboard charts render non-empty time-series on
  first visit. The seed is idempotent — re-running with the flag enabled is a
  no-op. Designed for the Render try.snoozeweb.net deployment.

### Changed

- **Web — Sidebar user chip opens an account menu.** Clicking the avatar/username
  at the bottom of the left navigation now opens a dropdown with **Profile** and
  **Log out** shortcuts (mirroring the top-bar user menu), so the two most common
  account actions are reachable from where the signed-in user is shown.
- **Web — Settings → OIDC / SSO uses progressive disclosure.** The OIDC tab now
  behaves like the LDAP tab: only the *Enabled* toggle shows until OIDC is
  switched on, then the issuer, client, scope and claim settings appear. Stops
  the tab dumping eight provider fields on operators who haven't enabled SSO.
- **Web — Dashboard "Alerts over time" shows a selection box while dragging.**
  Dragging across the chart now paints a translucent accent-coloured band that
  follows the cursor (Grafana-style), and on release drills into the alerts
  spanning the **whole** dragged window (first → last bucket) instead of just
  the bucket under the release point. A plain click still drills into a single
  bucket.
- **Web — Rules "Modifications" column shows the full action.** Each badge now
  reads e.g. `SET environment = prod`, `ARRAY_APPEND tags += urgent`,
  `REGEX_SUB msg = s/foo/bar/` or `KV_SET owner = owners[host]` instead of the
  truncated `SET environment`, so the rule's effect is legible without opening
  the editor.
- **Web — Alerts search no longer shows a redundant chip.** The active-filters
  strip dropped the *Search* chip (the search box already displays the query and
  has its own clear button); the strip now appears only for tab / environment
  filters.
- **Web — list-page search is now shareable via the URL.** Pressing Enter on a
  search query (once it parses cleanly) writes it to the address bar as
  `?search=…` alongside any other filters, so the filtered view can be
  bookmarked, shared, and survives a reload; clearing the box drops the
  parameter. This now applies to **every** list page (Alerts, Rules,
  Notifications, Snoozes, Users, Roles, Environments, Widgets, Key-Value), not
  just Alerts — the two tabbed pages (Rules, Notifications) keep an independent
  query per tab (`?search=` + `?aggSearch=` / `?actionSearch=`). Typing is still
  kept out of the URL per-keystroke — only the discrete Enter/clear commit
  updates history, sidestepping the async-navigation dropped-character problem.

### Added (web)

- **Web — comment count on alert rows.** A row whose `comment_count > 0` now
  carries a small count pill on the corner of its actions (`⋯`) button, flagging
  alerts that already have discussion; the full thread stays in the expandable
  row detail.

---

## v2.2.0

### Added

- **SSO users are now visible and manageable.** OIDC/Microsoft 365 users are
  provisioned just-in-time on their first login (previously they existed only
  inside the issued token), so they appear on the Users page under a per-backend
  tab (e.g. *Microsoft 365*) next to Local/LDAP. Re-login refreshes their groups
  and last-login without clobbering admin-assigned roles. The Users list shows
  **effective roles** — group-derived (SSO/LDAP) roles in addition to explicitly
  assigned ones — so an SSO admin no longer appears role-less, and the Groups
  column is capped (`+N more`) so a user with many directory groups stays
  readable.
- **Enable / disable any user.** Each user carries an `enabled` flag, toggled
  from the user editor and shown as a Status badge in the list. A disabled user
  is blocked at login (local **and** SSO) and can no longer refresh an existing
  session, so access is cut off within the access-token lease. The last enabled
  `platform_admin` is protected from being disabled.
- **Group → role mapping is now editable in the UI.** The Role editor gained a
  **Groups** field (and the roles list a Groups column), so admins can map
  auth-backend groups / OIDC App Roles (e.g. `GrafanaAdmin`) to a Snooze role
  from the web UI — previously this field existed only in the database.
- **OIDC config is now runtime-editable (Settings → OIDC / SSO).** The OIDC
  connection + claim fields (`enabled`, `issuer`, `client_id`, `redirect_url`,
  `scopes`, `roles_claim`, `groups_claim`) moved to DB-backed runtime settings
  with live reload, mirroring the LDAP tab. The `client_secret` stays a
  file/env secret (never written to the DB) and `method` stays file-config. The
  login index now evaluates backends under the default tenant so a runtime
  `enabled` toggle (OIDC or LDAP) surfaces on the login page without a restart.

- **OpenID Connect authentication backend** (Microsoft 365 / Entra ID supported
  out of the box). Configure via the `oidc` file-config section. Entra App Roles
  map to Snooze roles through the existing group→role mapping (`Admin` → `admin`).
- **Login page redesigned:** each enabled auth method is now a button (primary
  credential form with SSO/alternate methods below) instead of tabs.

- **Multi-tenancy (D1–D10).** A single `snooze-server` now hosts multiple
  isolated organizations (tenants). Every alert, rule, snooze filter, user,
  role, notification, and settings document is scoped to a `tenant_id` slug;
  data from different tenants is never mixed at query time.

- **`default` tenant.** A reserved `default` tenant is seeded automatically
  at first boot. A brand-new (empty) install needs no migration. An **existing
  pre-multitenancy database must be backfilled once** with `snooze-server
  migrate multitenancy` *before* starting the upgraded server — the fail-closed
  tenant scoping otherwise hides every un-stamped document (see below).

- **`POST /api/v1/tenant`** — create a new tenant (requires `rw_tenant`).
- **`GET /api/v1/tenant`** — list all tenants (requires `ro_tenant`).
- **`GET /api/v1/tenant/{id}`** — fetch one tenant (requires `ro_tenant`).
- **`PATCH /api/v1/tenant/{id}`** — update display name, status, or ingest
  token (requires `rw_tenant`).
- **`DELETE /api/v1/tenant/{id}`** — delete a tenant registry document
  (requires `rw_tenant`; the `default` tenant is undeletable).

- **Per-tenant ingest tokens.** Each tenant carries an opaque `ingest_token`.
  Supply it as `Authorization: Bearer <token>` (or `?token=<token>`) on
  `POST /api/v1/alerts` and `POST /api/v1/webhook/*` to route unauthenticated
  ingestion to that tenant. Absent or unknown tokens fall back to `default`.

- **Login `org` field.** All login endpoints (`/api/v1/login/local`, `/ldap`,
  `/anonymous`) accept an optional `"org"` field to scope the issued JWT to a
  specific tenant. Omitting `org` scopes to `default`.

- **`tenant_id` JWT claim.** Issued tokens carry a `tenant_id` claim. Legacy
  tokens without the claim are accepted and treated as `default`.

- **Platform-tier permissions** `rw_tenant` / `ro_tenant` gate the
  `/api/v1/tenant` registry routes, independent of any tenant.

- **`platform_admin` seeded role** (holds `rw_tenant` + `ro_tenant`). The root
  user is assigned this role at bootstrap.

- **`snooze tenant` CLI** with subcommands `create`, `list`, `get`, `update`,
  `delete`.

- **`snooze-server migrate multitenancy`** — one-shot, idempotent, dedup-safe
  migration that opens the configured database and backfills `tenant_id="default"`
  **in place** across every tenant-scoped collection (users and roles included),
  seeds the `default` tenant document + `platform_admin` role, and grants the
  root user `platform_admin`. A completion sentinel makes re-runs no-ops. Run it
  once against an existing pre-multitenancy database before starting the upgraded
  server.

- **LDAP per-tenant.** LDAP settings are stored in the `settings` collection
  and are therefore tenant-scoped; each tenant can point to a different
  directory.

- **Tenant-partitioned plugin caches.** Rule, snooze-filter, aggregate-rule,
  and notification processor caches are partitioned by tenant; a reload for
  tenant A does not flush tenant B's cache.

- **Tenant-aware login.** The login page is always multi-tenant aware. Tenants
  carry a `listed` flag (default true): same-org deployments get an Organization
  dropdown when more than one tenant is listed; SaaS deployments unlist tenants
  and share a per-tenant opaque login link (`/web/login?key=…`, rotatable from
  the tenant page) so the tenant list is never exposed to anonymous visitors.
  New endpoints: `GET /api/v1/login/tenant?key=` and
  `POST /api/v1/tenant/{id}/rotate-login-key`.

- **`db.Driver.Writer.Increment` gains a leading `ctx context.Context`.**
  Asyncwriter coalescing is now tenant-partitioned: stats from different
  tenants are never merged into the same counter bucket.

- **`RuntimeSettings.InvalidateForTenant(tenantID string)`.** Lets the
  settings plugin invalidate only the cache partition for the tenant that
  changed, rather than flushing the entire settings cache.

- **`syncer.Event.Tenant` field.** Syncer events carry the tenant slug;
  topic names follow the convention `collection.<collection>.<tenant>` for
  tenant-scoped events and `collection.<collection>` for global events.

- **`housekeeper.ForEachTenant`.** Cleanup jobs iterate active tenants and
  re-scope per tenant so one tenant's slow cleanup cannot block another.

- **Key-values dictionary tabs (web).** The admin **Key-values** page now shows
  a tab bar above the search bar — an **All** tab plus one tab per discovered
  dictionary — that filters the list to the selected dictionary. The bar is
  hidden when only a single dictionary exists.

### Changed

- **`GET /api/v1/login`** now returns backend descriptor objects
  (`{name, kind, display_name, icon}`) instead of a list of strings.

- **`sql.Builder.Convert` and `mongo.Convert`** gain leading `ctx context.Context`
  and `collection string` parameters. The new parameters drive automatic
  `tenant_id` predicate injection at the driver layer. All callers updated.

- **Refresh token primary key** is now `["tenant_id", "token_hash"]` so a
  refresh token in org A cannot clobber a token in org B.

- **User primary key** is `["tenant_id", "name", "method"]`; role PK is
  `["tenant_id", "name"]`.

- **Settings PK** is `["tenant_id", "name"]`; the settings cache is
  partitioned by tenant.

- **Alert comment timeline (web UI)** now lists activity newest-first
  (reverse-chronological), so the most recent comments land on page 1 instead
  of the last page. The pager gained **« first page** and **» last page** jump
  buttons alongside the existing previous/next controls.

- **Web UI colour consistency.** The Profile page now colours permissions with
  the same code as the Roles table (read-write `rw_*` amber vs read-only `ro_*`
  blue, instead of a flat blue list). Alert-page severity badges use the
  dashboard's gradated per-severity palette so each severity renders as its own
  shade. The dashboard "Ack" and "Closed" stat-tile accents are swapped (Ack
  green, Closed purple), and the "closed" lifecycle now renders as a muted
  purple badge in the recent-activity feed, the alert state column, and the
  comment timeline. The reserved `platform_admin` role gets a distinct violet
  accent in the roles and users tables.

### Fixed

- **`ingest` section now loadable from `ingest.yaml`.** The config loader's
  `sectionFiles` map was missing an `ingest` entry, so an `ingest.yaml` dropped
  in the `--config` directory was silently ignored and the section could only be
  set via `SNOOZE_SERVER_INGEST_*` env vars. It now layers from file like every
  other section.

- **`web` config section is now honored.** `web.enabled` / `web.path` (and
  `SNOOZE_SERVER_WEB_*`) were parsed but never consumed — the UI directory came
  solely from the `--web-dir` flag. The server now serves the UI from the
  config section; an explicitly passed `--web-dir` still wins (and
  `--web-dir=""` still disables the UI). The section's default `path` changed
  from the Python 1.x location `/opt/snooze/web` to `/var/lib/snooze/web`,
  matching where the deb/rpm install the bundle — migrated 1.x `web.yaml`
  files carrying the old path should drop or update it.

- **Tenants nav item (web UI)** is now gated by the same rule the backend
  enforces on `/api/v1/tenant` (`RequirePlatformPerm`): it appears only for
  users authenticated against the `default` tenant who hold a *literal*
  `ro_tenant`/`rw_tenant` permission. Previously the sidebar honored the
  `rw_all` wildcard and ignored tenant origin, so `rw_all` admins and
  non-default-tenant users saw a Tenants menu whose API calls returned 403.

### Security

- **Platform-admin integrity (hardening).** Granting or removing the
  `platform_admin` role now requires a *literal* `rw_tenant` permission (the
  `rw_all` wildcard no longer suffices); the reserved permissions
  `rw_tenant`/`ro_tenant` are confined to the seeded `platform_admin` role,
  which is now API-immutable — it cannot be created, edited (including its
  group mappings), or deleted through the API; and the server refuses to
  remove, disable, or delete the last enabled platform admin. Together these
  close a path by which a default-tenant `rw_all` admin could escalate to
  platform admin (directly, or indirectly by group-mapping users into the
  `platform_admin` role) or lock the tenant registry out. Boot logs a warning
  about any pre-existing role that carries reserved permissions outside
  `platform_admin`.

## v2.1.0

### Fixed
- **Aggregate timeline / `comment_count` drift.** The aggregate-rule processor
  bumped a record's `comment_count` on every lifecycle transition (auto-close,
  auto-reopen, watch-field re-escalation, re-escalation outside the throttle
  window) but no longer wrote the matching `comment` document — so the alert
  timeline (which reads real comment docs by `record_uid`) stayed empty while
  `comment_count` inflated without bound. Restored the Snooze 1.x behaviour of
  writing an automatic comment in lockstep with each counter bump, so these
  transitions show up in the timeline again. (Pre-existing records keep their
  historical inflated `comment_count`; only events from this release forward
  produce timeline entries.)
- **Snoozed alerts stuck out of the Alerts tab after escalating.** An alert
  snoozed under one severity (e.g. matched a `warning` snooze filter) kept its
  `snoozed` attribution after re-aggregating into a higher severity, so it
  stayed hidden from the Alerts tab even though it no longer matched any filter.
  The aggregate-rule processor now clears a stale `snoozed` whenever a record
  re-aggregates and continues to the snooze plugin (non-throttled), so the
  snooze plugin re-asserts it only if the current record still matches.
  Throttled / flapping / already-closed duplicates abort before the snooze
  plugin runs and deliberately keep their prior attribution.
- **Comments now record their author.** Human ack/close/comment actions stamp the
  authenticated user (and auth method) onto the `comment` document, so the alert
  timeline shows who acted and "edit your own comment" works. Auto-generated
  escalation/auto-close comments remain system events (no author).
- **`database.type: sqlite` no longer fails to boot.** Config validation only
  accepted `mongo`/`file`/`postgres` and rejected the documented `sqlite`
  spelling (plus the `pg`/`mongodb` aliases) that the driver layer already
  supports, so a config copied from the quickstart aborted at startup with a
  `oneof` error. Validation now accepts every spelling the driver dispatches on.
- **CLI now defaults to the right server port.** `snooze --server` fell back to
  `http://localhost:9001` while the server listens on `5200`, so out-of-the-box
  CLI commands failed to connect. The default (and the `runtime-server` image's
  `EXPOSE`) are now `5200`.
- **Runtime `housekeeping.cleanup_aggregate` override is honoured.** Editing the
  aggregate-cleanup interval in the Settings UI was silently dropped and the
  live job stayed pinned to the file-config baseline; the override now applies.
- **`core.enabled_optional_plugins` env override splits on commas.** Setting
  `SNOOZE_SERVER_CORE_ENABLED_OPTIONAL_PLUGINS=a,b` previously yielded a single
  `"a,b"` element; it now parses as a list like the other list-valued fields.
- **`auth.token_algorithm` validation matches the engine.** The schema accepted
  `HS384`/`HS512`, but the token engine implements only `HS256` and aborted at
  boot; validation now rejects the unsupported values up front.
- **Audit-log retention never ran.** The housekeeper's audit cleanup matched
  `action: "deleted"`, but the API writes the verb `"delete"`, so on every
  backend `CleanupAuditLogs` matched nothing and the `audit` collection grew
  unbounded. Fixed the literal; cleanup now prunes a deleted object's trail.
- **Snooze/notification auto-expiry broken on MongoDB.** The expiry sweep
  decoded nested documents as `bson.D` but only handled `bson.M`, so it silently
  found no expired entries and deleted nothing. Expired snoozes and
  notifications are now cleaned up on Mongo.
- **Cross-backend retention parity.** `CleanupTimeout` now uniformly keeps a
  record that has `ttl` but no `date_epoch` (matching the legacy pipeline), and
  `CleanupAuditLogs` resolves "latest event" by the populated `date_epoch` with
  identical same-epoch tie semantics across SQLite/Postgres/Mongo (Postgres was
  previously non-deterministic).
- **Helm: the server never loaded its mounted config.** The chart set
  `SNOOZE_SERVER_CONFIG` (which the binary ignores) instead of passing
  `-config /config`, so the mounted ConfigMap was dead; the SQLite
  StatefulSet and `docker-compose` also used `SNOOZE_DATABASE_*` env vars the
  loader drops. Both now use the `-config` flag and the
  `SNOOZE_SERVER_CORE_DATABASE_*` names.
- **systemd: the server unit pointed `-config` at a file and SQLite couldn't
  write.** `-config` now targets the `/etc/snooze/server` directory (created by
  the rpm/deb packages) and `WorkingDirectory=/var/lib/snooze` lets the default
  SQLite database land on the writable volume.
- **Postgres/SQLite immutable-field (`Constant`) check could panic** on a JSON
  array/object-valued field; the comparison is now panic-safe.
- **`snooze-server` leaked the message-queue connection at shutdown** (the
  Postgres/Mongo bus owned a pool/client held for the process lifetime); it is
  now closed.

### Added
- **"How to inject alerts" guide on the empty Alerts page.** When no alerts have
  been ingested yet, the Alerts table now offers a **How to inject alerts**
  button that opens a modal with copy-pasteable setup snippets for every
  injection endpoint (REST API, webhook receivers, daemon inputs), each linking
  to its documentation page. A new "Send your first alert" quickstart page backs
  it. A filtered or searched empty result shows a distinct "no matches" message
  instead.
- **Restore dashboard stat counters.** The dashboard now shows DB-persisted
  hourly counter series for hits / throttled / snoozed / notifications /
  action success / action errors, with by-state and top-host breakdowns.
  Counters accrue forward-only from the first run after upgrade; chart
  resolution is hourly. Counter writes and the dashboard are gated on
  `general.metrics_enabled`. Operator-configurable retention via
  `housekeeping.cleanup_stats` (default `9600h` = 400 days), editable in
  **Settings → Housekeeping** without a restart.
- **Dashboard activity feed = real users only.** The "Recent activity" pane now
  filters to attributed user actions (`EXISTS user`), so escalation/auto-close
  noise no longer floods it. Every dashboard pane title gained a content icon,
  and the "Top hosts" pane now ranks hosts by count with legible labels.
- `db.Driver.UnsetFields(ctx, collection, fields, cond)` — a portable field
  delete (`$unset` / jsonb `-` / `json_remove`) implemented across all three
  backends. Unlike a merge write, it truly removes the key so `EXISTS field`
  stops matching everywhere; covered by the shared dbtest suite and per-backend
  integration tests.
- In-process **Microsoft Teams** and **Mattermost** notifier plugins (Incoming
  Webhook), so chat integrations no longer require a hand-written generic
  `webhook` action.
- Branded **integration gallery** in the Actions editor, plus a per-integration
  **Send test** button (`POST /api/v1/action/test`) and a **setup-docs link**
  (`doc_url` / `category` plugin metadata).
- **Brand logos in the Actions integration picker.** The integration gallery and
  the config-step header now show each notifier's brand mark — Slack, Mattermost,
  Microsoft Teams, Discord, Telegram, Google Chat, PagerDuty, Opsgenie,
  Statuspage, Amazon SNS, Twilio, ntfy — instead of a generic category glyph.
  The marks are vendored single-path glyphs from Simple Icons (CC0) in
  `web/public/brands.svg`, rendered monochrome in the current theme color (no
  hard-coded brand colors, so dark/light theming is preserved). Notifiers with no
  brand mark (mail, webhook, script, …) keep their category glyph.

### Changed
- **Teams notifications link to the All tab.** The `snooze-teams` "View in
  Snooze" button and host link now point at `/web/alerts?tab=all&search=…`
  instead of the default Alerts tab. By the time a recipient clicks through, the
  alert may have been acked, closed, or snoozed — all hidden from the Alerts
  tab — so the All tab guarantees the record is visible.
- **Breaking (CLI):** the auxiliary `snooze-*` daemons now share one entry-point
  contract — config path is `-c` (the old `-config` is removed), `-debug`
  replaces `-log-level`, logs are text on stderr, and a `version` subcommand is
  standard. Update any systemd units or scripts that passed `-config`/`-log-level`.
  (This also fixes units that were already broken by the `-c`/`-config` mismatch.)
- **Aggregate identity is now severity-independent.** Throttle accepts a scalar
  **or** a `{value: seconds, …, default: seconds}` map matched against the rule's
  `watch` values (first match wins). This lets one severity-agnostic rule per
  problem keep per-value throttle, so `ok`/resolved events reliably close the
  matching open aggregate instead of leaking into `default`. Creating/updating a
  rule whose `fields` duplicate another enabled rule's is now rejected (422); the
  server logs existing duplicates at startup. Merging severity tiers into one
  rule re-forks those aggregates once.
- **`auth.token_secret` now takes effect.** Setting it (file config or
  `SNOOZE_SERVER_AUTH_TOKEN_SECRET`, ≥32 bytes) overrides the auto-generated
  DB-stored JWT signing key — previously the field was silently ignored. Lets
  operators pin a shared signing key across a fleet or rotate after a suspected
  compromise.
- **`syncer.hostname` and `syncer.sync_interval` now take effect** — they set
  the cluster-heartbeat node identity and cadence (and the syncer debounce
  window); both were previously inert. The redundant `syncer.sync_interval_ms`,
  the unused `syncer.total`, and the inert `housekeeping.renumber_field` knobs
  were removed (all three were silently ignored at runtime).

### Internal
- New `internal/daemon` harness backs every auxiliary binary; `internal/runtime`
  removed (its `automaxprocs` side effect folded into `internal/daemon`).
- The Cond→SQL WHERE translation for the Postgres and SQLite backends is now one
  shared builder (`internal/db/sql`) wired with per-backend dialects, replacing
  the two duplicated translators; the `internal/db/dbtest` conformance suite is
  wired into all three driver tests.

### Documentation

* Migrated the documentation site from Sphinx (reStructuredText) to
  **Docusaurus 3** (Markdown under `docs/content/`). All pages were converted
  from RST, cross-references rewritten, and the build enforces zero broken
  links/anchors (`onBrokenLinks`/`onBrokenAnchors: throw`). Local offline
  search, and the OpenAPI 3.1 contract rendered as an interactive Redoc page
  at `/api/`. A new `.github/workflows/docs.yml` builds on every PR and
  deploys to GitHub Pages on push to `master`. Build locally with
  `task docs:build` / preview with `task docs:serve`.

### New integrations

A large batch of input and output integrations. Each ships mock unit tests plus
an env-gated end-to-end test (`task go:test:e2e`) and a documentation page under
`docs/general/integrations/`. New plugins use `net/http`/stdlib only — no new
module dependencies.

**Inputs**

* `cloudwatch` — Amazon CloudWatch Alarms via SNS HTTP(S) delivery webhook receiver (auto-confirms subscriptions).
* `datadog` — Datadog monitor-alert webhook receiver.
* `azuremonitor` — Azure Monitor Common Alert Schema webhook receiver.
* `sentry` — Sentry webhook receiver (legacy plugin + modern Integration payloads).
* `newrelic` — New Relic Alerts webhook receiver (workflow + legacy condition shapes).
* `heartbeat` — dead-man's-switch plugin: a `heartbeat` collection, an unauthenticated ping endpoint (`/api/v1/webhook/heartbeat?name=<name>`), and a background scanner that fires one alert per missed heartbeat.
* `snooze-otlp` — daemon: OTLP/HTTP (JSON) receiver converting OpenTelemetry log records into alerts (logs only; HTTP+JSON, no gRPC/protobuf).
* `snooze-k8s-events` — daemon: watches the Kubernetes core/v1 Event API over plain HTTP (no client-go) and forwards Warning events as alerts, with in-cluster auto-detection and watch reconnect/410 handling.

**Outputs**

* `slack` — Slack notifier (Incoming Webhook + bot token, Block Kit, severity colours, resolve styling).
* `telegram` — Telegram Bot API notifier (HTML/MarkdownV2).
* `discord` — Discord webhook notifier (embeds + plain text).
* `googlechat` — Google Chat outbound notifier (cardsV2 + thread grouping).
* `pushover` — Pushover mobile-push notifier (severity→priority, emergency retry/expire).
* `ntfy` — ntfy notifier (public or self-hosted push, bearer/basic auth).
* `pagerduty` — PagerDuty Events API v2 notifier (trigger/resolve, dedup key from record hash).
* `opsgenie` — Opsgenie Alert API notifier (create/close by alias, us/eu region).
* `servicenow` — ServiceNow incident notifier (Table API, Basic auth, create + resolve).
* `statuspage` — Atlassian Statuspage notifier (create/resolve public incidents).
* `twilio` — Twilio SMS and automated voice-call notifier (multi-recipient).
* `sns` — Amazon SNS publish notifier, signed with a hand-rolled AWS SigV4 (stdlib only, no AWS SDK).

**AI / agents**

* `snooze-mcp` — daemon: a Model Context Protocol (MCP) stdio server exposing Snooze alerts and ack/close/comment/snooze actions as tools to AI assistants (Claude Desktop, Cursor).

### Ingest authentication

* Route authentication is now resolved **per path**: a single plugin can keep its CRUD subtree authenticated while exposing a public sub-path. `AuthorizeRoute(meta, path)` and the webhook mount honour `Metadata.Routes[path].Authentication` instead of only the plugin-wide default.
* New optional `ingest` bootstrap config section (all fields off by default → 1.5.0 parity):
  * `ingest.token` — a shared secret required on every `/api/v1/webhook/*` request (`Authorization: Bearer <token>` or `?token=`).
  * `ingest.sns_verify` — verify Amazon SNS message signatures on the `cloudwatch` receiver (with a SigningCertURL host allow-list / SSRF guard).
  * `ingest.sentry_secret` — verify the Sentry `sentry-hook-signature` HMAC-SHA256 on the `sentry` receiver.
* `heartbeat` is now secured properly: its CRUD collection requires operator auth, and the ping (`POST /api/v1/webhook/heartbeat?name=<name>&token=<token>`) is gated by an unguessable per-heartbeat token generated on create.

## v2.0.0

v2.0.0 is a ground-up rewrite of snooze from Python to Go, paired with a
React 19 frontend. The wire contract stays close to the Python API but
several legacy shapes are gone; see `docs/migration/python-to-go.md` for
the field-by-field mapping.

### Backend: Python → Go

* Server, CLI, and the eight auxiliary daemons (`snooze-relp`,
  `snooze-syslog`, `snooze-snmptrap`, `snooze-smtp`, `snooze-mattermost`,
  `snooze-googlechat`, `snooze-teams`, `snooze-pacemaker`) are now ten
  statically-linked Go binaries, distributed as distroless images on
  Docker Hub (`snoozeweb/snooze-<binary>`).
* Plugin loader no longer accepts Python modules. Built-ins are
  compiled in via `internal/pluginimpl/all`; out-of-tree plugins must
  be forked into the Go tree. Third-party Python plugins from
  `snoozeweb/snooze_plugins` will not load.

### Frontend: Vue → React

* Web UI rewritten in React 19 + Vite 6 + TypeScript, replacing the
  Vue 3 + CoreUI codebase. Feature parity preserved; sidebar
  reorganised into Operate / Configure / Admin groups.
* Rules + Aggregates merged into one page with two tabs. Same for
  Notifications + Actions.
* Dashboard charts switched to in-house Chart.js wrappers (Line / Bar /
  Donut) that read colours from CSS tokens, so the theme toggle works
  everywhere.
* Dark and light themes with a per-user toggle (defaults to dark).
* Command palette (⌘K / Ctrl+K) for jump-to navigation.
* Cross-tab auth sync: logging out in one tab logs out the others.
* Auto-refresh on the Alerts page, opt-out per user.
* In-house SVG icon sprite (45 Lucide-derived glyphs), one cached asset.
* Node 22+ required (the old Node-14 pin is gone).

### HTTP API (breaking)

* `Authorization: JWT <token>` is no longer accepted. Send
  `Authorization: Bearer <token>`. Tokens are still HS256 JWTs
  (`HS384`/`HS512` selectable in `core.yaml`).
* Paginated responses now use an envelope:
  `{"data": [...], "meta": {"count", "limit", "offset", "total"}}`.
  The bare-array shape is gone.
* Positional list URLs (`/{search}/{perpage}/{pagenb}/{orderby}/{asc}`)
  are replaced by `GET /api/v1/{plugin}?q=&offset=&limit=&orderby=&asc=`,
  plus `POST /api/v1/{plugin}/search` for queries that don't fit in a URL.
* Error envelope is `{"error": {"code", "message", "details",
  "request_id", "trace_id"}}` with stable string codes
  (`bad_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`,
  `validation_error`, `unavailable`, `internal`).
* CRUD verbs: `POST` to create, `PUT /{uid}` for full replace,
  `PATCH /{uid}` for partial update, `DELETE` (with `?q=`) for bulk
  delete. The `replace=true` query parameter is gone.
* Refresh-token flow for sessions: `/api/v1/login/{local,ldap,anonymous}`
  returns an access JWT plus a single-use opaque refresh token (32
  random bytes, stored as SHA-256). `/login/refresh` rotates the pair;
  `/login/logout` revokes (idempotent). Lease is `auth.refresh_token_lease`
  (default 7 days). Roles and permissions re-resolve on every refresh.
* New `GET /api/v1/metadata` and `/{plugin}` endpoints expose each
  plugin's parsed `metadata.yaml` (forms, widgets, settings catalogue)
  so the frontend can render typed forms instead of JSON textareas.
* `snooze-server` gained a `-web-dir` flag (default
  `/var/lib/snooze/web`) to serve the bundled SPA.

### Configuration (breaking)

* No more YAML hot-reload. `WritableConfig`, the `filelock` dance, and
  the on-disk-rewriting WebUI form are gone. Runtime-editable settings
  live in the database via the `settings` plugin.
* Bootstrap config is YAML only, in `/etc/snooze/server-go/`
  (`core.yaml`, `general.yaml`, `ldap.yaml`, `housekeeper.yaml`,
  `notification.yaml`, `syncer.yaml`, `web.yaml`, `auth.yaml`). The
  legacy `/etc/snooze/server/*.yaml` layout still loads.
* Env vars are `SNOOZE_<SECTION>_<KEY>` (e.g. `SNOOZE_CORE_PORT=5201`).
  The flat `DATABASE_URL` shortcut still works.
* LDAP and housekeeping settings are now runtime-editable. The settings
  plugin exposes the full `ldap.*` and `housekeeping.*` keysets; the
  LDAP backend re-reads on every auth, and housekeeper jobs consult
  the resolver on every fire. Changes in the Settings UI take effect
  on the next request — no restart.
* Removed knobs: `core.cluster_*` (replaced by the syncer, on by
  default), `core.bootstrap` legacy keys (now seeded by the `settings`
  plugin), `web.host_static` (the Go binary serves the SPA directly).

### Authentication (breaking)

* The Python bootstrap secret was `sha256("root")`. The Go bootstrap
  generates a 24-byte random password, bcrypt-hashes it, and prints the
  plaintext **once** to stderr on first start. There is no longer a
  known default `root:root` credential.
* Existing local users from upgraded databases are preserved. See
  `docs/migration/python-to-go.md#root-user-rotation` for how to
  re-bootstrap a fresh root via the admin Unix socket.
* `JWT` is no longer a valid method name in the `Authorization` header
  or the audit log; the canonical wire name is `bearer`.

### Storage & infra

* SQLite backend via `modernc.org/sqlite` (pure-Go, no cgo, JSON1).
  Single-binary, single-file deployments are possible and are the
  default for `database.type: sqlite` (legacy alias `file` still maps
  here).
* Three backend-native message buses: `inproc`, Postgres `LISTEN/NOTIFY`,
  and Mongo change streams. The Kombu / amqp-on-mongo bridge is retired
  and `snooze_kombu_*` collections are untouched.
* Cluster syncer rides the same channels (or `inproc` for SQLite). The
  standalone 1Hz polling loop is gone.
* Telemetry: structured `log/slog` JSON loggers (`api`, `audit`, `core`),
  OpenTelemetry SDK + OTLP gRPC exporter (`--otel-endpoint`), Prometheus
  registry at `/metrics`.
* Housekeeper now expires snoozes and notifications too
  (`cleanup_snooze`, `cleanup_notification` jobs), in addition to alerts.
* Packaging: GoReleaser-driven cross-arch releases, signed distroless
  images, `.deb` + `.rpm` via nfpm, per-binary systemd units, refreshed
  Helm chart with `database.kind: mongo | postgres | sqlite` (SQLite
  mode renders a StatefulSet; Postgres keeps the CloudNativePG hand-off
  from 1.6).
* Hand-curated `api/openapi.yaml` describes the v1 surface.

### Dropped

* TinyDB (replaced by SQLite/JSON1).
* `WritableConfig` and the filelock-based YAML mutator.
* Kombu (`kombu[mongodb]`) and the `snooze_kombu_*` collections.
* Dynamic Python plugin module loading and the `snooze.plugins.core`
  entry-point group.
* Sphinx-based Python API doc generation. Narrative docs remain.
* Falcon, Pydantic v1, Waitress, the in-process clustering helper.

### Bug fixes

* **Microsoft Teams reply threading restored.** Follow-up notifications now
  post as replies under the originating alert's Teams message instead of new
  top-level messages, matching the 1.x bot. Four pipeline gaps were closed:
  * `notification`: `inject_response` (`response_<action>`) is now stamped on a
    record's *first* firing. It was keyed on the not-yet-assigned `uid`, so
    alerts that never re-notified — e.g. `critical` aggregates with a long
    throttle window — never recorded their Teams message id, and the `response`
    field was simply absent.
  * `aggregaterule`: server-injected `response_<action>` fields are carried
    forward onto the in-memory record on a duplicate match (Python parity), so
    the notifier can read the recorded message ids. The incoming alert never
    carries them, so without this they were invisible to the pipeline.
  * `webhook`: a new `.ReplyToIDs` body-template variable exposes the recorded
    per-channel message ids, so a Teams action emits `reply_to_ids` without
    naming the (possibly space-containing) action in the template.
  * `snooze-teams`: the bridge records the thread *root* id across a reply
    chain rather than each reply's own id, so every follow-up keeps threading
    under the original message (Microsoft Graph only allows one reply level).
  * `snooze-teams`: a threaded follow-up posts a succinct text reply
    (`New escalation on <time>` + the alert message) instead of repeating the
    full Adaptive Card the thread root already shows, matching the 1.x bot and
    Teams' plain-text reply convention.
* **Action edits apply without a server restart.** The notification dispatcher
  caches the `action` collection in memory but only subscribed to its own
  collection's change events, so edits to an action (URL, payload,
  `inject_response`, …) silently took effect only after a restart. The syncer
  now also reloads a plugin when a collection it declares as a dependency
  changes (`ReloadDeps`); the notification plugin declares `action`.

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
