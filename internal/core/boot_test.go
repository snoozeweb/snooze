package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeProcessorWithDeps is a fakeProcessor that also declares reload
// dependencies (the syncer.ReloadDeps contract).
type fakeProcessorWithDeps struct {
	fakeProcessor
	deps []string
}

func (f *fakeProcessorWithDeps) ReloadCollections() []string { return f.deps }

// TestPluggableShim_ForwardsReloadCollections guards the integration seam: the
// syncer type-asserts its Pluggables to ReloadDeps, but boot.go wraps every
// plugin in pluggableShim. If the shim doesn't forward ReloadCollections, the
// notification plugin's `action` dependency is silently dropped and action
// edits don't propagate to the running dispatcher.
func TestPluggableShim_ForwardsReloadCollections(t *testing.T) {
	t.Parallel()

	// Compile-time: the shim must satisfy the syncer's dependency interface.
	var _ syncer.ReloadDeps = pluggableShim{}

	withDeps := &fakeProcessorWithDeps{
		fakeProcessor: fakeProcessor{name: "notification"},
		deps:          []string{"action"},
	}
	shim := pluggableShim{name: "notification", plugin: withDeps}
	require.Equal(t, []string{"action"}, shim.ReloadCollections(),
		"shim must forward the underlying plugin's reload dependencies")

	// A plugin that declares no dependencies yields nil (no extra subscriptions).
	plain := pluggableShim{name: "rule", plugin: &fakeProcessor{name: "rule"}}
	require.Nil(t, plain.ReloadCollections())
}

// fakeCachedPlugin is a non-processor plugin declaring auto_reload — the shape
// kv has: a tenant-keyed cache the syncer refreshes once per tenant.
type fakeCachedPlugin struct {
	name       string
	autoReload bool
}

func (f *fakeCachedPlugin) Name() string { return f.name }
func (f *fakeCachedPlugin) Metadata() plugins.Metadata {
	return plugins.Metadata{Name: f.name, AutoReload: f.autoReload}
}
func (f *fakeCachedPlugin) PostInit(context.Context, plugins.Host) error { return nil }
func (f *fakeCachedPlugin) Reload(context.Context) error                 { return nil }

// TestTenantCachedPlugins_SelectsAutoReloadNonProcessors guards the boot
// hydration gap: kv holds a tenant-keyed cache but has no Process method, so
// the processor loop never warmed it and only the default tenant (from
// PostInit's seedCtx) was ever loaded.
func TestTenantCachedPlugins_SelectsAutoReloadNonProcessors(t *testing.T) {
	t.Parallel()

	c := &Core{
		plugins: map[string]plugins.Plugin{
			"rule":     &fakeProcessor{name: "rule"},
			"kv":       &fakeCachedPlugin{name: "kv", autoReload: true},
			"reject":   &fakeCachedPlugin{name: "reject", autoReload: true},
			"user":     &fakeCachedPlugin{name: "user"},
			"snoozing": &fakeProcessor{name: "snoozing"},
		},
		processOrder: []plugins.Processor{&fakeProcessor{name: "rule"}},
	}

	names := make([]string, 0, 2)
	for _, p := range c.tenantCachedPlugins() {
		names = append(names, p.Name())
	}
	require.Equal(t, []string{"kv", "reject"}, names,
		"expect auto_reload plugins outside processOrder, in deterministic order")
}

// A plugin that is both auto_reload and a configured processor must not be
// reloaded twice per tenant.
func TestTenantCachedPlugins_SkipsPluginsAlreadyInProcessOrder(t *testing.T) {
	t.Parallel()

	c := &Core{
		plugins: map[string]plugins.Plugin{
			"reject": &fakeCachedPlugin{name: "reject", autoReload: true},
		},
		processOrder: []plugins.Processor{&fakeProcessor{name: "reject"}},
	}
	require.Empty(t, c.tenantCachedPlugins(),
		"processOrder already hydrates it; a second reload per tenant is waste")
}

