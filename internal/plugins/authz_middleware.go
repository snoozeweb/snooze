package plugins

import (
	"encoding/json"
	"net/http"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// AuthorizeCRUD returns a middleware that enforces the plugin's resolved
// authorization_policy against the request's claims. It is paired with the
// global Auth middleware: when a route's effective `authentication` flag
// resolves to false (i.e. the global Auth was skipped), this middleware
// still runs but lets the request through because no claims are required.
//
// On allow, control is passed to the next handler. On deny:
//   - missing claims AND authentication required: 401 unauthorized
//   - claims present but lacking permissions: 403 forbidden
//
// The handler intentionally does not consult `no_login` — the global Auth
// middleware honours that knob already (no_login mode would short-circuit
// the chain before reaching this handler). Tests can still exercise the
// no-login branch via IsAuthorized directly.
func AuthorizeCRUD(meta Metadata) func(http.Handler) http.Handler {
	return AuthorizeRoute(meta, "")
}

// AuthorizeRoute is the path-aware form of AuthorizeCRUD: it resolves the
// effective `authentication` flag and `authorization_policy` for one specific
// route path (the key under Metadata.Routes; pass "" for the plugin-level
// RouteDefaults). This lets a single plugin mix an authenticated CRUD subtree
// with a public sub-path — e.g. the heartbeat plugin keeps its `heartbeat`
// collection behind auth while exposing a public ping at
// /api/v1/webhook/heartbeat. The webhook mount passes WebhookPath() here so
// each receiver resolves its own flag instead of the plugin-wide default.
func AuthorizeRoute(meta Metadata, routePath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rt := meta.ResolveRoute(routePath)
			authRequired := rt.Authentication == nil || *rt.Authentication

			claims, hasClaims := auth.ClaimsFrom(r.Context())
			if !hasClaims {
				if !authRequired {
					next.ServeHTTP(w, r)
					return
				}
				writeUnauthorized(w, r, "authentication required")
				return
			}

			ok := IsAuthorized(meta, AuthzContext{
				PluginName: meta.PluginName,
				Method:     r.Method,
				RoutePath:  routePath,
				Claims:     claims,
			})
			if !ok {
				writeForbidden(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeUnauthorized / writeForbidden emit the canonical ErrEnvelope
// directly. We don't import internal/api/middleware to avoid a cycle —
// internal/api depends on internal/plugins, not the reverse.

func writeUnauthorized(w http.ResponseWriter, r *http.Request, msg string) {
	envelope := snoozetypes.ErrEnvelope{Error: snoozetypes.ErrBody{
		Code:      "unauthorized",
		Message:   msg,
		RequestID: telemetry.RequestIDFrom(r.Context()),
		TraceID:   telemetry.TraceIDFrom(r.Context()),
	}}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("WWW-Authenticate", `Bearer realm="snooze"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(envelope)
}

func writeForbidden(w http.ResponseWriter, r *http.Request) {
	envelope := snoozetypes.ErrEnvelope{Error: snoozetypes.ErrBody{
		Code:      "forbidden",
		Message:   "permission denied",
		RequestID: telemetry.RequestIDFrom(r.Context()),
		TraceID:   telemetry.TraceIDFrom(r.Context()),
	}}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(envelope)
}
