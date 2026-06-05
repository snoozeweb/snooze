package settings

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
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
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("settings-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func newReadyPlugin(t *testing.T) (*Plugin, *testHost) {
	t.Helper()
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "settings"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p, host
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "settings"))
}

func TestPostInitRoundtrip(t *testing.T) {
	p, _ := newReadyPlugin(t)
	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, "settings", p.Name())
}

func TestValidate(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(nil))
	require.NoError(t, p.Validate(map[string]any{"section": "general"}))
	require.Error(t, p.Validate(map[string]any{"section": ""}))
}

func TestSetGetRoundtrip(t *testing.T) {
	p, _ := newReadyPlugin(t)
	// The settings plugin upserts via ReplaceOne, whose write-side tenant_id
	// stamping is not yet implemented in the driver (see internal/db/dbtest
	// suite: upsert paths run under platform scope). Use platform scope so the
	// fail-closed driver accepts the round-trip.
	ctx := auth.WithPlatformScope(context.Background())

	// Missing key -> (nil, false, nil).
	v, ok, err := p.Get(ctx, "general", "anonymous_enabled")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, v)

	require.NoError(t, p.Set(ctx, "general", "anonymous_enabled", true))
	v, ok, err = p.Get(ctx, "general", "anonymous_enabled")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, true, v)

	// Second Set on same section should preserve the first key.
	require.NoError(t, p.Set(ctx, "general", "default_auth_backend", "ldap"))
	v, ok, _ = p.Get(ctx, "general", "anonymous_enabled")
	require.True(t, ok)
	require.Equal(t, true, v)
	v, ok, _ = p.Get(ctx, "general", "default_auth_backend")
	require.True(t, ok)
	require.Equal(t, "ldap", v)
}

func TestReplaceAndGetSettings(t *testing.T) {
	p, _ := newReadyPlugin(t)
	// Platform scope: ReplaceOne upsert does not yet stamp tenant_id (deferred
	// driver task); the fail-closed driver requires tenant or platform scope.
	ctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, p.Replace(ctx, "ldap", map[string]any{"host": "ldap.example.com", "port": float64(389)}))

	out, err := p.GetSettings(ctx, "ldap")
	require.NoError(t, err)
	require.Equal(t, "ldap.example.com", out["host"])
	require.EqualValues(t, 389, out["port"])

	// Replace overwrites — old keys gone.
	require.NoError(t, p.Replace(ctx, "ldap", map[string]any{"host": "new.example.com"}))
	out, err = p.GetSettings(ctx, "ldap")
	require.NoError(t, err)
	require.NotContains(t, out, "port")
	require.Equal(t, "new.example.com", out["host"])
}

func TestGetSectionUnmarshal(t *testing.T) {
	p, _ := newReadyPlugin(t)
	// Platform scope: ReplaceOne upsert does not yet stamp tenant_id (deferred
	// driver task); the fail-closed driver requires tenant or platform scope.
	ctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, p.Set(ctx, "general", "anonymous_enabled", true))
	var dst struct {
		AnonymousEnabled bool `json:"anonymous_enabled"`
	}
	require.NoError(t, p.GetSection(ctx, "general", &dst))
	require.True(t, dst.AnonymousEnabled)

	// Missing section -> no error, dst untouched.
	var empty struct {
		AnonymousEnabled bool `json:"anonymous_enabled"`
	}
	require.NoError(t, p.GetSection(ctx, "absent", &empty))
	require.False(t, empty.AnonymousEnabled)
}

func TestWatchFanout(t *testing.T) {
	p, _ := newReadyPlugin(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := p.Watch(ctx, "general")
	require.NoError(t, err)
	// Platform scope: ReplaceOne upsert does not yet stamp tenant_id (deferred
	// driver task); the fail-closed driver requires tenant or platform scope.
	require.NoError(t, p.Set(auth.WithPlatformScope(context.Background()), "general", "k", "v"))

	select {
	case evt := <-ch:
		require.Equal(t, "general", evt.Section)
		require.Equal(t, "k", evt.Key)
		require.Equal(t, "v", evt.Value)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected RuntimeChange on watch channel")
	}
}
