package telemetry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// LogFormat selects between JSON-line and human-friendly text output.
type LogFormat string

const (
	// LogFormatJSON emits one structured JSON object per line.
	LogFormatJSON LogFormat = "json"
	// LogFormatText emits a human-readable key=value line.
	LogFormatText LogFormat = "text"
)

// LoggerConfig drives Init.
//
// Mirrors the Python LogCommon model (logging.py): level + format, plus an
// explicit output sink (defaults to stderr).
type LoggerConfig struct {
	Level  string    // "debug", "info", "warn", "error" (case-insensitive). Empty defaults to "info".
	Format LogFormat // "json" (default) or "text".
	Output io.Writer // defaults to os.Stderr when nil.
}

// Loggers groups the four scoped slog loggers that mirror the Python codebase:
// snooze (main), snooze-process, snooze-api, snooze-audit.
type Loggers struct {
	Snooze  *slog.Logger
	Process *slog.Logger
	API     *slog.Logger
	Audit   *slog.Logger
}

// Init builds the four scoped loggers with a shared underlying handler.
//
// Each logger differs only by the `logger` attribute, so downstream JSON
// consumers can filter by source while keeping a single sink configuration.
func Init(cfg LoggerConfig) (*Loggers, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	out := cfg.Output
	if out == nil {
		out = os.Stderr
	}
	format := cfg.Format
	if format == "" {
		format = LogFormatJSON
	}
	handlerOpts := &slog.HandlerOptions{Level: level}
	var base slog.Handler
	switch format {
	case LogFormatJSON:
		base = slog.NewJSONHandler(out, handlerOpts)
	case LogFormatText:
		base = slog.NewTextHandler(out, handlerOpts)
	default:
		return nil, fmt.Errorf("telemetry: unsupported log format %q", format)
	}
	mk := func(name string) *slog.Logger {
		return slog.New(base.WithAttrs([]slog.Attr{slog.String("logger", name)}))
	}
	return &Loggers{
		Snooze:  mk("snooze"),
		Process: mk("snooze-process"),
		API:     mk("snooze-api"),
		Audit:   mk("snooze-audit"),
	}, nil
}

// WithRequest returns a derived logger annotated with the request_id, trace_id
// and span_id carried by ctx (any of which may be empty and is then skipped).
// Trace/span IDs are taken from the OpenTelemetry span if the explicit context
// values are not set.
func WithRequest(ctx context.Context, l *slog.Logger) *slog.Logger {
	if l == nil {
		l = slog.Default()
	}
	if ctx == nil {
		return l
	}
	attrs := []any{}
	if id := RequestIDFrom(ctx); id != "" {
		attrs = append(attrs, "request_id", id)
	}
	traceID := TraceIDFrom(ctx)
	spanID := SpanIDFrom(ctx)
	if traceID == "" || spanID == "" {
		if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
			if traceID == "" {
				traceID = sc.TraceID().String()
			}
			if spanID == "" {
				spanID = sc.SpanID().String()
			}
		}
	}
	if traceID != "" {
		attrs = append(attrs, "trace_id", traceID)
	}
	if spanID != "" {
		attrs = append(attrs, "span_id", spanID)
	}
	if len(attrs) == 0 {
		return l
	}
	return l.With(attrs...)
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error", "err":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("telemetry: unknown log level %q", s)
	}
}
