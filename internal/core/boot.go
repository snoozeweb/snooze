package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/config/schema"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/db/asyncwriter"
	"github.com/japannext/snooze/internal/housekeeper"
	"github.com/japannext/snooze/internal/mq"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/syncer"
)

// bootstrap wires every Core subsystem in dependency order. The implementation
// is split into helper methods so individual phases can be tested in isolation.
//
// Phases:
//  1. EnsureSecrets — JWT key + reload token (crypto/rand on first boot).
//  2. EnsureRoot    — bcrypt root user, log generated password to stderr.
//  3. TokenEngine.
//  4. AsyncWriter   — background DB-batch goroutine, not started yet.
//  5. MQ Bus        — inproc/pg/mongo depending on config.
//  6. Plugins       — plugins.Build with the configured process order.
//  7. Syncer        — c.Driver.Watcher() as its event bus + NodeHeartbeat.
//  8. Housekeeper   — default cleanup/renumber jobs.
func (c *Core) bootstrap(ctx context.Context) error {
	if err := c.bootSecrets(ctx); err != nil {
		return err
	}
	if err := c.bootRoot(ctx); err != nil {
		return err
	}
	if err := c.bootAsync(); err != nil {
		return err
	}
	if err := c.bootMQ(ctx); err != nil {
		return err
	}
	if c.Cfg.Core.BootstrapDB {
		if err := BootstrapDB(ctx, c.Driver); err != nil {
			return fmt.Errorf("boot: bootstrap db: %w", err)
		}
	}
	if err := c.bootPlugins(ctx); err != nil {
		return err
	}
	if err := c.bootSyncer(); err != nil {
		return err
	}
	if err := c.bootHousekeeper(); err != nil {
		return err
	}
	return nil
}

// bootSecrets runs EnsureSecrets and constructs the TokenEngine.
func (c *Core) bootSecrets(ctx context.Context) error {
	jwtKey, _, err := EnsureSecrets(ctx, c.Driver)
	if err != nil {
		return fmt.Errorf("boot: ensure secrets: %w", err)
	}
	engine, err := auth.NewTokenEngine(jwtKey, c.Cfg.Auth)
	if err != nil {
		return fmt.Errorf("boot: token engine: %w", err)
	}
	c.Tokens = engine
	return nil
}

// bootRoot runs auth.EnsureRoot and logs the generated password once to the
// snooze logger at WARN level (which routes to stderr in JSON or text mode).
func (c *Core) bootRoot(ctx context.Context) error {
	if !c.Cfg.Core.CreateRootUser {
		return nil
	}
	pwd, err := auth.EnsureRoot(ctx, c.Driver)
	if err != nil {
		return fmt.Errorf("boot: ensure root: %w", err)
	}
	if pwd != "" {
		c.Logger().Warn("bootstrap: created initial root user; record the password — it will not be shown again",
			"username", auth.RootUsername,
			"password", pwd,
		)
	}
	return nil
}

// bootAsync constructs the async writer. The goroutine launches when Run is
// called.
func (c *Core) bootAsync() error {
	c.Async = asyncwriter.New(c.Driver, asyncFlushInterval, nil)
	return nil
}

// bootMQ constructs the message-queue bus. The choice of backend mirrors the
// database backend for zero-config installs.
func (c *Core) bootMQ(ctx context.Context) error {
	kind := mqKindForDatabase(c.Cfg.Core.Database.Type)
	mgr, err := mq.NewManager(ctx, mq.Config{Kind: kind})
	if err != nil {
		return fmt.Errorf("boot: mq manager: %w", err)
	}
	c.mqManager = mgr
	c.MsgBus = mgr.Bus
	return nil
}

// mqKindForDatabase maps a db.type config value to the matching mq backend.
// SQLite/file-backed installs default to in-process; pg/mongo reuse the same
// connection family.
func mqKindForDatabase(dbType string) string {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "postgres", "pg":
		return mq.KindPG
	case "mongo", "mongodb":
		return mq.KindMongo
	default:
		return mq.KindInproc
	}
}

