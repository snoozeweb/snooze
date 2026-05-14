package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/telemetry"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// RequirePerm returns a middleware that lets the request through only when
// the caller's Claims carry at least one of perms (or the wildcard rw_all).
// Missing claims yields 401; mismatched permissions yields 403.
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
