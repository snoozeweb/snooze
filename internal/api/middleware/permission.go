package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// RequirePerm returns a middleware that lets the request through only when
// the caller's Claims carry at least one of perms, or a catch-all that covers
// one of them: `rw_all` covers everything, `ro_all` covers the `ro_*` entries
// only (see auth.HasPermission). Missing claims yields 401; mismatched
// permissions yields 403.
//
// Use RequireLiteralPerm instead when no catch-all may satisfy the gate.
func RequirePerm(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFrom(r.Context())
			if !ok {
				writeUnauthorized(w, r, "authentication required")
				return
			}
			for _, p := range perms {
				if auth.HasPermission(claims, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeForbidden(w, r)
		})
	}
}

// RequirePlatformPerm gates a control-plane route (the /api/v1/tenant registry).
// Unlike RequirePerm it is deliberately strict on two axes (closing C4):
//
//   - LITERAL permission membership: the caller's claims must carry one of perms
//     verbatim. The rw_all wildcard is NOT honored — a tenant admin seeded with
//     rw_all must never reach the tenant registry.
//   - PLATFORM ORIGIN: the caller must be authenticated against the default
//     tenant (platform admins live there per D5). Any other tenant origin is
//     rejected even when the literal permission is present.
//
// Missing claims yields 401; failing either axis yields 403.
func RequirePlatformPerm(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFrom(r.Context())
			if !ok {
				writeUnauthorized(w, r, "authentication required")
				return
			}
			// Platform origin: only the default tenant hosts platform admins.
			if claims.TenantID != snoozetypes.DefaultTenant {
				writeForbidden(w, r)
				return
			}
			// Literal membership only — rw_all must not satisfy a platform perm.
			if hasLiteralPerm(claims, perms) {
				next.ServeHTTP(w, r)
				return
			}
			writeForbidden(w, r)
		})
	}
}

// RequireLiteralPerm gates a route on LITERAL permission membership: the
// caller's claims must carry one of perms verbatim and the rw_all wildcard
// does NOT satisfy it. Unlike RequirePlatformPerm it places no constraint on
// the caller's tenant — any tenant may hold the permission, it just has to
// hold it explicitly.
//
// This is the gate for protected fields (protected.WritePermission): an
// existing admin role carrying rw_all must not silently inherit the right to
// write agent-authored analysis, or the protection is decorative.
//
// Missing claims yields 401; a caller without the literal permission gets 403.
func RequireLiteralPerm(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFrom(r.Context())
			if !ok {
				writeUnauthorized(w, r, "authentication required")
				return
			}
			if hasLiteralPerm(claims, perms) {
				next.ServeHTTP(w, r)
				return
			}
			writeForbidden(w, r)
		})
	}
}

// hasLiteralPerm reports whether claims carry any of want verbatim. The rw_all
// wildcard is intentionally NOT treated as a match.
func hasLiteralPerm(claims snoozetypes.Claims, want []string) bool {
	for _, w := range want {
		for _, p := range claims.Permissions {
			if p == w {
				return true
			}
		}
	}
	return false
}

// writeForbidden emits a canonical 403 envelope.
func writeForbidden(w http.ResponseWriter, r *http.Request) {
	envelope := snoozetypes.ErrEnvelope{
		Error: snoozetypes.ErrBody{
			Code:      "forbidden",
			Message:   "permission denied",
			RequestID: telemetry.RequestIDFrom(r.Context()),
			TraceID:   telemetry.TraceIDFrom(r.Context()),
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(envelope)
}
