# AGENTS.md — plugins

> Scope: authoring plugins under `internal/pluginimpl/`. For repo-wide rules
> and architecture read `../../AGENTS.md` first — it wins on any conflict.
> The plugin **framework** (registry, CRUD mounter, interfaces) lives next door
> in `internal/plugins/`; this guide is about writing concrete plugins.

A plugin is one package that registers itself at import time. `snooze-server`
blank-imports the whole set via `internal/pluginimpl/all`. **Copy the nearest
existing plugin of the same shape and adapt it** — don't invent the plumbing.

---

## The recipe

```
internal/pluginimpl/<name>/
├── plugin.go        # //go:embed metadata.yaml + init() → plugins.Register
├── plugin_test.go   # at minimum: a CRUD/round-trip test + Metadata()
└── metadata.yaml    # static UI/descriptor metadata, embedded into the binary
```

Registration is always the same (`record` is the canonical minimal example):

```go
//go:embed metadata.yaml
var metaYAML []byte

func init() { plugins.Register("<name>", metaYAML, factory) }

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}
```

Every plugin implements `plugins.Plugin`: `Name()`, `Metadata()`,
`PostInit(ctx, host)` (capture the `plugins.Host`), `Reload(ctx)`. Then add the
capability interfaces you need.

---

## Pick your interface(s) (`internal/plugins/plugin.go`)

| Interface         | Implement when…                                          |
|-------------------|----------------------------------------------------------|
| `Plugin`          | Always — `Name`, `Metadata`, `PostInit`, `Reload`.       |
| `DataModel`       | Plugin owns a CRUD-able collection (`Schema`, `Validate`).|
| `Processor`       | Plugin transforms/gates alerts in the pipeline.          |
| `Notifier`        | Plugin delivers to an external destination (chat, mail…).|
| `Actioner`        | Plugin exposes a user-triggered button in the UI.        |
| `WebhookReceiver` | Plugin accepts inbound HTTP (mounts `/api/v1/webhook/{name}` via `WebhookPath`/`HandleWebhook`). |
| `RouteProvider`   | Plugin mounts custom chi routes.                         |
| `LifecycleHook`   | Plugin needs Start/Stop (background workers).            |

`DataModel` plugins can additionally opt into refinements: `CreateHook` /
`UpdateHook` / `DeleteHook` (post-write side effects), `PrimaryKeyer` (declare a
natural key for upserts), `WriteTransformer` (rewrite a document before
persist — e.g. the `user` plugin hashing a password), and the **pre-write veto
guards** `WriteGuard` (`GuardWrite(ctx, uid, doc, replace)` — `replace` is true
on PUT, false on POST/PATCH; non-nil error → 403) and `DeleteGuard`
(`GuardDelete(ctx, uids)` — protects must-not-vanish rows, e.g. the last
platform admin).

> `Actioner` has **no concrete implementation** in `internal/pluginimpl/` yet,
> so there is no "copy the nearest plugin" example for it — implementing one is
> breaking new ground. `UpdateHook.AfterUpdate`'s third argument is the *full*
> document on PUT but only the *partial* on PATCH — don't assume it lists only
> changed fields.

---

## Talking to the rest of the server

Everything a plugin needs comes from the `plugins.Host` captured in `PostInit`:
`DB() db.Driver`, `Logger()`, `Tracer()`, `Metrics()`, `Config()`,
`Plugin(name)` (plus `Bus()`, which today only exposes `Close()` — no plugin
uses it). **`RuntimeSettings()` is *not* a `Host` method** — it lives on the
optional `plugins.RuntimeSettingsHost` interface; type-assert
`host.(plugins.RuntimeSettingsHost)` and handle absence (it is nil in tests and
the migration tool). The `settings` plugin is the canonical user.

* **Storage** is `host.DB()` → `db.Driver`. Import `internal/db` for its value
  types (`db.Document`, `db.Page`, `db.WriteOptions`). **Never** import a
  concrete driver (`internal/db/{mongo,postgres,sqlite}`) from production plugin
  code — see `internal/db/AGENTS.md`.
