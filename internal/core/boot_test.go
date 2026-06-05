package core

import (
	"context"
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
func runSeedPhase(t *testing.T, ctx context.Context, drv db.Driver) string {
	t.Helper()
	seedCtx := snoozetypes.WithTenant(ctx, snoozetypes.DefaultTenant)
	require.NoError(t, auth.BootstrapDB(seedCtx, drv))
	require.NoError(t, BootstrapDB(seedCtx, drv))
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

	pwd := runSeedPhase(t, ctx, drv)
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

	require.NotEmpty(t, runSeedPhase(t, ctx, drv), "first run mints a password")

	platform := snoozetypes.WithPlatformScope(ctx)
	tenantsBefore, _, err := drv.Search(platform, auth.TenantCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	rolesBefore, _, err := drv.Search(platform, auth.RoleCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	usersBefore, _, err := drv.Search(platform, auth.LocalCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)

	// Second run: must be a no-op.
	pwd2 := runSeedPhase(t, ctx, drv)
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
