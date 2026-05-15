// Package plugins defines the plugin interfaces, registry, metadata model,
// generic in-memory cache, and the REST CRUD mounter that every Snooze plugin
// composes from. Concrete plugins live under internal/pluginimpl/<name>/.
package plugins

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/japannext/snooze/pkg/snoozetypes"
)

// Plugin is the base contract every Snooze plugin satisfies. A plugin can
// additionally implement one or more of the optional interfaces in this file
// (Processor, Notifier, Action, WebhookReceiver, DataModel, RouteProvider,
// LifecycleHook) to participate in the corresponding subsystem.
type Plugin interface {
	// Name returns the stable plugin identifier (matches Metadata().Name and
	// the registry key). Must be a valid URL path segment.
	Name() string
	// Metadata returns the static descriptor parsed from metadata.yaml.
	Metadata() Metadata
	// PostInit is called once after the plugin has been registered, the Host
	// is wired in, and all other plugins have been instantiated. It is the
	// canonical place to fetch initial data from the database.
	PostInit(ctx context.Context, host Host) error
	// Reload is invoked when the syncer reports an external change to the
	// plugin's collection. Implementations refresh their in-memory cache.
	Reload(ctx context.Context) error
}

// Processor plugins participate in the alert-processing pipeline. Process is
// called once per record per ordered plugin until one returns a non-continue
// Action.
type Processor interface {
	Plugin
	// Process inspects/mutates the record. The returned Result.Record replaces
	// the in-flight record for the remainder of the pipeline.
	Process(ctx context.Context, rec snoozetypes.Record) (Result, error)
}

// Notifier plugins deliver outbound notifications (mail, webhook, chat …).
type Notifier interface {
	Plugin
	// Send dispatches a single notification rendered from the payload.
	Send(ctx context.Context, rec snoozetypes.Record, payload NotificationPayload) error
}

// Actioner plugins are notifier-like targets that the notification engine can
// invoke directly with form parameters supplied by the user. The name is
// deliberately not `Action` to avoid colliding with the pipeline-verdict
// `Action` int type in result.go.
type Actioner interface {
	Plugin
	// Execute runs the configured action against rec.
	Execute(ctx context.Context, rec snoozetypes.Record, opts ActionOpts) error
}

// WebhookReceiver plugins expose an inbound HTTP endpoint that maps a foreign
// alert source (Alertmanager, Grafana …) to a Snooze record.
type WebhookReceiver interface {
	Plugin
	// WebhookPath returns the route fragment mounted under /api/v1/webhook/.
	WebhookPath() string
	// HandleWebhook serves the inbound POST.
	HandleWebhook(w http.ResponseWriter, r *http.Request)
}

// DataModel plugins own a typed collection and may validate incoming bodies
// before they hit the database.
type DataModel interface {
	Plugin
	// Schema returns the JSON Schema describing valid objects. The return
	// type is `any` so this package does not pin a particular jsonschema
	// library; consumers cast as needed.
	Schema() any
	// Validate runs structural validation against a decoded JSON object.
	Validate(obj map[string]any) error
}

// RouteProvider plugins mount custom HTTP routes instead of (or in addition
// to) the generic CRUD endpoints.
type RouteProvider interface {
	Plugin
	// RegisterRoutes is called with a chi.Router scoped to /api/v1/{name}.
	RegisterRoutes(r chi.Router, host Host)
}

// LifecycleHook plugins run background goroutines tied to the server lifecycle.
type LifecycleHook interface {
	Plugin
	// Start launches background work. It must return promptly; long-running
	// work belongs to a goroutine the implementation owns.
	Start(ctx context.Context) error
	// Stop signals shutdown and blocks until clean.
	Stop(ctx context.Context) error
}

// CreateHook plugins run side effects after a successful POST /api/v1/{name}
// create. The hook fires after the underlying DB write has succeeded; it must
// not retry the create. Use cases: cascading updates onto other collections.
type CreateHook interface {
	Plugin
	// AfterCreate runs once per create request, receiving every document that
	// was just written. An error from AfterCreate is logged via host.Logger
	// but does not roll back the create — the side-effect is best-effort.
	AfterCreate(ctx context.Context, docs []map[string]any) error
}

// PrimaryKeyer plugins advertise a natural primary-key field set that the
// generic CRUD createHandler uses to enforce duplicate-policy at the DB
// layer. Returning an empty slice means "no extra primary beyond uid".
//
// Mirrors metadata.yaml's route_defaults.primary in the Python codebase.
type PrimaryKeyer interface {
	Plugin
	PrimaryKey() []string
}