// bootPlugins instantiates the registered plugin set and captures the ordered
// processor slice.
func (c *Core) bootPlugins(ctx context.Context) error {
	all, procs, err := plugins.Build(ctx, c, c.Cfg.Core.ProcessPlugins)
	if err != nil {
		return fmt.Errorf("boot: plugins: %w", err)
	}
	c.plugins = all
	c.processOrder = procs
	return nil
}

// bootSyncer constructs the per-driver syncer and the node heartbeat.
func (c *Core) bootSyncer() error {
	bus := c.Driver.Watcher()
	if bus == nil {
		// No watcher (e.g. tests, unsupported driver) — skip the syncer.
		c.Sync = nil
	} else {
		plugMap := make(map[string]syncer.Pluggable, len(c.plugins))
		for name, p := range c.plugins {
			plugMap[name] = pluggableShim{name: name, plugin: p}
		}
		c.Sync = &syncer.Syncer{
			Bus:     bus,
			Plugins: plugMap,
			Logger:  c.Logger(),
		}
	}

	c.Heart = &syncer.NodeHeartbeat{
		Persist: func(ctx context.Context, collection string, doc map[string]any) error {
			_, err := c.Driver.Write(ctx, collection, []db.Document{doc}, db.WriteOptions{
				Primary:    []string{"node"},
				UpdateTime: true,
			})
			return err
		},
		Logger: c.Logger(),
	}
	return nil
}

// pluggableShim adapts a plugins.Plugin into the syncer.Pluggable contract
// (which is structurally identical: Name + Reload).
type pluggableShim struct {
	name   string
	plugin plugins.Plugin
}

func (p pluggableShim) Name() string                          { return p.name }
func (p pluggableShim) Reload(ctx context.Context) error      { return p.plugin.Reload(ctx) }

// bootHousekeeper registers the default cleanup/renumber jobs.
func (c *Core) bootHousekeeper() error {
	hk := housekeeper.New(c.Logger(),
		housekeeper.WithTriggerOnStartup(c.Cfg.Housekeeper.TriggerOnStartup),
	)
	hkCfg := c.Cfg.Housekeeper

	jobs := []registration{
		intervalReg(housekeeper.CleanupTimeoutJob(c.Driver, "record"), durOrDefault(hkCfg.CleanupAlert, 5*time.Minute)),
		intervalReg(housekeeper.CleanupAggregateJob(c.Driver), durOrDefault(hkCfg.CleanupAggregate, time.Minute)),
		intervalReg(housekeeper.CleanupCommentsJob(c.Driver), durOrDefault(hkCfg.CleanupComment, 24*time.Hour)),
		intervalReg(housekeeper.CleanupOrphansJob(c.Driver, "record"), durOrDefault(hkCfg.CleanupOrphans, 24*time.Hour)),
		cronReg(housekeeper.CleanupAuditJob(c.Driver, time.Duration(hkCfg.CleanupAudit))),
		cronReg(housekeeper.RenumberJob(c.Driver, "stats", "date_epoch")),
	}

	for _, j := range jobs {
		if err := hk.Register(j.job, j.sched); err != nil {
			return fmt.Errorf("boot: housekeeper register %q: %w", j.name, err)
		}
	}
	c.HK = hk
	return nil
}

type registration struct {
	name  string
	job   housekeeper.Job
	sched housekeeper.Schedule
}

// intervalReg overrides the factory-provided Interval when override is
// non-zero. Factory defaults remain authoritative when the config value is
// zero (i.e. user did not customise the field).
func intervalReg(ij housekeeper.IntervalJob, override time.Duration) registration {
	interval := ij.Interval
	if override > 0 {
		interval = override
	}
	return registration{
		name:  ij.Job.Name(),
		job:   ij.Job,
		sched: housekeeper.Schedule{Interval: interval},
	}
}

func cronReg(cj housekeeper.CronJob) registration {
	return registration{
		name:  cj.Job.Name(),
		job:   cj.Job,
		sched: housekeeper.Schedule{Cron: cj.Cron},
	}
}

// durOrDefault returns d.AsDuration() unless it is zero, in which case it
// returns fallback.
func durOrDefault(d schema.Duration, fallback time.Duration) time.Duration {
	if v := d.AsDuration(); v > 0 {
		return v
	}
	return fallback
}
