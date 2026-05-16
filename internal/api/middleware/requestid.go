// Package middleware contains the chi-compatible middlewares used by the
// Snooze API: request-id injection, panic recovery, OpenTelemetry tracing,
// audit logging, CORS, JWT bearer auth, and permission gating.
package middleware

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/snoozeweb/snooze/internal/telemetry"
)

// RequestIDHeader is the HTTP header carrying the request correlation id.
const RequestIDHeader = "X-Request-ID"

// RequestID injects a request-id into the request context and echoes it back
// on the response. When the inbound request already carries one we honour it;
// otherwise we mint a fresh UUIDv4. The id is also stored on the context via
// telemetry.WithRequestID so log/error helpers can find it.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if id == "" {
				id = uuid.NewString()
			}
			ctx := telemetry.WithRequestID(r.Context(), id)
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