* **Don't** reach for a global, a raw `time.Now()` in pipeline-hot paths (use
  the injected clock), or `panic` for recoverable errors.

---

## Framework behaviours worth knowing

Non-obvious rules baked into `internal/plugins` that bite plugin authors:

* **Generic CRUD is gated on `DataModel`.** `MountCRUD` mounts the REST
  endpoints (`/api/v1/<name>`, `/search`, `/{uid}`, …) **only** for plugins
  implementing `DataModel`. A pure `Notifier`/`Actioner`/`WebhookReceiver` gets
  no CRUD surface (its config lives in the shared `action` collection); combine
  it with `DataModel` to re-enable CRUD.
* **Auditing is on by default.** `Metadata` defaults `audit: true`, so every
  CRUD write on a data-model emits an audit event
  (`object_type/object_id/action/username/method/summary/date_epoch`). Noisy
  collections opt out with `audit: false` (audit, comment, kv do).
* **`search_fields:` in `metadata.yaml` is load-bearing.** Declaring it makes
  `Build` call `Driver.CreateIndex` per collection — registering the fields for
  the SearchBar's bare-word `SEARCH` operator and creating a backing index.
  Without it, `SEARCH` over that collection is ill-defined (and differs per
  backend — see `internal/db/AGENTS.md`).
* **Dashboard stats.** A `Processor`/`Notifier` feeds dashboard time-series via
  `plugins.RecordStat(host, epoch, metric, labels, n)` (needs the optional
  `AsyncWriterHost`); see `snooze` / `notification` / `aggregaterule` emitting
  `alert_snoozed` / `notification_sent` / `alert_throttled`.
* **`Build` order.** Factories then `PostInit` run in lexicographic
  plugin-name order with no dependency graph, and `Build` runs once per process.
  A `PostInit` cannot assume a sibling's `PostInit` has already run
  (`host.Plugin(name)` returns the instance, possibly pre-`PostInit`).

---

## The catalog & its taxonomy

The 46 plugins fall into roles by the capability interfaces they implement
(grouped the same way in `all/all.go`). Counts are once-per-plugin — a
multi-role plugin is counted only under "Multi-role":

| Role | Count | Examples |
|------|------:|----------|
| Data models (CRUD collections) | 13 | record, user, role, kv, widget, settings, action, audit, comment, environment, profile, stats, tenant |
| Pipeline processors            | 3  | rule, snooze, notification |
| Notifiers (outbound)           | 18 | slack, mail, telegram, pagerduty, webhook, discord, googlechat, mattermost, ntfy, opsgenie, patlite, pushover, script, servicenow, sns, statuspage, teams, twilio |
| Webhook receivers (inbound)    | 10 | grafana, alertmanager, datadog, prometheus, azuremonitor, cloudwatch, influxdb2, kapacitor, newrelic, sentry |
| Multi-role                     | 2  | aggregaterule (model+processor), heartbeat (model+webhook+lifecycle) |

(`notification` is a **processor**, not a notifier — outbound delivery is done
by the `Notifier` plugins it dispatches to; its only `Send` methods are test
fakes. Total: 13 + 3 + 18 + 10 + 2 = 46.)

When you add a plugin, drop its blank import into the matching group in
`internal/pluginimpl/all/all.go` (the grouping is documentation only — order
doesn't affect registration).

---

## Done = registered + tested + documented

1. `plugin_test.go` covers the round-trip (CRUD for data-models; encode/decode
   + mapping for notifiers/receivers) and `Metadata()`. A plugin's `_test.go`
   **may** import a concrete DB backend to exercise against a real store.
2. Blank-imported in `all/all.go`.
3. **Documented**: a new integration gets a page at
   `../../docs/content/general/integrations/<name>.md` (see `docs/AGENTS.md`);
   a new inbound route is also reflected in `../../api/openapi.yaml`.
4. `task go:vet && task go:test && task go:lint` green.
