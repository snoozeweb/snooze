package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/telemetry"
)

// Trace wraps the next handler with otelhttp's instrumentation so every
// request is a span. We also store trace_id and span_id on the request
// context (telemetry.With{Trace,Span}ID) so the audit logger and the error
// envelope have a stable source regardless of provider gymnastics.
func Trace(name string) func(http.Handler) http.Handler {
	if name == "" {
		name = "snooze-api"
	}
	return func(next http.Handler) http.Handler {
		// otelhttp.NewMiddleware injects the span context; we then layer a
		// thin shim that copies the IDs into our telemetry context keys so
		// downstream code can read them without importing otel.
		wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
				ctx = telemetry.WithTraceID(ctx, sc.TraceID().String())
				ctx = telemetry.WithSpanID(ctx, sc.SpanID().String())
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
		return otelhttp.NewMiddleware(name)(wrapped)
	}
}
