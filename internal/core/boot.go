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
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// bootstrap wires every Core subsystem in dependency order. The implementation
// is split into helper methods so individual phases can be tested in isolation.
//
// Multitenancy note: every first-boot seed writes to either a global collection
// (tenant, secrets — bypass tenant injection) or a tenant-scoped collection
// (role, general, aggregaterule, user — fail-closed with ErrNoTenant on a naked
// context). We therefore derive a single seedCtx scoped to the reserved default
// tenant and run ALL seeding under it: global writes still pass, and
// default-tenant writes are stamped/injected correctly instead of fail-closing.
//
// Phases:
//  1. EnsureSecrets — JWT key + reload token (crypto/rand on first boot).
//  2. auth.BootstrapDB — default tenant doc (global) + platform_admin role
//     (default-tenant role collection). MUST run before EnsureRoot so the role
//     exists when root is granted it.
//  3. core.BootstrapDB — default RBAC roles + aggregate rule + init marker.
//  4. EnsureRoot    — bcrypt root user, log generated password to stderr.
//  5. AsyncWriter   — background DB-batch goroutine, not started yet.
//  6. MQ Bus        — inproc/pg/mongo depending on config.
//  7. Plugins       — plugins.Build (default-tenant scope) + per-tenant cache
//     hydration for every active tenant.
//  8. Syncer        — c.Driver.Watcher() as its event bus + NodeHeartbeat.
//  9. Housekeeper   — default cleanup jobs.
func (c *Core) bootstrap(ctx context.Context) error {
	// seedCtx scopes all first-boot seeding to the reserved default tenant so
	// tenant-scoped writes are stamped instead of fail-closing on ErrNoTenant.
	seedCtx := snoozetypes.WithTenant(ctx, snoozetypes.DefaultTenant)

	// bootSecrets stays on the plain ctx: it only touches the global "secrets"
	// collection, which bypasses tenant injection.
	if err := c.bootSecrets(ctx); err != nil {
		return err
	}
	if c.Cfg.Core.BootstrapDB {
		// Seed the default tenant doc + platform_admin role FIRST so the role
		// exists when EnsureRoot grants it to root below.
		if err := auth.BootstrapDB(seedCtx, c.Driver); err != nil {
			return fmt.Errorf("boot: auth bootstrap db: %w", err)
		}
		// Seed default RBAC roles + aggregate rule + init marker under the
		// default tenant.
		if err := BootstrapDB(seedCtx, c.Driver); err != nil {
			return fmt.Errorf("boot: bootstrap db: %w", err)
		}
	}
	if rogue, err := auth.RogueReservedRoles(snoozetypes.WithPlatformScope(ctx), c.Driver); err != nil {
		c.Logger().Warn("boot: rogue reserved-role audit failed", "err", err)
	} else if len(rogue) > 0 {
		c.Logger().Warn("boot: roles carry reserved platform permissions but are not platform_admin; "+
			"they grant platform access and should be removed", "roles", rogue)
	}
	if err := c.bootRoot(seedCtx); err != nil {
		return err
	}
	if err := c.bootAsync(); err != nil {
		return err
	}
	if err := c.bootMQ(ctx); err != nil {
		return err
	}
	if err := c.bootRuntimeSettings(); err != nil {
		return err
	}
	if err := c.bootPlugins(ctx, seedCtx); err != nil {
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
//
// When the operator supplies auth.token_secret (env
// SNOOZE_SERVER_AUTH_TOKEN_SECRET) it overrides the DB-generated key from
// EnsureSecrets, so the signing key is stable and operator-controlled across
// rotations of the secrets collection. The configured secret must meet the
// same minimum length NewTokenEngine enforces; we check it here too so the
// failure is a clear boot error rather than a generic engine error.
func (c *Core) bootSecrets(ctx context.Context) error {
	jwtKey, _, err := EnsureSecrets(ctx, c.Driver)
	if err != nil {
		return fmt.Errorf("boot: ensure secrets: %w", err)
	}
	if secret := c.Cfg.Auth.TokenSecret; secret != "" {
		if len(secret) < auth.MinSecretBytes {
			return fmt.Errorf("boot: auth.token_secret must be at least %d bytes (got %d)",
				auth.MinSecretBytes, len(secret))
		}
		jwtKey = []byte(secret)
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
//
// ctx MUST be scoped to the default tenant (the caller passes seedCtx): the
// root user lands in the tenant-scoped "user" collection, so a naked context
// fail-closes with ErrNoTenant.
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
// called. upsert=true is required so that the first increment for a new
// {metric,dim,key,bucket} tuple inserts the counter doc rather than being
// silently dropped by BulkIncrement's no-match skip path.
func (c *Core) bootAsync() error {
	c.Async = asyncwriter.New(c.Driver, asyncFlushInterval, nil, asyncwriter.WithUpsert(true))
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
//
// Multitenancy: processor plugins keep per-tenant in-memory caches and each
// Reload(ctx) only refreshes the tenant in TenantFrom(ctx). plugins.Build runs
// every plugin's PostInit (which typically calls Reload), so it MUST run under a
// tenant-scoped context or those reloads fail-close — we use seedCtx (the
// default tenant). After Build + optional-plugin filtering we walk every active
// tenant and reload each processor under that tenant's scope so the in-memory
// caches are warm for all tenants, not just the default one. Reloading the
// default tenant twice is harmless (Reload is idempotent).
//
// ctx is the plain (unscoped) boot context used only to list tenants via
// housekeeper.ForEachTenant, which lifts to platform scope internally and hands
// fn a per-tenant scoped context.
func (c *Core) bootPlugins(ctx, seedCtx context.Context) error {
	all, procs, err := plugins.Build(seedCtx, c, c.Cfg.Core.ProcessPlugins)
	if err != nil {
		return fmt.Errorf("boot: plugins: %w", err)
	}
	c.plugins = filterOptionalPlugins(all, c.Cfg.Core.EnabledOptionalPlugins)
	c.processOrder = procs

	// Hydrate every active tenant's processor caches. A per-tenant reload error
	// is logged and skipped so one broken tenant cannot block boot; a failure
	// reloading the default tenant is fatal (its caches back the bootstrap data
	// the server itself relies on).
	if err := housekeeper.ForEachTenant(ctx, c.Driver, func(tctx context.Context, tid string) error {
		for _, p := range c.processOrder {
			if rerr := p.Reload(tctx); rerr != nil {
				if tid == snoozetypes.DefaultTenant {
					return fmt.Errorf("reload %s for default tenant: %w", p.Name(), rerr)
				}
				c.Logger().Warn("boot: per-tenant plugin reload failed; skipping tenant",
					"plugin", p.Name(),
					"tenant", tid,
					"err", rerr,
				)
				// Stop reloading the remaining processors for this broken
				// tenant but continue with the next tenant.
				return nil
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("boot: hydrate tenant caches: %w", err)
	}
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

// bootSyncer constructs the per-driver syncer and the node heartbeat, wiring
// in the operator-supplied cfg.Syncer values:
//
//   - Hostname     → NodeHeartbeat.Node (the identity advertised on `nodes`).
//   - SyncInterval → NodeHeartbeat.Interval (heartbeat cadence) and the
//     Syncer's reload-debounce window.
//
// SyncInterval is the single source of truth for both cadences; a zero value
// lets each subsystem apply its own internal default.
func (c *Core) bootSyncer() error {
	interval := c.Cfg.Syncer.SyncInterval.AsDuration()

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
			Bus:      bus,
			Plugins:  plugMap,
			Debounce: interval,
			Logger:   c.Logger(),
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
		Node:     c.Cfg.Syncer.Hostname,
		Interval: interval,
		Logger:   c.Logger(),
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

// ReloadCollections forwards the wrapped plugin's reload dependencies so the
// syncer (which sees only the shim) still subscribes the plugin to the extra
// collections it declares. Plugins that don't implement the optional contract
// yield nil — no extra subscriptions. Without this forwarding the shim would
// mask the underlying plugin's ReloadCollections from the syncer's type
// assertion, silently dropping cross-collection reloads (e.g. notification's
// dependency on the `action` collection).
func (p pluggableShim) ReloadCollections() []string {
	if d, ok := p.plugin.(interface{ ReloadCollections() []string }); ok {
		return d.ReloadCollections()
	}
	return nil
}

// bootHousekeeper registers the default cleanup jobs.
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
		liveIntervalReg(housekeeper.CleanupStatsAsIntervalJob(c.Driver, c.Settings),
			liveInterval(func(h config.HousekeeperConfig) time.Duration { return h.CleanupStats.AsDuration() }, 400*24*time.Hour)),
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
