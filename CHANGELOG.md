## v2.6.0

### Added

- **Resolution hold.** A re-fire of an alert closed as `resolved` or `self_resolved` stays closed for `housekeeping.resolution_hold` (default 2h, `0` disables). Higher-severity re-fires still notify.
- **JIRA close sync.** Closing an alert the `jira` notifier ticketed comments on the ticket and can transition it (`on_close`, `close_transition`). New `plugins.CloseNotifier` interface.
- **API keys in the CLI.** `credentials.token` in `client.yaml`, `$SNOOZE_TOKEN`, `--token`. New `snooze apikey create|list|revoke`, `snooze whoami`, `--owner me`.
- **`GET /api/v1/user/me`** returns the verified caller identity.
- **Tool attribution.** Comments and CLI writes accept a `source` (`--source`, `$SNOOZE_SOURCE`), shown on the timeline.
- **Alert ownership.** Ack/close set an owner; new Assign and Release actions, owner filter, `POST /api/v1/record/bulk_owner`, `GET /api/v1/record/owners`. Run `snooze-server migrate owners` once after upgrading.
- **Profile pictures.** Upload from the Profile page; `PUT`/`DELETE /api/v1/user/me/avatar`, `GET /api/v1/avatar/{method}/{name}`, `GET /api/v1/people`. CLI: `snooze people`, `snooze avatar`.
- **`resolved` verdict** for `remediation_plan.status` (a fix was applied, as opposed to `self_resolved`).
- **CLI triage.** `record list` filters (`--active`, `--state`, `--host`, `--severity`, `--owner`, `-c`); `record comments`, `comment`, `reopen`, `escalate`.
- **Agentic analysis.** Alerts can carry an `agentic` subtree (`root_cause`, `remediation_plan`, server-stamped `analysis`), served by `GET`/`PUT`/`DELETE /api/v1/record/{uid}/agentic`, `snooze record agentic`, and the MCP tools. It is the first protected field: ingestion strips it, and CRUD, rules and notifiers refuse it. Writes need the literal `rw_protected` permission (`rw_all` does not grant it); reads need `ro_record`. See `general/agentic_analysis.md`.
- **Analysis in the web UI.** Inspector Analysis tab, dashboard Analyses view (`/web/dashboard?view=analyses`), and an Analysed tile.
- **Notification delivery history.** Every send is recorded in `notificationlog`, shown on a Deliveries tab. Settings: `notification.delivery_log` (default on), `housekeeping.cleanup_notificationlog` (30 days). Reads need `ro_notificationlog`.
- **Postgres search indexes.** Each `search_fields` entry gets expression indexes, built by a background worker.

### Changed

- Analyses view keeps closed alerts and has a multi-select Verdict filter; filters live in the URL.
- `snooze record agentic get` prints a readable summary; `--json` gives the raw subtree.
- Repeated deliveries and timeline entries are folded into runs (`GET /api/v1/notificationlog/runs`, `GET /api/v1/comment/runs`).
- The Flow chart marks where a run stopped (silenced, or held by the throttle).
- Alert tabs are Alerts, Acknowledged, Snoozed, Closed, Shelved, All. The Re-escalated tab is gone.
- Analysis contract adds `root_cause.detail`, `root_cause.caveats`, `remediation_plan.status` and `steps[].when`.
- `bulk_update` and `bulk_state` return `403` for collections with per-document write hooks (apikey, role, user, comment, savedsearch, notificationlog, heartbeat, aggregaterule, tenantmatch) unless the plugin implements `plugins.BulkWriteGuard`.
- The `snooze` plugin owns the `snoozed` field (`plugins.SuppressionOwner`). A filter's Hits counter now counts throttled repeats too.
- Alert details header reorganised; `first_seen` stamped on records; record tab fills the drawer and has a find box.
- Drawers are non-modal side panels instead of blocking scrims.

### Fixed

