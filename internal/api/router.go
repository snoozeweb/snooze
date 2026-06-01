package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
)

// AlertProcessor is the narrow surface routes_alert needs from the server's
// core. We keep it interface-shaped so the API package does not import
// internal/core (avoiding the import cycle internal/core → internal/api).
type AlertProcessor interface {
	ProcessRecord(ctx context.Context, rec map[string]any) (map[string]any, error)
}

// Router assembles the chi router used by the snooze-server binary. Fields
// are wired by the caller before Build() is invoked; nil values are
// permitted for tests (the corresponding middleware/routes degrade
// gracefully).
type Router struct {
	Auth *auth.TokenEngine
	// Refresh is the refresh-token store used by the /api/v1/login/refresh
	// and /api/v1/login/logout endpoints. Concrete type at runtime is
	// *auth.RefreshTokenStore; the interface narrows the dependency for
	// route tests. Nil disables both endpoints (logout still returns 204).
	Refresh         refreshIssuer
	Plugins         map[string]plugins.Plugin
	Host            plugins.Host
	DB              db.Driver
	Logger          *slog.Logger
	AuditLog        *slog.Logger
	Metrics         *telemetry.Registry
	MetricsGatherer prometheus.Gatherer
	Tracer          trace.Tracer
	Config          *config.Config
	Providers       *auth.Registry
	// Processor is the alert ingestion callback. Nil disables /alerts.
	Processor AlertProcessor
	// CORSConfig overrides the default CORS rules; zero value uses defaults.
	CORSConfig *middleware.CORSConfig
	// AuditExcludedPaths silences audit log lines whose path has one of
	// these prefixes (defaults: /metrics, /healthz, /readyz).
	AuditExcludedPaths []string
	// SkipAuthPaths receives the paths that must bypass the auth middleware
	// (defaults: /healthz, /readyz, /metrics, /api/v1/login, OPTIONS).
	SkipAuthPaths []string
	// WebFS serves the static web UI; nil leaves /web disabled.
	WebFS http.FileSystem
}

// Build assembles the chi router with the canonical middleware chain. The
// returned chi.Router is ready to be passed to http.ListenAndServe.
//
// Chain order:
//
//	RequestID → RealIP → Recoverer → Trace → Audit → CORS → Auth (skip *)
func (rt *Router) Build() chi.Router {
	r := chi.NewRouter()

	// 1. RequestID first so every later middleware sees the id.
	r.Use(middleware.RequestID())
	// 2. RealIP from chi — populates RemoteAddr from X-Forwarded-For/X-Real-IP.
	r.Use(chimw.RealIP)
	// 3. Recoverer wraps everything below so a panic still emits an envelope.
	r.Use(middleware.Recoverer(rt.Logger, rt.Metrics))
	// 4. Trace covers downstream handlers.
	r.Use(middleware.Trace("snooze-api"))
	// 5. Audit log (uses a separate slog logger from the operational one).
	auditLog := rt.AuditLog
	if auditLog == nil {
		auditLog = rt.Logger
	}
	excluded := rt.AuditExcludedPaths
	if excluded == nil {
		excluded = []string{"/metrics", "/healthz", "/readyz"}
	}
	r.Use(middleware.Audit(auditLog, excluded))
	// 6. CORS.
	corsCfg := middleware.DefaultCORS()
	if rt.CORSConfig != nil {
		corsCfg = *rt.CORSConfig
	}
	r.Use(middleware.CORS(corsCfg))
	// 7. Auth — last so the request_id/trace are already set on context.
	skip := rt.skipAuth
	r.Use(middleware.Auth(rt.Auth, skip))

	// --- public endpoints (skip filter above lets them through) -------------
	rt.mountHealth(r)
	rt.mountMetrics(r)
	rt.mountLogin(r)

	// --- versioned endpoints -----------------------------------------------
	rt.mountAlerts(r)
	rt.mountSchema(r)
	rt.mountPermissions(r)
	rt.mountMetadata(r)
	rt.mountCondition(r)

	// --- snooze retro-apply (mounted BEFORE plugin CRUD so the more
	//     specific `/{uid}/retro_apply` POST wins over the generic
	//     `/{uid}` handlers chi installs) ----------------------------------
	rt.mountSnoozeRetro(r)

	// --- self-service /api/v1/user/me/* (mounted BEFORE the user plugin's
	//     CRUD so /me/password wins over the generic /{uid} matcher chi
	//     would otherwise route to). ----------------------------------------
	rt.mountUser(r)

	// --- action test-send (mounted BEFORE plugin CRUD so the static
	//     /api/v1/action/test segment wins over the action plugin's generic
	//     /{uid} routes). -----------------------------------------------------
	rt.mountActionTest(r)

	// --- webhook receivers (must precede plugin CRUD so the path-specific
	//     /api/v1/webhook/{name} mount wins over a generic CRUD route the
	//     same plugin might otherwise register at /api/v1/{name}). -----------
	rt.mountWebhooks(r)

	// --- plugin CRUD -------------------------------------------------------
	for _, p := range rt.Plugins {
		plugins.MountCRUD(r, rt.Host, p)
	}

	// --- web UI ------------------------------------------------------------
	rt.mountStatic(r)

	return r
}

