package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Recoverer catches panics raised by downstream handlers, logs the panic value
// + stack trace on logger, increments the SupervisorPanic metric labelled
// "api", and writes a 500 ErrEnvelope. ResponseWriter writes that already
// flushed cannot be recovered; we still record the panic so the operator sees
// it.
func Recoverer(logger *slog.Logger, metrics *telemetry.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if metrics != nil && metrics.SupervisorPanic != nil {
					metrics.SupervisorPanic.WithLabelValues("api").Inc()
				}
				stack := debug.Stack()
				if logger != nil {
					telemetry.WithRequest(r.Context(), logger).Error("api panic",
						slog.Any("panic", rec),
						slog.String("path", r.URL.Path),
						slog.String("method", r.Method),
						slog.String("stack", string(stack)),
					)
				}
				// Best-effort write; if the response was already committed
				// the client just sees a truncated body.
				envelope := snoozetypes.ErrEnvelope{
					Error: snoozetypes.ErrBody{
						Code:      "internal",
						Message:   "internal server error",
						RequestID: telemetry.RequestIDFrom(r.Context()),
						TraceID:   telemetry.TraceIDFrom(r.Context()),
					},
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				if err := json.NewEncoder(w).Encode(envelope); err != nil {
					_, _ = fmt.Fprintf(w, `{"error":{"code":"internal","message":%q}}`, err.Error())
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