func TestFilterOptionalPlugins_DropsDefaultDisabled(t *testing.T) {
	t.Parallel()
	all := map[string]plugins.Plugin{
		"rule":    &fakeProcessor{name: "rule"},
		"patlite": &fakeProcessor{name: "patlite"},
	}
	out := filterOptionalPlugins(all, nil)
	require.Contains(t, out, "rule")
	require.NotContains(t, out, "patlite",
		"patlite is optional and must be hidden when not in the enabled list")
}

func TestFilterOptionalPlugins_KeepsExplicitlyEnabled(t *testing.T) {
	t.Parallel()
	all := map[string]plugins.Plugin{
		"rule":    &fakeProcessor{name: "rule"},
		"patlite": &fakeProcessor{name: "patlite"},
	}
	out := filterOptionalPlugins(all, []string{"patlite"})
	require.Contains(t, out, "rule")
	require.Contains(t, out, "patlite",
		"patlite must remain when listed in enabled_optional_plugins")
}

func TestFilterOptionalPlugins_UnknownNameInEnabledIsIgnored(t *testing.T) {
	t.Parallel()
	all := map[string]plugins.Plugin{"rule": &fakeProcessor{name: "rule"}}
	out := filterOptionalPlugins(all, []string{"ghost"})
	require.Len(t, out, 1)
	require.Contains(t, out, "rule")
}

