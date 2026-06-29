package tenantmatch

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

type testHost struct{ drv *sqlite.Driver }

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return &testHost{drv: drv}
}

func (h *testHost) DB() db.Driver                { return h.drv }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return slog.Default() }
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("tenantmatch-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestPluginName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "tenant_match", New().Name())
}

func TestPrimaryKey(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"match_type", "match"}, New().PrimaryKey())
}

func TestValidate_AcceptsValidDoc(t *testing.T) {
	t.Parallel()
	p := New()
	require.NoError(t, p.Validate(map[string]any{
		"match_type": "group", "match": "ops", "tenant_id": "acme",
	}))
	// PATCH partial (empty body) is tolerated.
	require.NoError(t, p.Validate(nil))
}

func TestValidate_UnknownMatchType(t *testing.T) {
	t.Parallel()
	p := New()
	require.Error(t, p.Validate(map[string]any{
		"match_type": "carrier-pigeon", "match": "ops", "tenant_id": "acme",
	}))
}

func TestValidate_EmptyTenantID(t *testing.T) {
	t.Parallel()
	p := New()
	require.Error(t, p.Validate(map[string]any{
		"match_type": "group", "match": "ops", "tenant_id": "",
	}))
	// Missing tenant_id when match_type is present is also rejected.
	require.Error(t, p.Validate(map[string]any{
		"match_type": "group", "match": "ops",
	}))
}

func TestValidate_EmptyMatch(t *testing.T) {
	t.Parallel()
	p := New()
	require.Error(t, p.Validate(map[string]any{
		"match_type": "group", "match": "", "tenant_id": "acme",
	}))
}

// TestValidate_DuplicateMatchTypeMatch exercises the DB-aware integrity check:
// a second rule for the same (match_type, match) pair is rejected.
func TestValidate_DuplicateMatchTypeMatch(t *testing.T) {
	host := newTestHost(t)
	p := New()
	require.NoError(t, p.PostInit(context.Background(), host))
	ctx := auth.WithPlatformScope(context.Background())

	// Seed the target tenant so the existence check passes, plus an existing rule.
	_, err := host.drv.Write(ctx, "tenant", []db.Document{{"id": "acme", "status": "active"}}, db.WriteOptions{})
	require.NoError(t, err)
	_, err = host.drv.Write(ctx, "tenant_match", []db.Document{
		{"match_type": "group", "match": "ops", "tenant_id": "acme"},
	}, db.WriteOptions{Primary: []string{"match_type", "match"}})
	require.NoError(t, err)

	// A new doc for the same (group, ops) pair must be rejected by the DB-aware check.
	err = p.TransformWrite(ctx, map[string]any{
		"match_type": "group", "match": "ops", "tenant_id": "acme",
	})
	require.Error(t, err, "duplicate (match_type, match) must be rejected")
}

// TestValidate_UnknownTenant exercises referential integrity: the tenant_id
// must name an existing tenant.
func TestValidate_UnknownTenant(t *testing.T) {
	host := newTestHost(t)
	p := New()
	require.NoError(t, p.PostInit(context.Background(), host))
	ctx := auth.WithPlatformScope(context.Background())

	err := p.TransformWrite(ctx, map[string]any{
		"match_type": "group", "match": "ops", "tenant_id": "ghost",
	})
	require.Error(t, err, "rule targeting a non-existent tenant must be rejected")
}

// TestTransformWrite_AllowsExistingTenant: a rule whose tenant exists passes.
func TestTransformWrite_AllowsExistingTenant(t *testing.T) {
	host := newTestHost(t)
	p := New()
	require.NoError(t, p.PostInit(context.Background(), host))
	ctx := auth.WithPlatformScope(context.Background())

	_, err := host.drv.Write(ctx, "tenant", []db.Document{{"id": "acme", "status": "active"}}, db.WriteOptions{})
	require.NoError(t, err)

	require.NoError(t, p.TransformWrite(ctx, map[string]any{
		"match_type": "domain", "match": "example.com", "tenant_id": "acme",
	}))
}

func TestCollectionIsGlobal(t *testing.T) {
	host := newTestHost(t)
	p := New()
	require.NoError(t, p.PostInit(context.Background(), host))
	require.True(t, db.IsGlobalCollection("tenant_match"),
		"tenant_match must be registered as a global (non-tenant-scoped) collection")
}

func TestPluginImplementsInterfaces(t *testing.T) {
	t.Parallel()
	var p any = New()
	_, ok := p.(plugins.Plugin)
	require.True(t, ok, "must implement plugins.Plugin")
	_, ok = p.(plugins.DataModel)
	require.True(t, ok, "must implement plugins.DataModel")
	_, ok = p.(plugins.PrimaryKeyer)
	require.True(t, ok, "must implement plugins.PrimaryKeyer")
	_, ok = p.(plugins.WriteTransformer)
	require.True(t, ok, "must implement plugins.WriteTransformer")
}

func TestRegistration(t *testing.T) {
	t.Parallel()
	found := false
	for _, n := range plugins.Registered() {
		if n == "tenant_match" {
			found = true
		}
	}
	require.True(t, found, "tenant_match must self-register via init()")
}

// TestSetResolverLoad verifies the plugin loads rules into a wired resolver on
// Reload, projecting the documents into matchRules consumed by the resolver.
func TestSetResolverLoad(t *testing.T) {
	host := newTestHost(t)
	p := New()
	require.NoError(t, p.PostInit(context.Background(), host))
	ctx := auth.WithPlatformScope(context.Background())

	_, err := host.drv.Write(ctx, "tenant", []db.Document{{"id": "acme", "status": "active"}}, db.WriteOptions{})
	require.NoError(t, err)
	_, err = host.drv.Write(ctx, "tenant_match", []db.Document{
		{"match_type": "group", "match": "ops", "tenant_id": "acme", "priority": float64(0)},
	}, db.WriteOptions{Primary: []string{"match_type", "match"}})
	require.NoError(t, err)

	resolver := auth.NewTenantMatchResolver()
	p.SetResolver(resolver)
	require.NoError(t, p.Reload(ctx))

	got, err := resolver.Resolve("", auth.Identity{Username: "alice", Groups: []string{"ops"}})
	require.NoError(t, err)
	require.Equal(t, "acme", got)

	// Sanity: the default tenant for an unmatched user (open mode).
	got, err = resolver.Resolve("", auth.Identity{Username: "nobody"})
	require.NoError(t, err)
	require.Equal(t, snoozetypes.DefaultTenant, got)
}
