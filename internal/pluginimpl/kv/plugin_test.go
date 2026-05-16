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
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("kv-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "kv"))
}

func TestPostInitHydratesCache(t *testing.T) {
	host := newTestHost(t)
	ctx := context.Background()
	_, err := host.DB().Write(ctx, "kv", []db.Document{
		{"dict": "colors", "key": "red", "value": "#f00"},
		{"dict": "colors", "key": "green", "value": "#0f0"},
		{"dict": "shapes", "key": "circle", "value": float64(1)},
	}, db.WriteOptions{})
	require.NoError(t, err)

	p := &Plugin{meta: plugins.Metadata{Name: "kv"}}
	require.NoError(t, p.PostInit(ctx, host))

	v, ok := p.Get("colors", "red")
	require.True(t, ok)
	require.Equal(t, "#f00", v)

	_, ok = p.Get("colors", "blue")
	require.False(t, ok)

	v, ok = p.Get("shapes", "circle")
	require.True(t, ok)
	require.EqualValues(t, 1, v)
}

func TestValidate(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(nil))
	require.NoError(t, p.Validate(map[string]any{"dict": "d", "key": "k", "value": 1}))
	require.Error(t, p.Validate(map[string]any{"dict": "", "key": "k"}))
	require.Error(t, p.Validate(map[string]any{"dict": "d", "key": ""}))
}
