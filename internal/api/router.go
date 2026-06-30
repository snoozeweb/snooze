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
	"github.com/snoozeweb/snooze/internal/housekeeper"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
)

// AlertProcessor is the narrow surface routes_alert needs from the server's
// core. We keep it interface-shaped so the API package does not import
// internal/core (avoiding the import cycle internal/core → internal/api).
type AlertProcessor interface {
	ProcessRecord(ctx context.Context, rec map[string]any) (map[string]any, plugins.Action, error)
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
	Refresh refreshIssuer
	// APIKeys is the user API-key store. When non-nil, a snz_-prefixed Bearer
	// token is authenticated via it in the Auth middleware. Nil disables key
	// auth (the JWT path is unaffected).
	APIKeys *auth.APIKeyStore
	// ProxyAuth provisions/resolves trusted-header (auth-proxy) users. It is
	// consulted by the Auth middleware ONLY when Config.AuthProxy.Enabled is
	// true; when the mode is disabled (the default) it is ignored and the
	// middleware chain is byte-identical to the no-proxy path. Concrete type at
	// runtime is *auth.ProxyAuthenticator.
	ProxyAuth       middleware.ProxyAuth
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
	// TenantResolver maps ingest tokens to tenant slugs for the IngestTenant
	// middleware on /api/v1/webhook/* and /api/v1/alerts. Nil means every
	// unauthenticated request lands in the default tenant (zero-config single-org).
	TenantResolver *middleware.TenantResolver
	// TenantChecker verifies that the resolved ingest tenant is not suspended.
	// Nil disables the check (tests; single-tenant deploys without the tenant plugin).
	TenantChecker middleware.TenantStatusChecker
	// TenantMatch resolves an authenticated SSO/LDAP Identity to a tenant slug
	// from the global tenant_match registry, used by the login handlers when the
	// user did not supply an explicit non-default org (Plan 29). Nil means the
	// feature is not wired: resolveTenantFromAttributes then returns the
	// requested org (or DefaultTenant) unchanged — existing login behaviour is
	// byte-identical.
	TenantMatch *auth.TenantMatchResolver
	// IngestAllowed is the runtime kill-switch: it reports whether alert intake
	// is currently permitted for the tenant in the request context. When it
	// returns false, POST /api/v1/alerts and every webhook receiver respond
	// 503 immediately. Nil disables the switch (intake always allowed) — kept
	// as a narrow func so this package need not import internal/config for it.
	IngestAllowed func(context.Context) bool
	// HK is the housekeeper; when non-nil POST /api/v1/housekeeping/run fires
	// all registered jobs on demand and GET /api/v1/housekeeping/status reports
	// the registered-job count. Nil disables both endpoints (they return 503).
	HK *housekeeper.Housekeeper
	// RuntimeStore is the live-editable settings store consulted by the public
	// GET /api/v1/config endpoint to overlay the runtime `console` section onto
	// the code defaults. We hold the narrow config.RuntimeStore interface (not
	// the concrete settings plugin) so this package never imports
	// internal/pluginimpl. Nil ⇒ /api/v1/config returns pure code defaults.
	RuntimeStore config.RuntimeStore
}

