package plugins

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/japannext/snooze/internal/config"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/telemetry"
)

// Bus is the minimal cross-instance event publisher surface the plugin layer
// needs. It is structurally satisfied by internal/syncer.Bus; the package is
// not imported directly here to avoid an import cycle through internal/db.
type Bus interface {
	// Close releases all resources. Idempotent.
	Close() error
}

// Host is the small surface a plugin needs from the core server: DB driver,
// cross-instance bus, structured logger, OTEL tracer, metrics registry,
// immutable config snapshot, and sibling-plugin lookup.
//
// Concrete Host implementations live in internal/core. Tests in this package
// supply lightweight stubs.
type Host interface {
	DB() db.Driver
	Bus() Bus
	Logger() *slog.Logger
	Tracer() trace.Tracer
	Metrics() *telemetry.Registry
	Config() *config.Config
	// Plugin returns the registered plugin by name, or nil when absent.
	Plugin(name string) Plugin
}
