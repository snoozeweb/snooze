package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/snoozeweb/snooze/internal/housekeeper"
	"github.com/snoozeweb/snooze/internal/mq"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
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
	if err := c.bootRuntimeSettings(); err != nil {
		return err
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

// bootSecrets runs EnsureSecrets and constructs the TokenEngine plus the
// refresh-token store. The latter persists hashed refresh tokens in the
// "refresh_token" collection; the lease is sourced from cfg.Auth.
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
	c.Refresh = auth.NewRefreshTokenStore(c.Driver, time.Duration(c.Cfg.Auth.RefreshTokenLease))
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
	dbcfg := c.Cfg.Core.Database
	kind := mqKindForDatabase(dbcfg.Type)
	mqcfg := mq.Config{Kind: kind}
	switch kind {
	case mq.KindMongo:
		uri := dbcfg.DSN
		if uri == "" {
			if s, ok := dbcfg.Host.(string); ok {
				uri = s
			}
		}
		mqcfg.Mongo = mq.MongoConfig{
			URI:      uri,
			Database: dbcfg.Database,
		}
	case mq.KindPG:
		mqcfg.PG = mq.PGConfig{
			DSN: dbcfg.DSN,
		}
	}
	mgr, err := mq.NewManager(ctx, mqcfg)
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

// bootRuntimeSettings constructs the DB-backed read-through cache that
// subsystems consult for live-editable settings (LDAP, housekeeper,
// notification frequency, ...). Cache TTL is short enough that a missed
// invalidation still self-heals within seconds; the settings plugin's
// hook drops the cache explicitly on every write.
func (c *Core) bootRuntimeSettings() error {
	c.Settings = config.NewRuntimeSettings(c.Driver, c.Cfg, 5*time.Second)
	return nil
}

// optionalPlugins lists plugins that are registered (blank-imported in
// pluginimpl/all) but disabled by default — operators opt in via
// core.enabled_optional_plugins. Anything in this set that the operator did
// not enable is dropped from c.plugins after plugins.Build, which hides it
// from the /metadata endpoint, the CRUD router, and the notification
// dispatcher (which logs "notifier plugin not registered" if a notification
// references it). Add to this map deliberately — most plugins should be on
// by default.
var optionalPlugins = map[string]bool{
	"patlite": true,
}

// bootPlugins instantiates the registered plugin set and captures the ordered
// processor slice. Optional plugins not present in
// Cfg.Core.EnabledOptionalPlugins are filtered out before the plugin map is
// stored on Core.
func (c *Core) bootPlugins(ctx context.Context) error {
	all, procs, err := plugins.Build(ctx, c, c.Cfg.Core.ProcessPlugins)
	if err != nil {
		return fmt.Errorf("boot: plugins: %w", err)
	}
	c.plugins = filterOptionalPlugins(all, c.Cfg.Core.EnabledOptionalPlugins)
	c.processOrder = procs
	return nil
}

// filterOptionalPlugins drops entries that appear in optionalPlugins unless
// they were named in the enabled-list. Returned map shares keys with the
// input (the input map is mutated in place).
func filterOptionalPlugins(all map[string]plugins.Plugin, enabledList []string) map[string]plugins.Plugin {
	enabled := make(map[string]bool, len(enabledList))
	for _, name := range enabledList {
		enabled[name] = true
	}
	for name := range all {
		if optionalPlugins[name] && !enabled[name] {
			delete(all, name)
		}
	}
	return all
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

func (p pluggableShim) Name() string                     { return p.name }
func (p pluggableShim) Reload(ctx context.Context) error { return p.plugin.Reload(ctx) }

// bootHousekeeper registers the default cleanup/renumber jobs.
//
// Cadence is sourced from “c.Settings“ so that an operator who edits a
// housekeeping.* setting in the Settings UI sees the new interval applied
// to the next firing of every affected job, without a server restart.
func (c *Core) bootHousekeeper() error {
	hk := housekeeper.New(c.Logger(),
		housekeeper.WithTriggerOnStartup(c.Cfg.Housekeeper.TriggerOnStartup),
	)

	// liveInterval reads the current housekeeper snapshot and returns the
	// field selector applied to it. The closure-per-job pattern keeps the
	// call site declarative.
	liveInterval := func(pick func(config.HousekeeperConfig) time.Duration, fallback time.Duration) func(ctx context.Context) time.Duration {
		return func(ctx context.Context) time.Duration {
			if c.Settings == nil {
				return fallback
			}
			snap, err := c.Settings.Housekeeper(ctx)
			if err != nil {
				return fallback
			}
			if d := pick(snap); d > 0 {
				return d
			}
			return fallback
		}
	}

	jobs := []registration{
		liveIntervalReg(housekeeper.CleanupTimeoutJob(c.Driver, "record"),
			liveInterval(func(h config.HousekeeperConfig) time.Duration { return h.CleanupAlert.AsDuration() }, 5*time.Minute)),
		liveIntervalReg(housekeeper.CleanupAggregateJob(c.Driver),
			liveInterval(func(h config.HousekeeperConfig) time.Duration { return h.CleanupAggregate.AsDuration() }, time.Minute)),
		liveIntervalReg(housekeeper.CleanupCommentsJob(c.Driver),
			liveInterval(func(h config.HousekeeperConfig) time.Duration { return h.CleanupComment.AsDuration() }, 24*time.Hour)),
		liveIntervalReg(housekeeper.CleanupOrphansJob(c.Driver, "record"),
			liveInterval(func(h config.HousekeeperConfig) time.Duration { return h.CleanupOrphans.AsDuration() }, 24*time.Hour)),
		liveIntervalReg(housekeeper.CleanupSnoozeJob(c.Driver),
			liveInterval(func(h config.HousekeeperConfig) time.Duration { return h.CleanupSnooze.AsDuration() }, 72*time.Hour)),
		liveIntervalReg(housekeeper.CleanupNotificationJob(c.Driver),
			liveInterval(func(h config.HousekeeperConfig) time.Duration { return h.CleanupNotification.AsDuration() }, 72*time.Hour)),
		liveIntervalReg(housekeeper.CleanupAuditAsIntervalJob(c.Driver, c.Settings),
			liveInterval(func(h config.HousekeeperConfig) time.Duration { return h.CleanupAudit.AsDuration() }, 28*24*time.Hour)),
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

// liveIntervalReg pairs a factory-provided job with a live-interval
// resolver. The runtime poll cadence defaults to 30 seconds; the resolver
// chooses when the job actually fires.
func liveIntervalReg(ij housekeeper.IntervalJob, resolver func(ctx context.Context) time.Duration) registration {
	return registration{
		name: ij.Job.Name(),
		job:  ij.Job,
		sched: housekeeper.Schedule{
			LiveInterval:     resolver,
			LivePollInterval: 30 * time.Second,
		},
	}
}

func cronReg(cj housekeeper.CronJob) registration {
	return registration{
		name:  cj.Job.Name(),
		job:   cj.Job,
		sched: housekeeper.Schedule{Cron: cj.Cron},
	}
}
