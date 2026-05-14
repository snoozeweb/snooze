package core

import (
	"go.opentelemetry.io/otel/trace"

	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/telemetry"
)

// host.go implements the plugins.Host interface. Methods are split out from
// core.go to keep the orchestrator's public surface separate from the
// dependency-injection contract every plugin sees.

// DB returns the storage driver.
func (c *Core) DB() db.Driver { return c.Driver }

// Tracer returns the OpenTelemetry tracer used by every Snooze subsystem.
func (c *Core) Tracer() trace.Tracer { return c.Trc }

// Metrics returns the Prometheus metrics registry.
func (c *Core) Metrics() *telemetry.Registry { return c.Reg }

// Bus returns the cross-instance event bus. Plugins use it only via the
// plugins.Bus interface (Close()) — the underlying value is the per-driver
// syncer.Bus from c.Driver.Watcher(). May return nil when the driver does
// not provide a watcher (e.g. in unit tests).
func (c *Core) Bus() plugins.Bus {
	if c.Driver == nil {
		return nil
	}
	w := c.Driver.Watcher()
	if w == nil {
		return nil
	}
	return w
}

// Compile-time guarantee that *Core satisfies plugins.Host.
var _ plugins.Host = (*Core)(nil)
