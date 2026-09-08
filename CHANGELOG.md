## Unreleased

### Added

- **Notification delivery history.** Every actual send an action performs —
  not just a matched notification — is now recorded as a permanent row in a
  new tenant-scoped `notificationlog` collection: send time, duration,
  `success`/`error` (with the error text), the action and notifier used,
  batching info, the notification(s) that routed it, and a snapshot of every
  alert covered (host, severity, message, state), so a row still renders after
  the alert record itself has expired. A misconfigured action (missing,
  notifier-less, or pointing at an unregistered notifier) now writes a failed
  row instead of failing silently. A **Deliveries** tab surfaces this on a
  notification's, an action's and an alert's details drawer; the alert
  inspector also gets a "Last notified … via …" line. The notifications table
  gained **Sent** and **Last sent** columns, and the dashboard gained a
  **Notifications** panel ranking notifications by send count for the current
  window, linking into the matching Deliveries tab. New settings:
  `notification.delivery_log` (default on) gates the writes;
  `housekeeping.cleanup_notificationlog` (default 30 days) governs retention,
  swept by a fixed daily housekeeper job regardless of the retention window's
  length. Batched sends (mail/webhook/script with `batch: true`) now report
  their outcome at flush time instead of at queue time — the record's action
  status reads `sent` until the batch actually delivers — and a batching
  action also flushes (`batch_reason: "shutdown"`) on a graceful server stop
  instead of losing its pending bucket. A misconfigured action's row is
  rate-limited to one per (notification, action) pair every 10 minutes.
  `GET`/`POST .../search` on `/api/v1/notificationlog` need `ro_notificationlog`
  (the seeded **notifications** role now includes it, backfilled onto
  existing installs); the collection otherwise refuses HTTP writes with `403`
  — rows come from the dispatcher only, `DELETE` still works with
  `rw_notificationlog`. **Send test** always delivers immediately, even on a
  batching action, bypassing the batch bucket entirely and leaving no row,
  counter bump or record stamp behind.