- Switching a condition group to NOT keeps only its first child.
- An alert still firing when its snooze filter ends is notified (the throttle no longer hides it).
- A snooze filter now applies to alerts inside a throttle window (`plugins.Filter`).
- Alerts silenced by a filter that no longer exists are released (`reconcile_suppression` sweep).
- A snooze filter the pipeline cannot parse is rejected with `422` instead of silently ignored.
- A `discard` filter no longer leaves phantom lifecycle comments (`plugins.Result.AfterPersist`).
- A recovered silenced alert shows as passed through, not "not notified".
- Snooze hit counts are batched, and a throttled repeat is no longer counted twice on the dashboard.
- API-key actions are owned by the key's owner.
- Profile pictures for email logins display (`%40` in avatar paths).
- Back navigation pushes one history entry per tab change.
- Deep links with both `?search=` and `?record=` open the inspector.
- `POST /api/v1/{plugin}/bulk_update` no longer returns 405.
- `ro_all` satisfies `ro_*` gates on bespoke routes.
- Data race between `plugins.WaitBackground` and `Build`.
- Batch buckets, `hits`/`last_sent` counters and `notificationlog` writes are tenant-scoped and no longer trigger reload storms.
- SQLite concurrent writes wait on `busy_timeout` instead of failing with `SQLITE_BUSY`.
- Postgres maintenance queries skip rows with non-numeric values instead of aborting.

## v2.5.0

### Added

- JIRA ticket titles can be overridden (`summary`, `summary_template`); the title is clamped to 255 characters and falls back to the built-in title.
- Re-escalation reaches every notifier (ticket updates, incident escalation, threaded chat replies).
- Google Chat records thread ids so slash-commands resolve to the right alert.
- Sessions renew silently; sign-out revokes the refresh token.
- Snooze settings `snooze_bypass_severities` and `syncer.reload_safety_interval` (default `5m`, periodic full reload).
- Web: alert table redesign, manual refresh, snooze from the row, keyboard triage and search in the command palette, accessible failure states, visual refresh.

### Changed

- `general.ok_severities` and `general.snooze_bypass_severities` are read from runtime settings and apply live.
- Tenant-less change events fan out to every active tenant, so deleting a filter takes effect without a restart.
- The reload debounce is capped at ten windows.
- The mongo bus logs dropped subscriber events.

### Fixed

- A throttled duplicate no longer increments `duplicates` twice.
- The anti-flapping budget refills per throttle window.
- One JIRA ticket per alert again (the handle is stamped on the record, `tojson` keeps `Record.Extra`).
- JIRA priorities come from the project's live priority scheme, not hardcoded English names. New package `internal/jirapriority`.
- Close transitions are not wedged open by a snooze filter.
- `general.ok_severities` is enforced in the ingest pipeline.
- `kv` values no longer leak between tenants, and its reload no longer logs a warning every 5 minutes.
- Boot hydrates `kv` for every tenant.
- Live config reload works on MongoDB (change-stream `bson.D` decoding).
- SQLite and Postgres `Contains`/`In` no longer misfire on non-array fields.
- Snooze filters evaluate oldest-first.
- "Create and apply" retro-applies.
- Empty alert payloads are rejected.

## v2.4.0

### Added