// TestBootAsync_UpsertEnabled_StatsCountersPersist is the production-wiring
// regression test for the shared async writer. It exercises the REAL bootAsync
// path (not a hand-built writer) against a real SQLite driver and asserts that
// the first RecordStat increment for a new {metric,dim,key,bucket} tuple
// actually creates a document in the stats collection.
//
// The test MUST FAIL when bootAsync builds asyncwriter.New without
// asyncwriter.WithUpsert(true), because BulkIncrement silently skips
// non-matching searches when upsert=false, leaving the stats collection empty.
func TestBootAsync_UpsertEnabled_StatsCountersPersist(t *testing.T) {
	t.Parallel()
	// Platform scope: this exercises the stats counter pipeline (a tenant-scoped
	// collection) through the real driver, which now resolves tenancy via
	// db.TenantScope. Platform scope bypasses tenant_id injection so the test
	// stays about asyncwriter upsert wiring, not tenancy.
	ctx := snoozetypes.WithPlatformScope(context.Background())

	// Open a real SQLite database in a per-test temp file (same pattern as
	// internal/db/sqlite/driver_test.go newTestDriver).
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(ctx, sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	// Wire a minimal Core with the real driver and the default config
	// (MetricsEnabled=true by schema.DefaultGeneral).
	cfg := config.Default()
	c := &Core{
		Cfg:    cfg,
		Driver: drv,
	}

	// bootAsync is the production code under test: it must build the writer
	// with upsert=true, otherwise the increment below will be lost.
	require.NoError(t, c.bootAsync())
	require.NotNil(t, c.Async)

	// Enqueue one stat increment through the same call path that the alert
	// pipeline uses. eventEpoch 1780302245 → hour bucket 1780300800.
	plugins.RecordStat(ctx, c, 1780302245, "alert_hit", map[string]string{"source": "syslog"}, 1)

	// Flush synchronously so we don't need to start the Run goroutine.
	require.NoError(t, c.Async.Flush(ctx))

	// Assert that the counter doc was written to the stats collection.
	// If upsert=false the doc does not exist and GetOne returns ErrNotFound,
	// which makes the test fail — the intended signal when Fix 1 is reverted.
	doc, err := drv.GetOne(ctx, "stats", map[string]any{
		"metric": "alert_hit",
		"dim":    "source",
		"key":    "syslog",
	})
	require.NoError(t, err, "stats doc must exist after Flush; "+
		"if missing, bootAsync is building the writer with upsert=false")
	// value should be exactly 1 (we incremented by 1 from a zero baseline).
	require.EqualValues(t, 1, doc["value"],
		"counter value must equal the increment delta")
}

// TestProcessRecord_CounterBucketsAtIngestTime is the end-to-end regression for
// the dashboard's "TOTAL 0 next to OPEN 8" contradiction. An ingested alert
// carries no date_epoch of its own — the storage driver stamps it at write
// time — so the pipeline recorded its counters with eventEpoch 0 and every
// counter document landed in the 1970-01-01 hour bucket, i.e. outside every
// window the dashboard can ask for. The counter must be filed under the hour
// the alert was actually ingested.
func TestProcessRecord_CounterBucketsAtIngestTime(t *testing.T) {
	t.Parallel()
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(ctx, sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	c := &Core{Cfg: config.Default(), Driver: drv}
	require.NoError(t, c.bootAsync())

	// No date_epoch on the way in — exactly what POST /api/v1/alerts receives.
	_, action, err := c.ProcessRecord(ctx, snoozetypes.Record{
		Host: "srv-a", Source: "syslog", Message: "disk full", Severity: "critical",
	})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, action)
	require.NoError(t, c.Async.Flush(ctx))

	doc, err := drv.GetOne(ctx, "stats", map[string]any{
		"metric": "alert_hit", "dim": "source", "key": "syslog",
	})
	require.NoError(t, err, "ingesting an alert must write an alert_hit counter")

	var bucket int64
	switch v := doc["bucket"].(type) {
	case int64:
		bucket = v
	case float64:
		bucket = int64(v)
	default:
		t.Fatalf("unexpected bucket type %T", doc["bucket"])
	}
	require.Equal(t, time.Now().UTC().Truncate(time.Hour).Unix(), bucket,
		"counter must sit in the current hour bucket, not the epoch-0 one")
}

// TestBootSecrets_PrefersConfiguredTokenSecret is the production-wiring
// regression for FEATURE 1: when auth.token_secret is set the TokenEngine must
// be built from THAT key, not the DB-generated one from EnsureSecrets. We prove
// it by signing a token at boot and verifying it succeeds with an independent
// engine built from the configured secret AND fails with an engine built from
// a different secret. If bootSecrets reverts to always using the DB key, the
// configured-secret verify fails and the test catches it.
func TestBootSecrets_PrefersConfiguredTokenSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const configured = "this-is-a-stable-operator-supplied-secret-key" // ≥32 bytes
	cfg := config.Default()
	cfg.Auth.TokenSecret = configured

	c := &Core{Cfg: cfg, Driver: newFakeDB()}
	require.NoError(t, c.bootSecrets(ctx))
	require.NotNil(t, c.Tokens)

	tok, _, err := c.Tokens.Sign(snoozetypes.Claims{Subject: "alice", Method: "local"})
	require.NoError(t, err)

	// An engine built from the configured secret must accept the token.
	want, err := auth.NewTokenEngine([]byte(configured), cfg.Auth)
	require.NoError(t, err)
	claims, err := want.Verify(tok)
	require.NoError(t, err, "token signed at boot must verify with the configured secret")
	require.Equal(t, "alice", claims.Subject)

	// An engine built from a *different* secret must reject it — proving the
	// boot engine did not silently fall back to the DB-generated key.
	other, err := auth.NewTokenEngine([]byte("a-completely-different-32byte-secret!!"), cfg.Auth)
	require.NoError(t, err)
	_, err = other.Verify(tok)
	require.Error(t, err, "token must NOT verify under an unrelated secret")
}

// TestBootSecrets_RejectsShortTokenSecret asserts the boot-time length guard:
// a configured secret below auth.MinSecretBytes must abort boot with a clear,
// attributable error rather than a generic engine failure.
func TestBootSecrets_RejectsShortTokenSecret(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Auth.TokenSecret = "too-short"

	c := &Core{Cfg: cfg, Driver: newFakeDB()}
	err := c.bootSecrets(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "auth.token_secret")
}