- **Postgres builds a per-search-field expression index.** Every field a
  plugin declares in `search_fields` now gets two B-tree expression indexes on
  its collection's table: a text one (`(data->>'field')`, serving the
  equality/regex/`IN` predicates) and a PARTIAL guarded-numeric one (the
  `CASE`-guarded `::numeric` cast, `WHERE … IS NOT NULL`, serving range and
  equality filters such as the housekeeper's `date_epoch < $1` sweep). The
  expressions are the same ones the query compiler emits, which is what lets
  the planner match them; the numeric index is partial because for a
  text-only field it would otherwise be an all-NULL B-tree paid for on every
  INSERT. Before this, `search_fields` registration was metadata only and a
  30-day `notificationlog` meant a sequential scan per page.

  The builds never run on a caller's goroutine. `CREATE INDEX CONCURRENTLY`
  waits out every transaction older than itself, so an inline build stalls
  boot for as long as the busiest open transaction lives; the driver queues
  them for a single background worker instead, on a dedicated non-pooled
  maintenance connection (visible in `pg_stat_activity` with an
  `… index-maint` application_name) so a `pool_max_size: 1` deployment is not
  starved. Passes are serialised across replicas by a session-level advisory
  lock — the loser skips, since the winner is building the identical set — and
  an interrupted build's leftover invalid index is dropped and rebuilt rather
  than skipped forever by `IF NOT EXISTS`. SEARCH scoping is registered
  synchronously and is never affected by a slow or failed build.

### Changed

- **`bulk_update` and `bulk_state` now return `403` for collections whose
  plugin carries a per-document write hook**, unless that plugin opts in by
  implementing the new `plugins.BulkWriteGuard`. Affected collections:
  `apikey`, `role`, `user`, `comment`, `savedsearch`, `notificationlog`,
  `heartbeat`, `aggregaterule`, `tenantmatch`. The per-document hooks are
  structurally unusable on a whole-query mutation and dangerous when forced
  onto one: `TransformWrite` runs once on the shared field merge, so the
  identity fields some transforms stamp (savedsearch's `owner`, comment's
  `user`/`method`) would be written onto every matched row — a bulk edit that
  quietly reassigns other people's rows to whoever ran it — while `GuardWrite`
  is defined per uid and would receive `""`, turning user's last-admin
  protection, role's reserved-role protection and comment's state-transition
  checks into no-ops that still look enforced. Refusing is the safe default;
  a collection becomes bulk-writable again by implementing `GuardBulkWrite`
  with semantics chosen for a query-wide write. The alerts UI is unaffected:
  `record` implements no write hook, so bulk ack/close/tag keep working.
  Retro-applying a snooze goes through the same gate.

### Fixed

- **A `notificationlog` write no longer reloads the `notification` plugin's
  cache.** The syncer's topic-prefix match was a plain string prefix, so a
  change event on `collection.notificationlog.<tenant>` matched the
  `collection.notification` subscription too (the longer collection name
  starts with the shorter one). `TopicMatches` is now delimiter-aware.
- **Batch buckets are tenant-scoped.** The batching notifiers (mail, webhook,
  script) key their in-memory flush buckets by tenant, so two tenants sharing
  the same action no longer flush each other's alerts into one delivery.
- **`hits` and `last_sent` counters on the notification entry** are stamped by
  the dispatcher on every successful delivery (one read-modify-write per
  notification per record, not per action), and are excluded from the diff the
  editor sends back on save so they never appear as a spurious change.
- **A concurrent SQLite write no longer fails outright with `SQLITE_BUSY`/`SQLITE_BUSY_SNAPSHOT`.** Every write path reads before it writes (primary-key lookup, read-modify-write patch), and under WAL's default DEFERRED locking that read-then-write promotion is refused instead of retried, bypassing `busy_timeout` entirely. `buildDSN` now sets `_txlock=immediate` and every write path opens its transaction through `beginWrite`, so the write lock is taken up front and a concurrent writer just waits out `busy_timeout` instead of erroring.
- **A counter-only update on `notification` or `snooze` no longer triggers a
  full plugin reload.** The read-modify-write that stamps `hits`/`last_sent`
  (and the snooze rule's own hit counter) on every match used to look like a
  semantically meaningful change to every syncer backend, so a busy server
  fed itself a reload storm from its own delivery traffic. SQLite, Postgres
  and Mongo now share one `IsCounterOnlyPatch` field set and suppress the
  change notification when a patch touches only those fields — generalized
  from a `notification`-only, `hits`-only check that would have missed the
  new `last_sent` counter and the `snooze` collection's own `hits` bump.
- **Postgres maintenance queries survive non-numeric text.** `record.Validate`
  accepts any JSON value, so a single row holding `{"ttl":"soon"}` or
  `{"date_epoch":"yesterday"}` used to abort a whole statement with `invalid
  input syntax for type numeric` — permanently, since the housekeeper and the
  inputs page re-run the identical statement every cycle, and the `data ? 'x'`
  tests that looked protective never were (SQL `AND` has no guaranteed
  evaluation order). The timeout sweep, audit retention, `SourceActivity`,
  `ComputeStats` and `Increment` now all project through the same guarded
  expression: a bad value reads as SQL NULL (or `0` where the query already
  coalesced a missing key), so the offending row is skipped and every other
  row is processed. `ComputeStats` gets the same treatment for a `date` that
  is not a timestamp.

## v2.5.0

### Added

- **The JIRA ticket title is overridable.** The in-process `jira` notifier
  already rendered its title from the action's **Summary** field; it now
  clamps the result to JIRA's 255-character limit and falls back to the
  built-in title (with a warning) instead of dropping the notification when
  the operator's template fails to render. The `snooze-jira` daemon gained a
  per-alert override: the `/alert` envelope accepts `summary` (and
  `summary_template`), taking precedence over `summary_template` in
  `jira.yaml`, so one webhook action can title its tickets differently from
  another without a second daemon.

### Fixed

- **A throttled duplicate no longer counts twice.** `duplicates` was bumped
  twice for every occurrence the aggregate rule held back: once by the merge
  assignment the pipeline persists (ActionAbortUpdate writes too — it only
  skips the `date_epoch` stamp) and once more by a queued atomic increment on
  the same two paths. With a real async writer the increment lands after the
  write, so a throttled duplicate added 2 — inflating the repeat counter on
  every aggregated alert (a production record read 197). Without an async
  writer the old fallback incremented synchronously *before* the write, which
  then overwrote it, which is why no test caught it. The counter now has one
  source of truth: the merge assignment. `queueIncrement` and the plugin's
  private `asyncWriterHost` interface are gone.
- **The anti-flapping budget refills per throttle window instead of once per
  record.** `flapping_countdown` was only ever decremented, never restored, so
  after `flapping` (default 3) watched-field changes in an aggregate's entire
  lifetime *every* later re-open or re-escalation was dropped as "flapping" —
  however many quiet hours sat in between. A nightly K8s alert on production
  had reached -9, so the transition that mattered (`ok => critical`, the alert
  starting to fire again) was silently held back, with a timeline note
  claiming notifications were stopped "until throttle expires (0s left)" —
  a window that had ended a day earlier. Worse, that path aborts without
  stamping `date_epoch`, so the throttle clock was not restarted either and
  the next plain repeat 14 seconds later sailed through as a context-free
  "New escalation". The countdown now refills whenever the aggregate has been
  quiet for a full throttle window (as the documentation always described) and
  floors at 0 instead of running away negative. Inside a window the cap is
  unchanged, and `throttle: -1` still means the budget never refills.
- **One JIRA ticket per alert again, instead of one per escalation.** The
  `snooze-jira` daemon recognises an alert it has already ticketed by reading
  the issue key back off the record, and every leg of that round-trip was
  broken. The daemon learned the action name — the key that names the record
  field holding the handle — only from a `snooze_action_name` query parameter
  that nothing ever set, so it looked under `response_unknown_action` and
  found nothing; snooze-server now sends `X-Snooze-Action-Name` on every
  webhook call. And the handle itself sat one level too deep: the daemon
  answers `{"<alert hash>": {"issue_key": …}}` — one entry per alert, in that
  shape even for a single alert — and `inject_response` stamped that envelope
  verbatim, below where every reader looks. The webhook notifier now stamps
  the record's own entry, and the readers (`plugins.NotifyRef`, the daemon's
  `findExistingIssue`) see through the old shape so records already stamped
  keep their ticket. Finally, the body never carried the handle in the first
  place: `{{ tojson .Record }}` — and the 1.x `{{ __self__ | tojson() }}` that
  rewrites to it — encoded the record with `encoding/json`, which drops
  `Record.Extra` (`json:"-"`), so every untyped field went missing from the
  request body, handles included. `tojson` now encodes a record through
  `plugins.MarshalRecord`, the same flattening the default body has always
  used. Observed in production as CG-1811 and CG-1812 opened two minutes
  apart for the same alert hash.
- **JIRA priorities are resolved from the live scheme instead of hardcoded
  English names.** Both the `jira` notifier and the `snooze-jira` daemon
  shipped a severity → priority map written in English (`critical: High`,
  `warning: Medium`, …). Priority names are localized per JIRA site and
  renamable by any admin, so on a site whose priorities are, say, `Critique /
  Grave / Moyen / Faible`, *every* create failed with
  `400 priority: the selected priority is invalid` and no ticket was ever
  opened. Snooze now reads the project's own priority scheme
  (`GET /issue/createmeta`, falling back to `GET /priority`), maps the
  severity onto a position in it, and sends the priority **id** — stable and
  language-independent. The scheme is cached per project (`priority_cache_ttl`,
  default 1h) and refetched with one retry when JIRA rejects a priority.
  `priority` / `priority_mapping` become optional overrides accepting an id or
  a name; a value that names nothing in the scheme is ignored rather than
  failing the create, and a severity outside Snooze's ladder now omits the
  field so JIRA applies its own default. New shared package
  `internal/jirapriority`.
- **Close transitions could be wedged open by a snooze filter.** A `discard`
  filter matching the close write for an existing aggregate dropped it
  entirely, leaving the alert open forever with its snooze attribution
  stripped. Close transitions of an existing aggregate now pass through all
  snooze filters unconditionally.
- **`general.ok_severities` was documented but unenforced.** The field was
  loaded and shown in the settings UI, but nothing in the Go port consumed
  it. It is now enforced centrally in the ingest pipeline: a record arriving
  without an explicit state whose (case-folded) severity is in the list gets
  `state: close` stamped before the plugin chain runs.
- **`kv` values could leak between tenants.** The kv plugin cached the whole
  collection in one flat `dict → key → value` map while the collection itself
  is tenant-scoped, so each per-tenant reload overwrote the cache wholesale and
  whichever tenant reloaded last served its values to every other tenant. A
  `KV_SET` rule modification could therefore resolve to another tenant's value
  — non-deterministically, since it depended on reload order. The cache is now
  keyed by tenant and every lookup names one. Notably the DB fallback that the
  cache exists to optimize was already tenant-correct, so the fast path had
  been the less safe of the two.
- **`kv` logged a reload failure every 5 minutes.** kv was the only
  tenant-scoped plugin whose `Reload` had no naked-context guard, so the
  tenant-less entry in the syncer's reload fan-out — deliberate, and needed by
  genuinely global collections — fail-closed with `auth: no tenant in context`
  and logged a warning on every safety tick and every kv write. It now skips a
  tenant-less reload like every other tenant-scoped plugin.
- **`kv` was only hydrated for the default tenant at boot.** Boot's per-tenant
  cache warm-up walked only the configured process plugins; kv has no `Process`
  method, so it was never in that list and its sole hydration was `PostInit`
  under the default-tenant seed context. Boot now also warms every `auto_reload`
  plugin outside the processor list, mirroring what the syncer does at runtime.
  A tenant whose bucket is not yet loaded falls back to the tenant-scoped
  database lookup rather than reporting every key as absent.
- **Live config reload was silently dead on MongoDB.** Editing a snooze filter,
  rule, aggregate rule, or notification took effect only after a
  `snooze-server` restart. The mongo change-stream watcher type-asserted
  `bson.M` on the nested fields of each event, but the driver decodes nested
  sub-documents as `bson.D` — so every event lost its `tenant_id`, the syncer
  reloaded under a tenant-less context, and every tenant-scoped plugin
  correctly treated that as "nothing to do". Nothing logged, and the
  hit-counter reload-storm filter was inert for the same reason. Change events
  now read those fields regardless of the driver's decode shape, and the test
  stub round-trips fixtures through real BSON so the gap cannot reopen.
  PostgreSQL and SQLite were unaffected (they stamp the tenant from the
  writer's context).

### Added

- **`snooze_bypass_severities`** added to the settings catalogue, so it is
  editable from the web UI (Settings → General) alongside `ok_severities`.
- **`syncer.reload_safety_interval`** (default `5m`): a periodic full reload of
  every plugin cache for every active tenant, backstopping change-event
  delivery so a lost or dropped event cannot leave a cache stale until the next
  restart. Set a negative duration to disable it.

### Changed

- `general.ok_severities` and `general.snooze_bypass_severities` are now read
  from the runtime settings store, falling back to the config file. Editing
  either from Settings → General applies live, without a restart.
- The syncer fans a tenant-less change event (a delete carries no full
  document) out to every active tenant instead of issuing one naked reload that
  tenant-scoped plugins skip — deleting a filter now takes effect without a
  restart.
- A burst of change events can no longer postpone a reload indefinitely: the
  debounce window still restarts per event but is capped at ten windows.
- The mongo bus logs when a subscriber's channel is full and an event is
  dropped (first drop, then every 100th). Previously this was the only
  unlogged failure point on the reload path.

### Added

- Re-escalation now reaches every notifier: jira/servicenow update the
  existing ticket, statuspage/opsgenie/pagerduty escalate onto the existing
  incident, chat outputs thread instead of reposting, and notifiers with no
  threading concept still escalate urgency in the message.
- Google Chat records its thread id so inbound slash-commands resolve back to
  the right alert.
- Sessions renew silently in the background instead of dying at token expiry;
  sign-out now revokes the refresh token.
- Alerts table: message-first layout, honest duplicate counts, prioritised
  mobile cards, manual refresh button, truncated-cell tooltips, snooze
  straight from the row.
- Dashboard leads with the noise-reduction story; counters are bucketed live
  at ingest instead of snapshotted.
- Keyboard triage and alert search in the command palette.
- Accessible, human failure states across the app; an aria-live announcer for
  polled screens.
- Visual refresh: split ack/closed hues, warm-paper light theme, unified
  lifecycle vocabulary across chips/tabs/tiles/legend/timeline, Close
  de-weighted in favour of Acknowledge, Flow chart always renders the Snooze
  stage with visible connectors.

### Fixed

- SQLite: `Contains`/`In` conditions no longer misfire `json_each` against a
  non-array field.
- Snooze filters evaluate oldest-first so first-match-wins is stable.
- "Create and apply" actually retro-applies now (the create response's shape
  was misread).
- Empty alert payloads are rejected instead of creating blank table rows.
- Various small UI honesty/accessibility fixes: no false "All clear" during
  an outage, no cropped severity/state badges, no stray scrollbar on the
  Alerts tab strip, absolute time on timeline hover, 3:1 form-control
  borders.
- Alerts table's Sev column no longer wraps the badge onto two lines.
- Condition editor: switching the operator (e.g. `contains` → `matches`, or
  via SEARCH, which masks the field) keeps the field and operand you already
  typed instead of clearing them.

## v2.4.0

### Added

- Federation is now a notification action. The standalone **Federation** admin
  page and the `forward` collection are removed; relaying to a Snooze peer is
  configured as a **Forward to another Snooze peer** (`snoozepeer`) action on a
  notification. Deployments with existing `forward` destinations convert them by
  running `snooze-server migrate forward-to-action` (a one-time, idempotent
  operator command — it is not run automatically at daemon startup). The
  `X-Snooze-Loop` loop-prevention contract and the per-node `syncer.hostname`
  requirement are unchanged.

- **Ingest kill-switch toggle.** A dedicated **Ingest** tab now appears in
  Settings with an **Alert intake enabled** switch. Disabling it halts all
  `POST /api/v1/alerts` requests and every webhook receiver (503 Service
  Unavailable) for the current tenant instantly, matching the documented
  maintenance-mode workflow. The card has a red left-border accent and a
  danger Save button; a warning caption appears when intake is paused.

- **API keys — usage display in the console.** The admin keys table now shows
  "Last used" (sortable, relative time) and "Uses" columns. The self-service
  profile card shows the last-used age and a "Stale" badge for keys idle for
  more than 30 days. The admin edit drawer shows a read-only usage summary.
  (Values are updated at most once per hour per the Plan 08 throttle.)

- **Tenant routing admin UI:** a new **Org matching** page under Admin lets operators manage
  attribute→tenant routing rules (group / domain / login → tenant slug) via create / edit / delete
  forms with columns for match type, match value, target tenant, and priority. A duplicate
  `(match type, match)` pair surfaces a conflict message. A new **Tenant routing** tab in Settings
  exposes the `tenant_match.fail_closed` safety toggle. The login page shows a hint when org
  auto-detection is active (`tenant_match_enabled: true` from the server).

- Groups are now manageable in the web console (Admin → Groups): create named cohorts, add/remove
  `{username, method}` member pairs, and assign roles to the group via the existing Roles editor.

- **Security Audit console.** A new "Security Audit" admin page
  (`/web/admin/audit`) lists all auth-event audit rows
  (`login`, `login_failed`, `token refresh`, `logout`) with columns for
  timestamp, action, username, method, and summary. Free-text filter via the
  standard condition search bar. The auth-action badge labels were also
  fixed — they previously rendered as `undefined` in the existing per-object
  audit timeline.

- **Heartbeats console page.** A new **Heartbeats** page (`/web/heartbeats`) in
  the **Configure** sidebar group lets operators manage dead-man's-switch
  heartbeats from the web console: create/edit/delete, browse with a live status
  badge (`ok` / `slow` / `overdue`), filter by status, and copy-paste the ping URL
  and token directly from the editor drawer. No backend change (requires Plans 06
  and 32 in `done/`).

- **Console branding is now consumed by the SPA:** `console.logo` renders in the sidebar and login
  screen (falling back to the bundled Snooze logo when empty), `console.title` drives the browser
  tab title (defaulting to `"Snooze"`), `console.audio` plays a cue when new alerts arrive during
  auto-refresh, `console.clipboard_template` formats the row copy action using `{{field}}`
  substitution (empty defaults to pretty-printed JSON), and `console.default_filter` pre-fills the
  alerts SearchBar on a clean load (URL `?search=` still overrides it per session).

- **"Select all N matching this filter" affordance** on the alerts action bar: when the total
  result count exceeds the visible page and rows are selected, a link expands the bulk scope
  beyond the visible page to every record matching the current tab and search query.
- **"Tag / set fields" button** in the alerts action bar opens a dialog for bulk-tagging or
  merging attributes across a selection (`POST /api/v1/record/bulk_update`); the success toast
  shows per-op counts (matched / set / tagged / untagged).
- **Shelve action now posts a `shelve` comment** with a configurable duration (default 4h
  from the dialog picker) instead of patching `ttl=-1`. Alerts automatically return to open
  when the duration expires (requires Plan 34 backend). A `ShelveDialog` duration picker
  (1h / 4h / 8h / 24h / 48h / Custom) replaces the old immediate toggle.
- **Legacy permanent-exempt action (`ttl=-1`)** preserved under **"Permanent exempt (legacy)"**
  in the row action menu, clearly labelled to prevent confusion with timed shelve.
- **Shelved tab** now includes alerts with `state=="shelved"` (new backend model) in addition
  to the legacy `ttl<0` predicate.
- **TTL column** shows `"returns in Xh Ym"` for timed-shelved alerts based on `shelve_until`.
- **Alerts table — action gating:** illegal state transitions (e.g. Acknowledge
  on an already-acked alert) are now hidden from the kebab menu, quick-action
  buttons, right-click context menu, and bulk toolbar; the backend 403 remains
  as a concurrent-change backstop.
- **Alerts table — lifecycle countdowns:** acked rows now show an "in Xh" expiry
  countdown when `ack_until` is set; open rows show "escalates in Xh" when
  `escalate_at` is armed.
- **Alerts table — trend badge:** a ↑/↓/— indicator in the severity column
  reflects `trend_indication` stamped by the aggregaterule plugin on every merge.
- **Comment timeline — system comments:** auto-generated comments from the
  housekeeper (`auto: true`) are attributed as "System (auto)" and cannot be
  edited or deleted.
- **Chat ack/close/re-open from Slack & Telegram message buttons.** The Slack
  and Telegram notifiers can now render opt-in interactive buttons
  (`interactive: true` on the action form; default off). New webhook receivers
  (`POST /api/v1/webhook/slack`, `POST /api/v1/webhook/telegram`) apply the
  pressed action through the Plan 05 transition guard — writing an attributed
  comment so the dashboard activity feed records it — and edit the chat message
  in place. Slack requests are authenticated by the v0 request signature
  (`slack_interactive.signing_secret`, constant-time HMAC-SHA256 with a 5-minute
  replay window); Telegram by the `X-Telegram-Bot-Api-Secret-Token` header
  (`telegram_interactive.secret_token`, constant-time). Both fail closed: an
  unset secret makes the receiver reject every request (401). An illegal move
  (e.g. ack of a closed alert) is refused and the chat reply states why.
- **OIDC provider presets + multiple simultaneous IdPs.** A new optional
  `provider:` key (`google`/`azure`/`cognito`/`keycloak`/`gitlab`) plus
  `provider_params` pre-fills the discovery `issuer` from a built-in table, so
  common providers no longer need a hand-typed issuer URL (an explicit `issuer`
  still wins). A new optional `oidc_providers.yaml` (a top-level list of OIDC
  entries) registers several identity providers at once — e.g. corporate Entra
  plus Google Workspace — each on its own `/api/v1/login/{method}` routes. The
  legacy single `oidc:` config is unchanged and used as a fallback when
  `oidc_providers` is empty, so existing deployments are unaffected.
  `client_secret` stays per-entry, file/env only (never from the DB). GitHub
  OAuth (no `id_token`) remains out of scope.
- **Timed shelve with auto-return.** Posting a `shelve` comment transitions an
  alert to `shelved` and stamps a `shelve_until` epoch. A new minute-cadence
  housekeeper sweep reverts any shelved alert past its deadline back to `open`
  and writes an auto-comment. Duration is operator-configurable via
  `housekeeping.shelve_timeout` (default 4h, live-editable in Settings).
- **PagerDuty inbound status sync.** A new webhook receiver at
  `/api/v1/webhook/pagerduty` maps `incident.acknowledge` → `State: "ack"` and
  `incident.resolve` → `State: "close"` back onto the originating Snooze record
  (identified by the `dedup_key` / `incident_key` echoed by PagerDuty).
  Unacknowledge and escalate events re-open the record. The existing PagerDuty
  outbound notifier is unchanged.
- **Heartbeat — latency / slow-ping detection.** Heartbeat documents now
  accept an optional `max_latency` (ms) field. When the ping URL includes
  `?sent_at=<unix-ms>`, the server records `last_latency = receive_time - sent_at`.
  If `last_latency > max_latency` while the heartbeat is still within its
  `interval + grace` window, a lower-severity `"slow"` alert is injected —
  giving an early-warning signal before the dead-man's switch fully expires.
  The `status` field (Plan 06) gains a third value: `"slow"`.
- **Snooze — suppression-bypass severities.** A new `general.snooze_bypass_severities`
  list (default empty) exempts records whose `severity` matches any entry from all
  snooze rules. Set it to e.g. `['ok', 'critical']` to ensure recovery and
  highest-priority events are never silenced by a maintenance window.
- **aggregaterule**: new record fields `previous_severity` and
  `trend_indication` (`moreSevere`/`lessSevere`/`noChange`) stamped on every
  aggregate merge. Severity escalations (`moreSevere`) now bypass the throttle
  window so a `warning` escalating to `critical` is never silently swallowed.
- **Attribute-based tenant routing.** A new global `tenant_match` registry lets
  operators map IdP groups, email domains, or login names to tenant slugs,
  removing the need for users to know or type their org slug at login. SSO
  (OIDC/SAML) and LDAP users who authenticate without an explicit `org` are
  routed to the matched tenant automatically; rules are evaluated by `priority`
  (first hit wins). The `tenant_match.fail_closed` runtime setting (default
  `false`) denies an unmatched user with `403` instead of landing them in the
  `default` tenant. CRUD at `/api/v1/tenant_match` (requires `rw_tenant`).
- **Server-driven web-console config.** `GET /api/v1/config` exposes org-wide
  defaults (alert-table columns, default filter, sort, auto-refresh interval,
  severity rank ladder, and branding: logo/title/new-alert audio/clipboard
  template) from the runtime `console` settings section, overlaid on the
  server's code defaults. The endpoint is public and read-only; the SPA fetches
  it at boot and falls back to its hardcodes when unavailable. Custom severities
  are placed by **rank** (`console.severity_ranks: {"p1": 2}`) and inherit
  theme-aware colours automatically — no per-label colour map. The Plan 21
  severity ladder is the single runtime source of truth (the frontend `RANK`
  map becomes an offline fallback). Editable from the admin Settings page
  (Console group).
- **Alerts — "Acked by" column in the alert list.** The operator who last
  acknowledged an alert is now stamped directly onto the record as `acked_by`
  by the comment plugin. The alert list displays this in a new "Acked by"
  column; the field is cleared automatically when the alert is re-opened or
  closed.
- **Stackdriver / Google Cloud Monitoring inbound receiver.** The new
  `stackdriver` plugin accepts GCP Monitoring incident webhook notifications
  at `/api/v1/webhook/stackdriver`, mapping incident state (`open` →
  critical, `acknowledged` → ack, `closed` → ok/close) to Snooze records.
  The optional `documentation.content` JSON blob overrides any record field
  before pipeline submission.
- Server-managed user groups (`GET/POST/PUT/PATCH/DELETE /api/v1/group`):
  operators can now create named cohorts, add local or LDAP users as members,
  and assign roles to the group without editing each user's `roles[]` field.
- **Snooze — window lifecycle status and remaining countdown.** The snooze list
  now shows a derived *Status* badge (active / pending / expired / always-on)
  and a *Remaining* countdown column for time-bounded rules. Both are computed
  server-side at read time from `time_constraints.datetime`; no migration
  required. The editor gains a **Silence for…** shortcut row (presets 1h / 4h /
  24h / 7d and a free-text field accepting "2h30m") that sets the absolute
  datetime window without navigating the date-picker.
- **Custom source mapping guide.** Operators can now onboard any JSON alert
  source without writing Go: post to `POST /api/v1/alerts` and use a rule tree
  to remap foreign field names to canonical Snooze fields. A new
  `_preserve_raw: true` ingest hint copies all unrecognised keys into the record's
  `raw` field before rules run, preserving the original payload for audit.
  See [Custom source mapping](docs/content/general/integrations/custom-source.md).
- Canonical severity vocabulary in `pkg/snoozetypes` (`DefaultSeverityRank`, `SeverityRank`, `SeverityVariant`, `NormalizeSeverity`, `CompareSeverity`), mirroring the frontend severity ladder. Enables severity-aware re-escalation (Plan 30), suppression bypass (Plan 31), and the server-driven console config (Plan 28).
- Added server-to-server alert federation: a hot-reloadable "forward" destinations collection relays accepted alerts to downstream Snooze/HTTP peers, with condition scoping, per-destination auth (bearer/basic/apikey), and X-Snooze-Loop loop prevention.
- SAML2 SP-initiated SSO: redirect-to-IdP, ACS endpoint, assertion→identity group mapping, and an SP metadata endpoint (new `saml` config section).
- Bulk operations across a query: POST /api/v1/record/bulk_state (ack/close/open/esc) and POST /api/v1/{plugin}/bulk_update (set/tag/untag) apply a mutation to every record matching a ?q condition in one call, with one audit row per affected record.
- Auth-proxy mode: trust an upstream reverse proxy (oauth2-proxy/Pomerium/mod_auth) to authenticate users via configurable username/groups headers, with optional JIT auto-signup, IP allowlist, and group-based role mapping (config: `auth_proxy.*`, disabled by default).
- **Timed alert lifecycle — acks now expire and stale alerts auto-escalate.** An acknowledged alert is stamped with a server-controlled `ack_until`; a new minute-cadence housekeeper sweep reverts expired acks back to `open` and, when `housekeeping.escalate_after` is set, flips an alert left unacknowledged past the deadline to `esc` and re-fires its notifications. Closes the gap where a one-shot acked alert was silenced forever. Both timeouts are live-editable in Settings.
- **Saved searches.** Operators can now bookmark named DSL filters from the
  Alerts page. Searches are stored per-user/per-tenant at
  `GET /api/v1/savedsearch` and apply instantly with a single click.
- **Metrics — live record-count gauge.** Added the `snooze_records` gauge
  (labelled by `state`) to the Prometheus `/metrics` endpoint. It reports the
  current number of records in the database grouped by state, summed across all
  tenants and refreshed on every scrape — graph open-alert backlog growth or
  alert when the queue exceeds a threshold.
- **Admin — on-demand housekeeping trigger.**
  `POST /api/v1/housekeeping/run` (requires `rw_all`) fires every registered
  cleanup job synchronously and returns per-job results, including errors and
  durations. `GET /api/v1/housekeeping/status` reports the number of
  registered jobs. Useful after data floods or to verify a retention change
  without restarting the server.
- **Version endpoint.** `GET /api/v1/version` (public, no token required) returns
  the compiled-in version string, git commit, and build date. Operators can use
  this to confirm which binary is running on each cluster node; the SPA can
  display the running version in the UI.
- **Ingest kill-switch.** A new `ingest.allow` runtime setting (default `true`)
  lets operators instantly halt all alert intake — both `POST /api/v1/alerts`
  and every webhook receiver — without a server restart. The switch is
  per-tenant and responds with `503 Service Unavailable` while disabled.
- **API keys — last-used tracking.** Each key now records `last_used_at`
  (Unix epoch) and a `use_count` (lower-bound, throttled to at most one
  write per hour) updated on every successful authentication. Both fields
  appear in the key-list response so stale machine keys are easy to identify
  and prune.
- **Alert ingest now stamps `source_ip`.** Every record posted to
  `POST /api/v1/alerts` receives a `source_ip` field (resolved client IP,
  honouring `X-Forwarded-For` / `X-Real-IP`). Caller-supplied values are
  preserved. Use the field in Rules to route or filter by collector origin.
- **Audit — structured auth-event trail.** Login, login-failed, token-refresh,
  and logout events are now written as queryable rows to the `audit` collection
  (`object_type: auth`) in addition to the existing HTTP request log lines.
  Retention follows the standard housekeeper `audit` TTL.
- **Reject-at-ingest policy processor.** A new `reject` plugin provides a
  CRUD-managed collection of condition-based policy rules. Alerts matching any
  enabled rule are aborted before persistence and the sender receives an HTTP
  422 `policy_rejected` response with the matching rule's name, replacing the
  previous silent discard behaviour.
- Reject rules are now managed in the web console under a new **Reject** tab on
  the Rules page (create/edit/enable/delete with the standard condition editor).
- **Graylog webhook receiver.** `POST /api/v1/webhook/graylog` ingests
  Graylog stream-alert HTTP notifications. `stream.title` becomes the record
  host; `check_result.result_description` the message. Query-string overrides
  `event`, `environment`, `service`, `severity`, and `event_type` mirror the
  Alerta Graylog webhook contract.
- **`pingdom`** — Pingdom uptime state-change webhook receiver. DOWN events
  produce `warning`/`critical` records; UP events close the prior DOWN via
  `State="close"`. Mounted at `/api/v1/webhook/pingdom`.
- **Heartbeat — computed `status` field on list/get responses.** `GET
  /api/v1/heartbeat` and `GET /api/v1/heartbeat/{uid}` now include a
  read-time `status` field (`ok` or `overdue`) on every heartbeat document.
  An optional `?status=` query parameter filters the list to heartbeats
  matching the supplied value(s). No database migration required.
- **Alert flow visibility: matched notifications + action outcomes.** Each
  processed alert now records the notification entries it matched
  (`record.notifications`) and the outcome of every action they fired
  (`record.actions`: `success`/`error`(with message)/`skipped`/`pending`/`sent`).
  The Alerts row-detail panel gains a **Flow** tab beside the Timeline,
  rendering the pipeline path (input → rules → aggregate, then a branch per
  matched notification with its own actions, or a terminal snooze box) with
  green/red action boxes — click a red box for the error. Action-outcome
  resolution is one merge-write per notifying alert, gated by the new
  `notification.persist_action_outcomes` flag (default `true`; disable on
  high-volume SQLite). The demo seed (`core.seed_demo`) now stamps these fields
  on its sample alerts too — the matched rules, the `Host and Message`
  aggregate, and the notifications/actions each critical alert fired (with one
  deliberately-failed Slack delivery on the escalated alert) — so the Flow panel
  is fully populated out of the box.
- **Inputs page** (Admin → Inputs): lists every supported alert input with its
  last-received time, a docs link, and a per-input "how to receive alerts" guide.
- `GET /api/v1/inputs`: per-source alert activity (max epoch + count within a
  windowed lookback), gated by `ro_stats`/`rw_stats`.

### Fixed

- **Permissions catalog — named `authorization_policy` grants are no longer
  omitted.** `GET /api/v1/permissions` now also walks the `read` + `write`
  lists of every plugin's `authorization_policy` (on `route_defaults` and on
  each per-path `routes` override), so the catalog never silently misses a
  permission string the authorizer actually honours. The implicit `any`
  sentinel (and the empty string) stays excluded. Response shape is unchanged
  — a sorted `{data: []string}`.
- **Alerts — state-transition comments now validated before they are saved.**
  Posting an `ack` comment to an already-acknowledged or closed alert, or a
  `close` comment to a closed alert, now returns a 403 with a clear error
  message instead of silently applying the nonsensical state change.
- **New Relic receiver — `acknowledged` alerts now land as `State: "ack"`.**
  A legacy webhook with `current_state: acknowledged` was previously ingested
  as a firing record (empty State), causing Snooze to re-notify despite the
  upstream ack. The legacy-receiver mapping now emits `State: "ack"`, which
  the notification pipeline suppresses and the aggregaterule plugin re-escalates
  if the ack lapses. A shared `receiverutil.MapLegacyState` helper is introduced
  for reuse by future receivers.
- **Web — the Rules tree is now mobile-responsive.** The drag-and-drop rule
  hierarchy was the one table that still scrolled sideways on a phone (the
  v2.3.0 mobile pass card-collapsed every `DataTable` but not the bespoke rules
  tree). Below a 640px container each rule now reflows into a labeled **card**
  — name as the title, Condition and Modifications as wrapped fields, and the
  select / expand / **+ Add** controls in a footer row (the add menu is now
  always visible on touch instead of hover-revealed). Reordering stays a
  desktop gesture: the drag handle is hidden on narrow screens. Pure
  container-query CSS — the ≥640px desktop layout is unchanged.

### Changed

- **Bulk alert actions (ack/close/re-escalate) now call `POST /api/v1/record/bulk_state` once
  for the entire selection** instead of one `POST /comment` per row; the success toast shows the
  matched/updated counts. The `comment` action still uses the per-record loop (bulk_state does not
  write per-alert activity entries).
- **Housekeeper** — expired API keys and refresh tokens are now purged hourly.
  The cadence is tunable via `housekeeping.cleanup_apikey` and
  `housekeeping.cleanup_refresh_token` (both default to `1h`).
- **Web — table rows are now text-selectable, with a context-menu Copy.**
  Drag-selecting text inside a table row no longer opens the row (the
  click-to-open is suppressed while a selection is active), so cell values can
  finally be highlighted and copied. Right-clicking a row with text selected
  shows a **Copy** entry at the top of the context menu that copies exactly the
  highlighted text. The redundant **Open** entry was removed from row context
  menus — clicking a row already opens it.

### Security

- **Fixed an authentication-bypass in auth-proxy mode (`auth_proxy.enabled=true`).** The
  `trusted_proxies` IP allowlist was evaluated against `ClientIP`, which reads the
  client-controllable `X-Forwarded-For` / `X-Real-IP` headers first. An attacker reaching Snooze
  directly could send `X-Forwarded-For: <a trusted-proxy IP>` together with `X-Forwarded-User: root`
  to satisfy the allowlist and be trusted as any user, with no token. The allowlist now matches the
  genuine TCP peer address (`RemoteAddr` captured by a new `CapturePeerIP` middleware mounted before
  chi's `RealIP`), which ignores all forwarding headers. Audit-log and ingested-record client-IP
  capture are unchanged (they still honor `X-Forwarded-For`). Only deployments that had explicitly
  enabled `auth_proxy` were affected; the mode is off by default.

## v2.3.0

### Added

- **Mobile-friendly web UI.** The SPA is now usable on phones down to 360px,
  optimized for on-call triage. Below 1024px the desktop sidebar shell is
  replaced by a thumb-reachable **bottom-tab bar** (Alerts · Dashboard ·
  Snoozes · Rules) plus a **More** sheet holding the rest of the navigation,
  theme toggle, and account actions; data tables collapse into labeled
  **cards**, editor drawers and the command palette open as **full-screen
  sheets**, and interactive controls meet the 44px touch-target minimum on
  coarse pointers. The ≥1024px desktop layout is unchanged. Implemented as a
  pure CSS/container-query retrofit (new `--bp-sm/md/lg`, `--touch-target`,
  `--safe-bottom` tokens; one `useIsMobileShell` hook) — no new dependencies.
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