- **Federation is a notification action.** The Federation page and `forward` collection are removed; relay to a peer with the `snoozepeer` action. Convert existing destinations with `snooze-server migrate forward-to-action`.
- **Ingest kill-switch.** Settings → Ingest → *Alert intake enabled* halts `POST /api/v1/alerts` and webhooks with `503`.
- **Tenant routing.** Admin → Org matching maps groups, domains or logins to tenants (`tenant_match`, `/api/v1/tenant_match`). `tenant_match.fail_closed` denies unmatched users.
- **Groups** (Admin → Groups, `/api/v1/group`) bundle users and roles.
- **Security Audit** page for auth events; **Heartbeats** page with status badges.
- **Console branding** (`console.logo`, `console.title`, `console.audio`, `console.clipboard_template`, `console.default_filter`) is applied by the SPA. `GET /api/v1/config` serves org-wide console defaults.
- **Bulk actions:** "Select all N matching" and a "Tag / set fields" dialog (`POST /api/v1/record/bulk_update`).
- **Timed shelve.** A `shelve` comment sets `shelve_until`; alerts return to open after `housekeeping.shelve_timeout` (default 4h).
- **Lifecycle countdowns and trend badges** on the alerts table; illegal transitions are hidden from menus.
- **Chat buttons.** Slack and Telegram can render interactive ack/close/re-open buttons (`interactive: true`), signed or secret-checked, fail-closed.
- **OIDC presets** (`provider:` google/azure/cognito/keycloak/gitlab) and multiple IdPs via `oidc_providers.yaml`.
- **Acks expire and stale alerts escalate.** `ack_until` reverts expired acks; `housekeeping.escalate_after` escalates unacknowledged alerts.
- **PagerDuty inbound** webhook (`/api/v1/webhook/pagerduty`) syncs ack/resolve back to records.
- **Heartbeat latency.** `max_latency` and `?sent_at=` raise a `slow` alert before the heartbeat expires.
- **Suppression-bypass severities** (`general.snooze_bypass_severities`).
- **aggregaterule** stamps `previous_severity` and `trend_indication`; escalations bypass the throttle.
- **Attribute-based tenant routing** and **server-driven console config**.
- **Acked-by column**, **Stackdriver** receiver, **Graylog** and **Pingdom** receivers.
- Groups, saved searches, the `snooze_records` gauge, `POST /api/v1/housekeeping/run`, `GET /api/v1/version`, `ingest.allow`, API-key `last_used_at`, and `source_ip` on ingest.
- Audit rows for login, logout and token refresh; `reject` plugin (HTTP `422 policy_rejected`) with a Reject tab.
- Alert Flow tab (matched notifications and action outcomes). Gated by `notification.persist_action_outcomes` (default on).
- Inputs page and `GET /api/v1/inputs`.

### Fixed

- Named `authorization_policy` grants appear in `GET /api/v1/permissions`.
- Invalid state transitions (ack on acked, close on closed) return `403`.
- New Relic `acknowledged` maps to `State: "ack"`.
- The Rules tree is mobile-responsive.

### Security

- Auth-proxy bypass fixed: the `trusted_proxies` allowlist checks the TCP peer address, not the client-settable `X-Forwarded-For` header. Only deployments with `auth_proxy` enabled were affected.

## v2.3.0

### Added

- Mobile web UI down to 360px: bottom-tab bar, cards, full-screen sheets.
- User API keys (`snz_…`, Profile → API Keys), `ro_apikey`/`rw_apikey`, `auth.apikey_max_ttl` (365d).
- Demo seed (`core.seed_demo`, `SNOOZE_SERVER_CORE_SEED_DEMO=true`).

### Changed

- Sidebar user chip opens an account menu.
- OIDC settings use progressive disclosure.
- Dashboard "Alerts over time" supports drag-to-select.
- Rules "Modifications" shows the full action.
- List-page search is shareable through `?search=`.

### Added (web)

- Comment-count pill on alert rows.

## v2.2.0

### Added

- **Multi-tenancy.** Tenants (`default` seeded), `POST`/`GET`/`PATCH`/`DELETE /api/v1/tenant`, per-tenant ingest tokens, the login `org` field, `tenant_id` JWT claim, `platform_admin` role and `snooze tenant` CLI.
  - **Upgrade:** run `snooze-server migrate multitenancy` once before starting the upgraded server on an existing database.
- **Login.** Tenant-aware login page; `GET /api/v1/login/tenant?key=`; `POST /api/v1/tenant/{id}/rotate-login-key`.
- **SSO users** are provisioned on first login and appear on the Users page, with effective roles.
- **Enable/disable users.** Disabled users cannot log in or refresh.
- **Group → role mapping** is editable in the Role editor.
- **OIDC / Microsoft 365 / Entra** backend (`oidc` config). OIDC settings are runtime-editable.
- **Login page** shows each enabled method as a button.
- **Key-values** tabs per dictionary.

### Changed

- `GET /api/v1/login` returns backend descriptor objects.
- Primary keys are tenant-scoped (users, roles, refresh tokens, settings).
- Alert comments are newest-first, with first/last page buttons.
- Colour consistency across permissions, severities and lifecycle states.

### Fixed

- `ingest.yaml` loads from file.
- The `web` config section is honoured.
- The Tenants nav item follows the backend's rule.

### Security

- `platform_admin` is immutable through the API, and granting or removing it requires a literal `rw_tenant`. The last enabled platform admin cannot be removed.

