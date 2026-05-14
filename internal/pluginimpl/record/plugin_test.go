package record

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

// testHost is a Host backed by a fresh per-test sqlite database.
type testHost struct {
	drv *sqlite.Driver
}

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
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("record-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "record"),
		"record plugin should be registered after init()")
}

func TestPostInitRoundtrip(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "record"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	require.Same(t, host, p.host)
	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, "record", p.Name())
}

func TestSchemaAndValidate(t *testing.T) {
	p := &Plugin{}
	require.NotNil(t, p.Schema())
	require.NoError(t, p.Validate(map[string]any{"host": "alpha"}))
	require.NoError(t, p.Validate(nil))
}
