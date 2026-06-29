package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// IngestAllow returns a chi middleware enforcing the runtime ingest
// kill-switch on the webhook receivers. The allowed predicate reports whether
// intake is currently permitted for the tenant in the request context (it is
// read per-request so a toggle takes effect without a restart).
//
// When allowed is nil, or returns true, the request passes through unchanged.
// When it returns false the middleware short-circuits with a 503
// ingest_disabled envelope, mirroring the api package's ErrUnavailable shape
// inline (this package cannot import the parent api package without creating an
// import cycle — see writeUnauthorized in auth.go for the same pattern).
func IngestAllow(allowed func(context.Context) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if allowed == nil || allowed(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			writeIngestDisabled(w, r)
		})
	}
}

// writeIngestDisabled writes a 503 ErrEnvelope directly (we do not import the
// parent api package to keep the import graph DAG-shaped).
func writeIngestDisabled(w http.ResponseWriter, r *http.Request) {
	envelope := snoozetypes.ErrEnvelope{
		Error: snoozetypes.ErrBody{
			Code:      "ingest_disabled",
			Message:   "alert ingestion is disabled",
			RequestID: telemetry.RequestIDFrom(r.Context()),
			TraceID:   telemetry.TraceIDFrom(r.Context()),
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(envelope)
}
