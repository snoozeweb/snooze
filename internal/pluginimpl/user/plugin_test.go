package user

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/japannext/snooze/internal/config"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/db/sqlite"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/telemetry"
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
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("user-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "user"))
}

func TestPostInitRoundtrip(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "user"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, "user", p.Name())
}

func TestValidate(t *testing.T) {
	p := &Plugin{}
	t.Run("empty patch ok", func(t *testing.T) {
		require.NoError(t, p.Validate(nil))
		require.NoError(t, p.Validate(map[string]any{}))
	})
	t.Run("valid object", func(t *testing.T) {
		require.NoError(t, p.Validate(map[string]any{"name": "alice", "method": "local"}))
	})
	t.Run("empty name rejected", func(t *testing.T) {
		require.Error(t, p.Validate(map[string]any{"name": "", "method": "local"}))
	})
	t.Run("empty method rejected", func(t *testing.T) {
		require.Error(t, p.Validate(map[string]any{"name": "alice", "method": ""}))
	})
}

func TestSchemaShape(t *testing.T) {
	s := (&Plugin{}).Schema().(map[string]any)
	require.Equal(t, "object", s["type"])
	props := s["properties"].(map[string]any)
	require.Contains(t, props, "name")
	require.Contains(t, props, "method")
}
