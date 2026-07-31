package kv

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
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
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("kv-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "kv"))
}

func TestPostInitHydratesCache(t *testing.T) {
	host := newTestHost(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err := host.DB().Write(ctx, "kv", []db.Document{
		{"dict": "colors", "key": "red", "value": "#f00"},
		{"dict": "colors", "key": "green", "value": "#0f0"},
		{"dict": "shapes", "key": "circle", "value": float64(1)},
	}, db.WriteOptions{})
	require.NoError(t, err)

	p := &Plugin{meta: plugins.Metadata{Name: "kv"}}
	require.NoError(t, p.PostInit(ctx, host))

	require.True(t, p.TenantLoaded(ctx))

	v, ok := p.Get(ctx, "colors", "red")
	require.True(t, ok)
	require.Equal(t, "#f00", v)

	_, ok = p.Get(ctx, "colors", "blue")
	require.False(t, ok)

	v, ok = p.Get(ctx, "shapes", "circle")
	require.True(t, ok)
	require.EqualValues(t, 1, v)
}

// TestTenantIsolation is the regression test for the flat cache: reloading one
// tenant used to replace the whole map, so whichever tenant reloaded last
// served its values to every other tenant.
func TestTenantIsolation(t *testing.T) {
	host := newTestHost(t)
	defCtx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	otherCtx := auth.WithTenant(context.Background(), "acme")

	_, err := host.DB().Write(defCtx, "kv", []db.Document{
		{"dict": "owner", "key": "web01", "value": "alice"},
	}, db.WriteOptions{})
	require.NoError(t, err)
	_, err = host.DB().Write(otherCtx, "kv", []db.Document{
		{"dict": "owner", "key": "web01", "value": "bob"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	p := &Plugin{meta: plugins.Metadata{Name: "kv"}}
	require.NoError(t, p.PostInit(defCtx, host))
	// Hydrating the second tenant must not evict the first.
	require.NoError(t, p.Reload(otherCtx))

	v, ok := p.Get(defCtx, "owner", "web01")
	require.True(t, ok)
	require.Equal(t, "alice", v, "default tenant must not see the other tenant's value")

	v, ok = p.Get(otherCtx, "owner", "web01")
	require.True(t, ok)
	require.Equal(t, "bob", v)

	// Reloading the default tenant again must likewise leave "acme" intact.
	require.NoError(t, p.Reload(defCtx))
	v, ok = p.Get(otherCtx, "owner", "web01")
	require.True(t, ok)
	require.Equal(t, "bob", v)
}

// TestUnknownTenantIsAMissNotALeak covers a tenant that was never hydrated: it
// must report unloaded (so the rule plugin falls back to the DB) rather than
// silently serving another tenant's bucket.
func TestUnknownTenantIsAMissNotALeak(t *testing.T) {
	host := newTestHost(t)
	defCtx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err := host.DB().Write(defCtx, "kv", []db.Document{
		{"dict": "owner", "key": "web01", "value": "alice"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	p := &Plugin{meta: plugins.Metadata{Name: "kv"}}
	require.NoError(t, p.PostInit(defCtx, host))

	ghostCtx := auth.WithTenant(context.Background(), "never-hydrated")
	require.False(t, p.TenantLoaded(ghostCtx))
	_, ok := p.Get(ghostCtx, "owner", "web01")
	require.False(t, ok)
}

// TestNakedContextIsSkipped pins the guard that silences the syncer's
// tenant-less fan-out entry: kv is tenant-scoped, so a naked reload is a no-op
// rather than a fail-closed ErrNoTenant surfacing as a periodic WARN.
func TestNakedContextIsSkipped(t *testing.T) {
	host := newTestHost(t)
	defCtx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err := host.DB().Write(defCtx, "kv", []db.Document{
		{"dict": "owner", "key": "web01", "value": "alice"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	p := &Plugin{meta: plugins.Metadata{Name: "kv"}}
	require.NoError(t, p.PostInit(defCtx, host))

	naked := context.Background()
	require.NoError(t, p.Reload(naked), "naked reload must not error")
	require.False(t, p.TenantLoaded(naked))
	_, ok := p.Get(naked, "owner", "web01")
	require.False(t, ok)

	// The skipped reload must not have disturbed the hydrated tenant.
	v, ok := p.Get(defCtx, "owner", "web01")
	require.True(t, ok)
	require.Equal(t, "alice", v)
}

func TestValidate(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(nil))
	require.NoError(t, p.Validate(map[string]any{"dict": "d", "key": "k", "value": 1}))
	require.Error(t, p.Validate(map[string]any{"dict": "", "key": "k"}))
	require.Error(t, p.Validate(map[string]any{"dict": "d", "key": ""}))
}
