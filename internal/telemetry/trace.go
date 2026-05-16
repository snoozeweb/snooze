package telemetry

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/snoozeweb/snooze/internal/version"
)

// OTLPProtocol selects the wire protocol for the OTLP trace exporter.
type OTLPProtocol string

const (
	// OTLPProtocolGRPC is the default OTel-style protocol on port 4317.
	OTLPProtocolGRPC OTLPProtocol = "grpc"
	// OTLPProtocolHTTP is OTLP/HTTP protobuf on port 4318.
	OTLPProtocolHTTP OTLPProtocol = "http/protobuf"
)

// TracingConfig is the runtime configuration for tracing.
//
// Endpoint empty disables trace export and InitTracer becomes a no-op that
// still installs a working in-process TracerProvider (so spans can be
// recorded in tests without a collector). Protocol falls back to the
// OTEL_EXPORTER_OTLP_PROTOCOL env variable, then to grpc, matching Python.
type TracingConfig struct {
	Endpoint    string
	Protocol    OTLPProtocol
	ServiceName string // defaults to "snooze-server"
	Insecure    bool   // disables TLS for the OTLP connection
}

// InitTracer wires the global OpenTelemetry TracerProvider and returns a
// shutdown func that flushes pending spans and releases connections.
//
// The exporter is chosen by Protocol or the OTEL_EXPORTER_OTLP_PROTOCOL env
// variable. When no Endpoint is configured the provider runs without an
// exporter (suitable for tests and offline development).
func InitTracer(ctx context.Context, cfg TracingConfig) (func(context.Context) error, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "snooze-server"
	}

	hostname, _ := os.Hostname()
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(version.Version),
			semconv.ServiceInstanceID(hostname),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}

	if cfg.Endpoint != "" {
		exp, err := buildExporter(ctx, cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return tp.Shutdown, nil
}

func buildExporter(ctx context.Context, cfg TracingConfig) (*otlptrace.Exporter, error) {
	proto := cfg.Protocol
	if proto == "" {
		proto = OTLPProtocol(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")))
	}
	switch proto {
	case "", OTLPProtocolGRPC:
		grpcOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
		}
		return otlptrace.New(ctx, otlptracegrpc.NewClient(grpcOpts...))
	case OTLPProtocolHTTP, "http", "http/json":
		httpOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			httpOpts = append(httpOpts, otlptracehttp.WithInsecure())
		}
		return otlptrace.New(ctx, otlptracehttp.NewClient(httpOpts...))
	default:
		return nil, fmt.Errorf("telemetry: unknown OTLP protocol %q", proto)
	}
}
