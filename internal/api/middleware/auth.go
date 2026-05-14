package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/telemetry"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// SkipPredicate decides whether a request should bypass authentication.
// The middleware uses it to short-circuit before parsing the Authorization
// header — the canonical use case is /healthz, /readyz, /metrics, /login.
type SkipPredicate func(r *http.Request) bool

// Auth returns a chi middleware that validates the Authorization: Bearer
// token via engine, then stores the resulting Claims on the request context
// (auth.WithClaims). A missing or invalid token yields a 401 ErrEnvelope.
// skip lets the caller bypass the check for public endpoints.
func Auth(engine *auth.TokenEngine, skip SkipPredicate) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip != nil && skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			if engine == nil {
				writeUnauthorized(w, r, "auth not configured")
				return
			}
			header := r.Header.Get("Authorization")
			if header == "" {
				writeUnauthorized(w, r, "missing authorization header")
				return
			}
			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeUnauthorized(w, r, "expected Authorization: Bearer <token>")
				return
			}
			claims, err := engine.Verify(parts[1])
			if err != nil {
				writeUnauthorized(w, r, "invalid token")
				return
			}
			ctx := auth.WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeUnauthorized writes a 401 ErrEnvelope directly (we do not import the
// parent api package to keep the import graph DAG-shaped).
func writeUnauthorized(w http.ResponseWriter, r *http.Request, msg string) {
	envelope := snoozetypes.ErrEnvelope{
		Error: snoozetypes.ErrBody{
			Code:      "unauthorized",
			Message:   msg,
			RequestID: telemetry.RequestIDFrom(r.Context()),
			TraceID:   telemetry.TraceIDFrom(r.Context()),
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("WWW-Authenticate", `Bearer realm="snooze"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(envelope)
}