// TestBootSecrets_EmptyTokenSecretUsesDBKey guards the default path: with no
// configured secret, the engine is built from the EnsureSecrets DB key and the
// boot still succeeds (the configured-secret override is opt-in only).
func TestBootSecrets_EmptyTokenSecretUsesDBKey(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	require.Empty(t, cfg.Auth.TokenSecret)

	c := &Core{Cfg: cfg, Driver: newFakeDB()}
	require.NoError(t, c.bootSecrets(context.Background()))
	require.NotNil(t, c.Tokens)
}

// TestBootSyncer_HonorsConfig is the production-wiring regression for FEATURE
// 2: bootSyncer must propagate cfg.Syncer into the NodeHeartbeat (Node +
// Interval). The fakeDB's Watcher returns nil, so c.Sync is skipped while
// c.Heart is still constructed — exactly the surface this test pins.
func TestBootSyncer_HonorsConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Syncer.Hostname = "node-XYZ"
	cfg.Syncer.SyncInterval = schema.Duration(7 * time.Second)

	c := &Core{Cfg: cfg, Driver: newFakeDB()}
	require.NoError(t, c.bootSyncer())

	require.NotNil(t, c.Heart)
	require.Equal(t, "node-XYZ", c.Heart.Node,
		"heartbeat Node must come from cfg.Syncer.Hostname")
	require.Equal(t, 7*time.Second, c.Heart.Interval,
		"heartbeat Interval must come from cfg.Syncer.SyncInterval")
}

// TestBootSyncer_PropagatesDebounce pins that the configured interval also
// drives the Syncer's reload-debounce window when a watcher bus is present.
func TestBootSyncer_PropagatesDebounce(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Syncer.SyncInterval = schema.Duration(3 * time.Second)

	c := &Core{Cfg: cfg, Driver: busFakeDB{newFakeDB()}}
	require.NoError(t, c.bootSyncer())

	require.NotNil(t, c.Sync, "a non-nil Watcher must yield a live Syncer")
	require.Equal(t, 3*time.Second, c.Sync.Debounce,
		"syncer debounce must come from cfg.Syncer.SyncInterval")
}

// busFakeDB is a fakeDB whose Watcher returns a non-nil bus so bootSyncer
// takes the Syncer-construction branch. The embedded fakeDB supplies every
// other Driver method.
type busFakeDB struct{ *fakeDB }

func (b busFakeDB) Watcher() syncer.Bus { return stubBus{} }

// stubBus is a no-op syncer.Bus used only to make Watcher non-nil; the test
// never publishes or subscribes through it.
type stubBus struct{}

func (stubBus) Publish(context.Context, syncer.Event) error { return nil }
func (stubBus) Subscribe(context.Context, string) (<-chan syncer.Event, error) {
	return nil, nil
}
func (stubBus) Close() error { return nil }

// runSeedPhase replays the exact first-boot seeding order that bootstrap()
// performs under seedCtx (auth.BootstrapDB → core.BootstrapDB → EnsureRoot),
// against the supplied driver. It deliberately does NOT touch plugins.Build
// (one-shot per process), keeping this test re-runnable to prove idempotency.
func runSeedPhase(ctx context.Context, t *testing.T, drv db.Driver) string {
	t.Helper()
	seedCtx := snoozetypes.WithTenant(ctx, snoozetypes.DefaultTenant)
	require.NoError(t, auth.BootstrapDB(seedCtx, drv))
	require.NoError(t, BootstrapDB(seedCtx, drv, ""))
	pwd, err := auth.EnsureRoot(seedCtx, drv)
	require.NoError(t, err)
	return pwd
}