## v2.1.0

### Fixed

- Aggregate timeline and `comment_count` drift: lifecycle transitions write their timeline comment again.
- Snoozed alerts no longer stuck hidden after re-escalating.
- Comments record their author.
- `database.type: sqlite` boots.
- CLI default port is `5200`.
- Runtime `housekeeping.cleanup_aggregate` override applies.
- `core.enabled_optional_plugins` env splits on commas.
- `auth.token_algorithm` validation matches the engine (`HS256` only).
- Audit-log retention runs (`delete` verb).
- Snooze and notification expiry works on MongoDB.
- Retention parity across SQLite, Postgres and Mongo.
- Helm and docker-compose load the mounted config.
- systemd unit `-config` points at a directory; SQLite writes to `/var/lib/snooze`.
- Immutable-field check no longer panics on arrays or objects.
- Message-queue connection closes at shutdown.

### Added

- "How to inject alerts" guide on the empty Alerts page.
- Dashboard stat counters with hourly history (`housekeeping.cleanup_stats`, default 400 days).
- Activity feed shows real users only.
- `db.Driver.UnsetFields`.
- In-process Microsoft Teams and Mattermost notifiers.
- Integration gallery with brand logos, per-integration Send test, and setup-docs links.

### Changed

- Teams notifications link to the All tab.
- **Breaking (CLI):** daemons use `-c` (not `-config`), `-debug` (not `-log-level`), and a standard `version` subcommand.
- Aggregate identity is severity-independent; throttle accepts a per-value map. Duplicate rules are rejected with `422`.
- `auth.token_secret` takes effect, overriding the DB-stored JWT key.
- `syncer.hostname` and `syncer.sync_interval` take effect. `syncer.sync_interval_ms`, `syncer.total` and `housekeeping.renumber_field` are removed.
- Bulk alert actions use one `POST /api/v1/record/bulk_state` call.
- Housekeeper purges expired API keys and refresh tokens hourly.
- Table rows are text-selectable, with a context-menu Copy.

### Internal

- `internal/daemon` harness backs every auxiliary binary.
- Postgres and SQLite share one Cond→SQL builder (`internal/db/sql`).

### Documentation

- Docs site migrated from Sphinx to Docusaurus 3 (`docs/content/`), with zero broken links enforced. `task docs:build` and `task docs:serve`.

### New integrations

