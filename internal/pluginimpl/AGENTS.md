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
natural key for upserts) and `WriteTransformer` (rewrite a document before
persist — e.g. the `user` plugin hashing a password).

---

## Talking to the rest of the server

Everything a plugin needs comes from the `plugins.Host` captured in `PostInit`:
`DB() db.Driver`, `Bus()`, `Logger()`, `Tracer()`, `Metrics()`, `Config()`,
`RuntimeSettings()`, `Plugin(name)`.

* **Storage** is `host.DB()` → `db.Driver`. Import `internal/db` for its value
  types (`db.Document`, `db.Page`, `db.WriteOptions`). **Never** import a
  concrete driver (`internal/db/{mongo,postgres,sqlite}`) from production plugin
  code — see `internal/db/AGENTS.md`.
* **Don't** reach for a global, a raw `time.Now()` in pipeline-hot paths (use
  the injected clock), or `panic` for recoverable errors.

---

## The catalog & its taxonomy

The 43 plugins fall into roles (mirrored as comment groups in `all/all.go`):

| Role | Count | Examples |
|------|------:|----------|
| Data models (CRUD collections) | 12 | record, user, role, kv, widget, settings |
| Pipeline processors            | 3  | rule, aggregaterule, snooze |
| Notifiers (outbound)           | 16 | slack, mail, telegram, pagerduty, webhook |
| Webhook receivers (inbound)    | 10 | grafana, alertmanager, datadog, prometheus |
| Multi-role                     | 2  | heartbeat (webhook+model), notification (notifier+processor) |

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