// TestBootstrapSeed_RunsUnderDefaultTenantScope is the regression test for the
// boot wiring fix: every first-boot seed (the default tenant doc, the
// platform_admin role, the default RBAC roles, the init marker, and the root
// user) must succeed against the real tenancy-enforcing SQLite driver, which
// fail-closes with ErrNoTenant on tenant-scoped collections under a naked
// context. The presence of these docs proves seeding ran under a context scoped
// to the default tenant, not the naked boot ctx.
func TestBootstrapSeed_RunsUnderDefaultTenantScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(ctx, sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	pwd := runSeedPhase(ctx, t, drv)
	require.NotEmpty(t, pwd, "first boot must mint a root password")

	// Read everything back under platform scope so the driver does not filter by
	// tenant; this lets us inspect the stamped tenant_id directly.
	platform := snoozetypes.WithPlatformScope(ctx)

	// 1. Default tenant doc exists in the global tenant collection.
	tenants, _, err := drv.Search(platform, auth.TenantCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	var foundDefault bool
	for _, d := range tenants {
		if d["id"] == snoozetypes.DefaultTenant {
			foundDefault = true
			require.Equal(t, "active", d["status"])
		}
	}
	require.True(t, foundDefault, "default tenant doc must be seeded")

	// 2. platform_admin role exists, stamped with tenant_id=default and holding
	//    the tenant read/write permissions.
	roles, _, err := drv.Search(platform, auth.RoleCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	roleByName := map[string]db.Document{}
	for _, r := range roles {
		if n, ok := r["name"].(string); ok {
			roleByName[n] = r
		}
	}
	pa, ok := roleByName[auth.PlatformAdminRole]
	require.True(t, ok, "platform_admin role must be seeded")
	require.Equal(t, snoozetypes.DefaultTenant, pa["tenant_id"],
		"platform_admin role must be stamped with the default tenant")
	require.Contains(t, toStrings(pa["permissions"]), auth.PermReadTenant)
	require.Contains(t, toStrings(pa["permissions"]), auth.PermWriteTenant)

	// 3. Default RBAC roles from core.BootstrapDB are present.
	require.Contains(t, roleByName, "admin")
	require.Contains(t, roleByName, "viewer")
	require.Contains(t, roleByName, "notifications")

	// 4. Root user exists, stamped tenant_id=default, granted platform_admin.
	root, err := drv.GetOne(platform, auth.LocalCollection, db.Document{
		"name":   auth.RootUsername,
		"method": auth.LocalMethod,
	})
	require.NoError(t, err)
	require.NotNil(t, root)
	require.Equal(t, snoozetypes.DefaultTenant, root["tenant_id"],
		"root user must be stamped with the default tenant")
	require.Contains(t, toStrings(root["roles"]), auth.PlatformAdminRole)
	require.Contains(t, toStrings(root["roles"]), "admin")
}

// TestBootstrapSeed_Idempotent proves the seed phase is a no-op on a second run:
// the root password is empty (user already exists) and no duplicate tenant /
// role / user docs are created.
func TestBootstrapSeed_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(ctx, sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	require.NotEmpty(t, runSeedPhase(ctx, t, drv), "first run mints a password")

	platform := snoozetypes.WithPlatformScope(ctx)
	tenantsBefore, _, err := drv.Search(platform, auth.TenantCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	rolesBefore, _, err := drv.Search(platform, auth.RoleCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	usersBefore, _, err := drv.Search(platform, auth.LocalCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)

	// Second run: must be a no-op.
	pwd2 := runSeedPhase(ctx, t, drv)
	require.Empty(t, pwd2, "re-running the seed phase must not mint a new root password")

	tenantsAfter, _, err := drv.Search(platform, auth.TenantCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	rolesAfter, _, err := drv.Search(platform, auth.RoleCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	usersAfter, _, err := drv.Search(platform, auth.LocalCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)

	require.Len(t, tenantsAfter, len(tenantsBefore), "no duplicate tenant docs")
	require.Len(t, rolesAfter, len(rolesBefore), "no duplicate role docs")
	require.Len(t, usersAfter, len(usersBefore), "no duplicate user docs")
}

// toStrings normalises a []string / []any value read back from the driver into
// a []string for membership assertions.
func toStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// TestHousekeeperJobs_NotificationLogSweepsDaily pins the cadence of the
// delivery-log retention job. `housekeeping.cleanup_notificationlog` is the
// RETENTION WINDOW (default 720h), not a period: wiring it through
// liveInterval made the sweep fire every 30 days, so a row could live up to 60
// days while the docs promised a daily sweep. The cadence must stay fixed at
// 24h no matter what the retention is set to.
func TestHousekeeperJobs_NotificationLogSweepsDaily(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Housekeeper.CleanupNotificationLog = schema.Duration(720 * time.Hour)

	c := &Core{Cfg: cfg, Driver: newFakeDB()}
	jobs := c.housekeeperJobs(nil)

	var found bool
	for _, j := range jobs {
		if j.name != "cleanup_notificationlog" {
			continue
		}
		found = true
		require.NotNil(t, j.sched.LiveInterval)
		require.Equal(t, 24*time.Hour, j.sched.LiveInterval(context.Background()),
			"the delivery-log sweep must run daily, not once per retention window")
	}
	require.True(t, found, "cleanup_notificationlog must be registered")
}

// TestHousekeeperJobs_LiveCadencesStillTrackSettings is the counterpart guard:
// the jobs whose knob genuinely IS a cadence must keep reading it live, so the
// fix above did not accidentally freeze the rest of the table.
func TestHousekeeperJobs_LiveCadencesStillTrackSettings(t *testing.T) {
	t.Parallel()
	cfg := config.Default()

	c := &Core{Cfg: cfg, Driver: newFakeDB()}
	jobs := c.housekeeperJobs(nil)

	want := map[string]time.Duration{
		"cleanup_timeout/record": 5 * time.Minute,
		"cleanup_aggregate":      time.Minute,
		"cleanup_apikey":         time.Hour,
	}
	seen := map[string]time.Duration{}
	for _, j := range jobs {
		if _, ok := want[j.name]; !ok {
			continue
		}
		require.NotNil(t, j.sched.LiveInterval, "%s must resolve its interval live", j.name)
		seen[j.name] = j.sched.LiveInterval(context.Background())
	}
	require.Equal(t, want, seen)
}

// fakeLifecyclePlugin implements plugins.LifecycleHook so the shutdown drain
// can be observed.
type fakeLifecyclePlugin struct {
	name      string
	stopCalls int
	stopCtx   context.Context
	stopErr   error
}

func (f *fakeLifecyclePlugin) Name() string                                 { return f.name }
func (f *fakeLifecyclePlugin) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: f.name} }
func (f *fakeLifecyclePlugin) PostInit(context.Context, plugins.Host) error { return nil }
func (f *fakeLifecyclePlugin) Reload(context.Context) error                 { return nil }
func (f *fakeLifecyclePlugin) Start(context.Context) error                  { return nil }
func (f *fakeLifecyclePlugin) Stop(ctx context.Context) error {
	f.stopCalls++
	f.stopCtx = ctx
	return f.stopErr
}

// TestStopPlugins_CallsLifecycleHooks pins the shutdown drain. Before this
// existed, plugins.LifecycleHook.Stop was never invoked anywhere in the
// server: the batching notifiers' shutdown flush (and its batch_reason
// "shutdown" delivery row) was dead code, and queued-but-unsent alerts
// vanished on SIGTERM.
func TestStopPlugins_CallsLifecycleHooks(t *testing.T) {
	t.Parallel()
	hooked := &fakeLifecyclePlugin{name: "webhook"}
	plain := &fakeProcessor{name: "rule"}

	c := &Core{
		Cfg:     config.Default(),
		plugins: map[string]plugins.Plugin{"webhook": hooked, "rule": plain},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.StopPlugins(ctx)

	require.Equal(t, 1, hooked.stopCalls, "a LifecycleHook plugin must be stopped")
	require.NotNil(t, hooked.stopCtx)
	_, hasDeadline := hooked.stopCtx.Deadline()
	require.True(t, hasDeadline, "the shutdown deadline must reach the hook")
}

// TestStopPlugins_SurvivesAFailingHook: one plugin refusing to stop must not
// prevent the rest from draining, and must not panic the shutdown path.
func TestStopPlugins_SurvivesAFailingHook(t *testing.T) {
	t.Parallel()
	bad := &fakeLifecyclePlugin{name: "aaa-bad", stopErr: errors.New("boom")}
	good := &fakeLifecyclePlugin{name: "zzz-good"}

	c := &Core{
		Cfg:     config.Default(),
		plugins: map[string]plugins.Plugin{bad.name: bad, good.name: good},
	}
	c.StopPlugins(context.Background())

	require.Equal(t, 1, bad.stopCalls)
	require.Equal(t, 1, good.stopCalls, "a failing hook must not abort the drain")
}

// TestStopPlugins_NilSafe: the shutdown defer runs on every exit path,
// including ones where Core construction half-failed.
func TestStopPlugins_NilSafe(t *testing.T) {
	t.Parallel()
	var c *Core
	require.NotPanics(t, func() { c.StopPlugins(context.Background()) })
	require.NotPanics(t, func() { (&Core{Cfg: config.Default()}).StopPlugins(context.Background()) })
}

// TestBackfillNotificationsRolePerms covers the migration path for installs
// that booted before the delivery log existed: BootstrapDB short-circuits on
// the init_db marker, so the new default grant has to be applied separately or
// the Deliveries tab 403s for everyone holding only the notifications role.
func TestBackfillNotificationsRolePerms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(ctx, sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	platform := snoozetypes.WithPlatformScope(ctx)
	// Two active tenants; the backfill must reach both.
	_, err = drv.Write(platform, auth.TenantCollection, []db.Document{
		{"id": snoozetypes.DefaultTenant, "status": "active"},
		{"id": "acme", "status": "active"},
	}, db.WriteOptions{Primary: []string{"id"}})
	require.NoError(t, err)

	seed := func(tenant string, docs []db.Document) {
		tctx := auth.WithTenant(ctx, tenant)
		_, err := drv.Write(tctx, auth.RoleCollection, docs, db.WriteOptions{Primary: []string{"name"}})
		require.NoError(t, err)
	}
	seed(snoozetypes.DefaultTenant, []db.Document{
		{"name": "notifications", "permissions": []any{"rw_notification"}}, // legacy: needs the grant
		{"name": "viewer", "permissions": []any{"ro_all"}},                 // untouched: not the target role
	})
	seed("acme", []db.Document{
		{"name": "notifications", "permissions": []any{"rw_notification", "ro_all"}}, // ro_all subsumes it
	})

	require.NoError(t, BackfillNotificationsRolePerms(ctx, drv))

	perms := func(tenant, role string) []string {
		doc, err := drv.GetOne(auth.WithTenant(ctx, tenant), auth.RoleCollection, db.Document{"name": role})
		require.NoError(t, err)
		require.NotNil(t, doc)
		return toStrings(doc["permissions"])
	}
	require.Equal(t, []string{"rw_notification", "ro_notificationlog"},
		perms(snoozetypes.DefaultTenant, "notifications"),
		"the legacy role must gain the delivery-log read grant")
	require.Equal(t, []string{"ro_all"}, perms(snoozetypes.DefaultTenant, "viewer"),
		"unrelated roles must not be touched")
	require.Equal(t, []string{"rw_notification", "ro_all"}, perms("acme", "notifications"),
		"ro_all already subsumes the grant — no redundant permission added")

	// Idempotent: a second sweep changes nothing.
	require.NoError(t, BackfillNotificationsRolePerms(ctx, drv))
	require.Equal(t, []string{"rw_notification", "ro_notificationlog"},
		perms(snoozetypes.DefaultTenant, "notifications"))
}

// TestBackfillNotificationsRolePerms_NilDriver: the boot path calls this
// unconditionally, so it must not panic on a half-built Core.
func TestBackfillNotificationsRolePerms_NilDriver(t *testing.T) {
	t.Parallel()
	require.Error(t, BackfillNotificationsRolePerms(context.Background(), nil))
}