- **Inputs:** CloudWatch (SNS), Datadog, Azure Monitor, Sentry, New Relic, `heartbeat` (dead-man's-switch), `snooze-otlp` (OTLP/HTTP logs), `snooze-k8s-events`.
- **Outputs:** Slack, Telegram, Discord, Google Chat, Pushover, ntfy, PagerDuty, Opsgenie, ServiceNow, Statuspage, Twilio, SNS.
- **AI:** `snooze-mcp`, a stdio MCP server exposing alerts and actions to assistants.

### Ingest authentication

- Route authentication resolves per path.
- New `ingest` section: `ingest.token` (shared bearer for webhooks), `ingest.sns_verify`, `ingest.sentry_secret`.
- Heartbeat pings require a per-heartbeat token.

## v2.0.0

A ground-up rewrite from Python to Go, with a React 19 frontend. See `docs/migration/python-to-go.md` for the field-by-field mapping.

### Changed

- **Backend:** Go server, CLI and daemons, distributed as distroless images (`snoozeweb/snooze-<binary>`). Python plugins from `snooze_plugins` no longer load; built-ins are compiled in.
- **Frontend:** React 19 + Vite 6 + TypeScript replaces Vue 3. Rules and Aggregates, and Notifications and Actions, each merge into one page.
- **Auth:** `Authorization: Bearer <token>` replaces `JWT <token>`. Sessions use refresh tokens (`/api/v1/login/refresh`, `/login/logout`, lease `auth.refresh_token_lease`, default 7 days).
- **Root password:** generated and printed once to stderr on first boot. The `root:root` default is gone.
- **HTTP API:** paginated envelope `{"data", "meta"}`; `GET /api/v1/{plugin}?q=…` replaces positional URLs; error envelope with stable codes; `PUT` replaces, `PATCH` patches, and `replace=true` is gone.
- **Config:** YAML only in `/etc/snooze/server-go/`. Env vars are `SNOOZE_<SECTION>_<KEY>`. LDAP and housekeeping settings are runtime-editable. Removed: `core.cluster_*`, `web.host_static`.
- **Storage:** SQLite (pure Go) is the default. Inproc, Postgres LISTEN/NOTIFY and Mongo change-stream buses replace Kombu. Housekeeper also expires snoozes and notifications.
- **Packaging:** GoReleaser builds, `.deb`/`.rpm`, and a refreshed Helm chart.

### Removed

- TinyDB, Kombu, `WritableConfig`, Python dynamic plugin loading, Sphinx docs, Falcon and Waitress.

### Fixed

- Microsoft Teams replies thread under the originating alert.
- Action edits apply without a restart (`ReloadDeps`).

## v1.7.0

- `snooze-jira` is a native Go daemon, with a bidirectional poller that closes records when their ticket is Done.
- PostgreSQL backend (experimental) via `database.type: postgres`.
- Helm `database.kind: mongo | postgres` selector (CloudNativePG).
- `DATABASE_URL` accepts `postgres://`.

## v1.6.3

- Fixed a syntax error when a custom snooze action fired.

## v1.6.2

- Pinned `requests` to avoid a `urllib3` incompatibility.

## v1.6.1

- AlertManager webhook support.
- Fixed out-of-path access and CORS configuration.

## v1.6.0

- Grafana 8.5+ webhook and OpenTelemetry support.
- Clustering replaced by a periodic DB sync; simpler logging; better env-var support for lists and nested objects.
- Fixed Mongo regex options, nested rules, batched OK handling, and the flapping counter.

## v1.5.0

- Dashboard graph drills into alerts; alert preview in conditions; modifications on re-open.
- Rules tree with drag-and-drop; environment multi-select.
- Grafana 8.5+ support; housekeeper cleans rule orphans.

## v1.4.1

- Notification frequency and batch display; `/api/health`; Nagios/Icinga `check_snooze_server`.
- Fixed thread management and TinyDB audit; more logging detail.

## v1.4.0

- Batched actions, audit logs, daily backups, flapping prevention, time constraints over midnight.
- Switched to Poetry.

## v1.3.0

- AND/OR with more than two arguments; Key-values modification; rotating logs.
- Failed notifications are resent (configurable).

## v1.2.0

- RegexSub modification; Prometheus webhook.
- Table and modifications display improvements.

## v1.1.2

- Copy and search selection in table context menus.

## v1.1.1

- Pinned PyMongo below 4.0.

## v1.1.0

- Vue 3 and CoreUI 4 migration; row selector on tables; Regex Parse modification.
- `alert_closed` metric; `SNOOZE_CLUSTER` env variable; docker-compose deployment.

## v1.0.17

- Settings for auto-closing severities; tables configurable from files; expired notifications cleaned up.

## v1.0.16

- Fixed TinyDB after v1.0.11.

## v1.0.15

- Local metrics dashboard; default landing page; last-login tracking; InfluxDB 2.0 webhook.

## v1.0.14

- External core plugins; snooze filters can discard alerts and be retro-applied.

## v1.0.13

- Waitress and CI fixes.

## v1.0.12

- Kapacitor webhook; generated bootstrap secrets stored in the DB.

## v1.0.11

- Environments (search filters applied on top of any search).

## v1.0.10

- Anonymous login and an auth-disable option; Grafana webhook; Debian packaging.

## v1.0.9

- Manual alert trigger from the UI; auto-refresh toggle.

## v1.0.8

- Notification schedules; Jinja templates in modifications and webhooks; full-record webhooks; new query language.

## v1.0.7

- Notification time constraints and delay; aggregate watchlist; CA bundles for webhooks.

## v1.0.6

- Webhook response injection.

## v1.0.5

- Reworked alert lifecycle; webhook action.

## v1.0.4

- Aggregates merged into records.

## v1.0.3

- Widgets; record open/close lifecycle; Snooze time constraints; Patlite.

## v1.0.0–v1.0.2

- Initial release (2021-07-06), followed by small fixes.