// skipAuth returns the SkipPredicate used by the Auth middleware. The
// effective skip list is the union of:
//
//   - rt.SkipAuthPaths (operator override) OR the canonical defaults
//   - every plugin whose metadata RouteDefaults.Authentication is an
//     explicit `false` — that plugin's CRUD subtree (/api/v1/{name}) is
//     treated as public, matching 1.5.0's per-route `authentication = False`.
//
// Path matching is exact OR prefix-with-trailing-slash, so adding
// "/api/v1/webhook" skips "/api/v1/webhook/anything" but not
// "/api/v1/webhookfoo".
func (rt *Router) skipAuth(r *http.Request) bool {
	if r.Method == http.MethodOptions {
		return true
	}
	paths := rt.SkipAuthPaths
	if paths == nil {
		paths = []string{
			"/healthz", "/readyz", "/metrics",
			"/api/v1/login",
			"/api/v1/health",
			// /api/v1/alerts is the generic record-ingest endpoint
			// (1.5.0 AlertRoute had `authentication = False`).
			// Anything that POSTs alerts — internal jobs, lightweight
			// integrations without a webhook-receiver plugin — should
			// not need a Bearer token.
			"/api/v1/alerts",
			"/", "/web", "/web/",
		}
	}
	// Plugins that declared `authentication: false` extend the public set.
	paths = append(paths, rt.pluginPublicPaths()...)
	for _, p := range paths {
		if r.URL.Path == p || strings.HasPrefix(r.URL.Path, p+"/") {
			return true
		}
	}
	// Web UI assets — anything under /web/ (handled by SPA) is public.
	if strings.HasPrefix(r.URL.Path, "/web/") || r.URL.Path == "/" {
		return true
	}
	return false
}

// pluginPublicPaths walks rt.Plugins and returns the path prefixes that should
// bypass the Bearer-token middleware. Two mount points are resolved
// independently so a plugin can mix authenticated CRUD with a public ingest
// path:
//
//   - CRUD subtree (/api/v1/{name}) is public when the plugin-level default
//     (ResolveRoute("")) is `authentication: false`.
//   - A webhook receiver's mount (/api/v1/webhook + WebhookPath()) is public
//     when ResolveRoute(WebhookPath()) is `authentication: false` — that
//     consults the per-path Routes override and falls back to RouteDefaults.
//     This is what lets the heartbeat plugin keep its `heartbeat` collection
//     authenticated while exposing a public ping at /api/v1/webhook/heartbeat.
func (rt *Router) pluginPublicPaths() []string {
	var out []string
	for name, p := range rt.Plugins {
		meta := p.Metadata()
		if route := meta.ResolveRoute(""); route.Authentication != nil && !*route.Authentication {
			out = append(out, "/api/v1/"+name)
		}
		if wr, ok := p.(plugins.WebhookReceiver); ok {
			if wp := wr.WebhookPath(); wp != "" {
				if route := meta.ResolveRoute(wp); route.Authentication != nil && !*route.Authentication {
					out = append(out, "/api/v1/webhook"+wp)
				}
			}
		}
	}
	return out
}

// mountWebhooks wires every plugins.WebhookReceiver at
//
//	POST /api/v1/webhook/{plugin}
//
// The route fragment returned by WebhookPath() is treated as a sub-path
// under the plugin name (e.g. `/alertmanager` ⇒ /api/v1/webhook/alertmanager).
// Auth is enforced by the same AuthorizeCRUD middleware that wraps CRUD
// subrouters; combined with `authentication: false` in the plugin's
// metadata.yaml that mirrors 1.5.0 where `WebhookRoute.authentication = False`
// + `authorization_policy.write: [any]` made these endpoints public.
func (rt *Router) mountWebhooks(r chi.Router) {
	r.Route("/api/v1/webhook", func(sub chi.Router) {
		// Optional shared-secret gate for ALL inbound webhook traffic. A
		// no-op unless config.ingest.token is set (middleware.IngestToken),
		// so existing unauthenticated receivers keep working by default.
		ingestToken := ""
		if rt.Config != nil {
			ingestToken = rt.Config.Ingest.Token
		}
		sub.Use(middleware.IngestToken(ingestToken))

		for name, p := range rt.Plugins {
			wr, ok := p.(plugins.WebhookReceiver)
			if !ok {
				continue
			}
			meta := p.Metadata()
			meta.PluginName = name
			path := wr.WebhookPath()
			if path == "" {
				continue
			}
			// Per-route authorize middleware so each receiver's
			// authentication flag and authorization_policy are resolved for
			// its specific webhook path (not the plugin-wide default).
			sub.With(plugins.AuthorizeRoute(meta, path)).Post(path, wr.HandleWebhook)
		}
	})
}
