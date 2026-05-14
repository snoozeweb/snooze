// Package telemetry centralises Snooze's observability primitives: scoped
// slog loggers, the OpenTelemetry tracer setup, and the Prometheus metrics
// registry. Sub-files provide each pillar; ctx.go carries the cross-cutting
// context-key helpers used by every middleware and handler in Snooze.
package telemetry

import "context"

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxTraceID
	ctxSpanID
)

// WithRequestID returns a child context carrying the request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

// RequestIDFrom returns the request id attached to ctx, or "" if none.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// WithTraceID returns a child context carrying the trace id.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxTraceID, id)
}

// TraceIDFrom returns the trace id attached to ctx, or "" if none.
func TraceIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxTraceID).(string); ok {
		return v
	}
	return ""
}

// WithSpanID returns a child context carrying the span id.
func WithSpanID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxSpanID, id)
}

// SpanIDFrom returns the span id attached to ctx, or "" if none.
func SpanIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxSpanID).(string); ok {
		return v
	}
	return ""
}
