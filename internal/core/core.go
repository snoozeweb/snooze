// Package core implements the Snooze server orchestrator: the boot sequence,
// the alert-processing pipeline, the per-goroutine supervisor that replaces
// Python's SurvivingThread + tenacity pair, and the first-boot DB seeding.
//
// Core is the single value the rest of the binary consumes: every plugin, the
// HTTP layer, and the CLI wire dependencies through it.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/config"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/db/asyncwriter"
	"github.com/japannext/snooze/internal/housekeeper"
	"github.com/japannext/snooze/internal/mq"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/syncer"
	"github.com/japannext/snooze/internal/telemetry"
)

// Core orchestrates every Snooze subsystem and implements plugins.Host.
//
// Field names that would otherwise collide with the Host interface methods
// (DB, Bus, Metrics, Tracer, Config) are accessible via the matching
// accessor; the underlying values can also be reached through the
// suffixed-or-renamed fields (Driver, MsgBus, Reg, Trc, Cfg).
type Core struct {
	Cfg       *config.Config
	Driver    db.Driver
	Tokens    *auth.TokenEngine
	MsgBus    mq.Bus
	Reg       *telemetry.Registry
	Trc       trace.Tracer
	Loggers   *telemetry.Loggers
	Async     *asyncwriter.Writer
	Providers *auth.Registry
	// Settings is the live read-through cache for DB-stored runtime
	// settings. Subsystems (LDAP backend, housekeeper, notification
	// scheduler, …) consult it on every operation so an edit in the UI
	// takes effect on the next request. The settings plugin's
	// create/update/delete hooks call ``Settings.Invalidate``.
	Settings *config.RuntimeSettings
	HK       *housekeeper.Housekeeper
	Sync     *syncer.Syncer
	Heart    *syncer.NodeHeartbeat
	Sup      *Supervisor

	mqManager *mq.Manager

	plugins      map[string]plugins.Plugin
	processOrder []plugins.Processor
}

// asyncFlushInterval is the default cadence at which the async writer flushes
// counter increments to the database.
const asyncFlushInterval = 250 * time.Millisecond

// New constructs a Core, runs the boot sequence, and returns the wired
// orchestrator. The returned Core has not yet started any goroutines — call
// Run to drive them.
func New(ctx context.Context, cfg *config.Config, drv db.Driver, loggers *telemetry.Loggers, metrics *telemetry.Registry) (*Core, error) {
	if cfg == nil {
		return nil, errors.New("core: nil config")
	}
	if drv == nil {
		return nil, errors.New("core: nil db driver")
	}
	if loggers == nil {
		return nil, errors.New("core: nil loggers")
	}
	if metrics == nil {
		return nil, errors.New("core: nil metrics")
	}
	c := &Core{
		Cfg:     cfg,
		Driver:  drv,
		Loggers: loggers,
		Reg:     metrics,
		Trc:     otel.Tracer("snooze"),
	}
	c.Sup = &Supervisor{Logger: loggers.Snooze, Metrics: metrics}
	if err := c.bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("core: bootstrap: %w", err)
	}
	return c, nil
}

// Run drives every long-running subsystem under a single errgroup and blocks
// until ctx is cancelled or a critical subsystem returns. The first non-nil
// error from any subsystem is returned.
func (c *Core) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)

	// AsyncWriter is critical: a dropped flush silently loses counter updates.
	if c.Async != nil {
		c.Sup.Go(g, gctx, Job{
			Name:     "asyncwriter",
			Critical: true,
			Fn:       c.Async.Run,
		})
	}

	// Housekeeper is non-critical: cleanup jobs can fail without taking the
	// server down with them.
	if c.HK != nil {
		c.Sup.Go(g, gctx, Job{
			Name: "housekeeper",
			Fn:   c.HK.Run,
		})
	}

	// Syncer is non-critical: a sync failure stalls plugin-reload but the
	// hot path keeps serving alerts.
	if c.Sync != nil {
		c.Sup.Go(g, gctx, Job{
			Name: "syncer",
			Fn:   c.Sync.Run,
		})
	}

	// NodeHeartbeat is non-critical: missing heartbeats only hurt visibility
	// into the cluster.
	if c.Heart != nil {
		c.Sup.Go(g, gctx, Job{
			Name: "node-heartbeat",
			Fn:   c.Heart.Run,
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("core: run: %w", err)
	}
	return nil
}

// Plugin returns the registered plugin by name, or nil when absent. Part of
// the plugins.Host contract.
func (c *Core) Plugin(name string) plugins.Plugin { return c.plugins[name] }

// Logger returns the main snooze logger. Part of the plugins.Host contract.
func (c *Core) Logger() *slog.Logger {
	if c.Loggers == nil {
		return slog.Default()
	}
	return c.Loggers.Snooze
}

// MQ returns the message-queue bus.
//
// The plugins.Host Bus method exposes the syncer event bus instead (see
// host.go); the mq bus is reachable to non-plugin callers via this getter.
func (c *Core) MQ() mq.Bus { return c.MsgBus }

// Plugins returns a snapshot map of every registered plugin keyed by name.
func (c *Core) Plugins() map[string]plugins.Plugin {
	out := make(map[string]plugins.Plugin, len(c.plugins))
	for k, v := range c.plugins {
		out[k] = v
	}
	return out
}

// ProcessOrder returns the ordered processor slice in pipeline order.
func (c *Core) ProcessOrder() []plugins.Processor {
	out := make([]plugins.Processor, len(c.processOrder))
	copy(out, c.processOrder)
	return out
}

// Config returns the immutable bootstrap config. Part of the plugins.Host
// contract.
func (c *Core) Config() *config.Config { return c.Cfg }
