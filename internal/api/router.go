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

	// --- snooze retro-apply (mounted BEFORE plugin CRUD so the more
	//     specific `/{uid}/retro_apply` POST wins over the generic
	//     `/{uid}` handlers chi installs) ----------------------------------
	rt.mountSnoozeRetro(r)

	// --- plugin CRUD -------------------------------------------------------
	for _, p := range rt.Plugins {
		plugins.MountCRUD(r, rt.Host, p)
	}

	// --- web UI ------------------------------------------------------------
	rt.mountStatic(r)

	return r
}

// skipAuth returns the SkipPredicate used by the Auth middleware. The result
// honours rt.SkipAuthPaths when set, else applies the canonical defaults.
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
			"/", "/web", "/web/",
		}
	}
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