// Build assembles the chi router with the canonical middleware chain. The
// returned chi.Router is ready to be passed to http.ListenAndServe.
//
// Chain order:
//
//	RequestID → CapturePeerIP → RealIP → Recoverer → Trace → Audit → CORS → Auth (skip *)
func (rt *Router) Build() chi.Router {
	r := chi.NewRouter()

	// 1. RequestID first so every later middleware sees the id.
	r.Use(middleware.RequestID())
	// 1.5. CapturePeerIP stashes the genuine TCP peer (RemoteAddr from the
	// socket) BEFORE RealIP overwrites it from the spoofable X-Forwarded-For/
	// X-Real-IP/True-Client-IP headers. This ordering is load-bearing: the
	// auth-proxy trust gate reads PeerIP, so capturing here is what makes the
	// trusted_proxies allowlist immune to header spoofing. Unconditional and
	// cheap (a context write); harmless when auth-proxy is off.
	r.Use(middleware.CapturePeerIP)
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
	var keys middleware.APIKeyAuthenticator
	if rt.APIKeys != nil {
		keys = rt.APIKeys
	}
	// Auth-proxy (trusted-header) mode is gated behind an explicit enable: when
	// off (the default) we keep the exact existing middleware.Auth call path so
	// behaviour is byte-identical. When on, AuthWithProxy adds the trusted-header
	// branch in front of the unchanged Bearer/snz_/JWT path.
	if rt.Config != nil && rt.Config.AuthProxy.Enabled && rt.ProxyAuth != nil {
		r.Use(middleware.AuthWithProxy(rt.Auth, keys, rt.ProxyAuth, &rt.Config.AuthProxy, skip))
	} else {
		r.Use(middleware.Auth(rt.Auth, keys, skip))
	}

	// --- public endpoints (skip filter above lets them through) -------------
	rt.mountHealth(r)
	rt.mountHousekeeping(r)
	rt.mountMetrics(r)
	rt.mountLogin(r)

	// --- platform-gated tenant registry -----------------------------------
	rt.mountTenant(r)

	// --- versioned endpoints -----------------------------------------------
	rt.mountAlerts(r)
	rt.mountSchema(r)
	rt.mountPermissions(r)
	rt.mountMetadata(r)
	rt.mountCondition(r)
	rt.mountConfig(r)

	// --- snooze retro-apply (mounted BEFORE plugin CRUD so the more
	//     specific `/{uid}/retro_apply` POST wins over the generic
	//     `/{uid}` handlers chi installs) ----------------------------------
	rt.mountSnoozeRetro(r)

	// --- bulk operations across a query (mounted BEFORE plugin CRUD for the
	//     same reason as retro-apply: the specific `/record/bulk_state` and
	//     `/{plugin}/bulk_update` POSTs must win over the generic `/{uid}`
	//     handlers chi installs). -------------------------------------------
	rt.mountBulk(r)

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
		// The tenant registry is mounted explicitly above (mountTenant) with
		// platform-scope + literal rw_tenant/ro_tenant gating. Because that
		// mount bypasses the generic CRUD path, it also bypasses the tenant
		// plugin's AfterCreate hook; handleTenantCreate therefore seeds the new
		// tenant's default roles + init_db marker itself (seedTenant), and
		// handleTenantDelete suspends-then-cascade-purges every tenant-scoped
		// collection before removing the registry doc (see routes_tenant.go).
		// Skip the generic CRUD mount here to avoid a duplicate
		// /api/v1/tenant registration (chi panics on double Mount).
		if p.Name() == auth.TenantCollection {
			continue
		}
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
			"/api/v1/version",
			// /api/v1/config is the public, read-only web-console defaults
			// document. The login screen needs branding (logo/title) before
			// a token exists, and the blob is presentation-only.
			"/api/v1/config",
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
		// 1. Legacy shared-secret gate (backward compat, optional). A no-op
		//    unless config.ingest.token is set (middleware.IngestToken), so
		//    existing unauthenticated receivers keep working by default.
		ingestToken := ""
		if rt.Config != nil {
			ingestToken = rt.Config.Ingest.Token
		}
		sub.Use(middleware.IngestToken(ingestToken))

		// 2. Per-tenant token resolution — always applied, even when no
		//    TenantResolver is wired (nil resolver falls back to DefaultTenant).
		resolver := rt.TenantResolver
		if resolver == nil {
			resolver = middleware.NewTenantResolver()
		}
		sub.Use(middleware.IngestTenant(resolver, rt.TenantChecker))

		// 3. Runtime kill-switch: reject every receiver with 503 while ingest
		//    is disabled for the tenant in context. Applied after IngestTenant
		//    so the tenant is resolved before the per-tenant flag is read.
		if rt.IngestAllowed != nil {
			sub.Use(middleware.IngestAllow(rt.IngestAllowed))
		}

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
