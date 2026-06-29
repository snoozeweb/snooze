package core

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/pluginimpl/rule"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// csmHost is a minimal plugins.Host backed by a real sqlite driver, used to
// drive the rule plugin's PostInit/Reload from this package's integration test.
// It mirrors the host stub in internal/pluginimpl/rule/plugin_test.go.
type csmHost struct {
	driver db.Driver
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
	tracer trace.Tracer
}

func (h *csmHost) DB() db.Driver                { return h.driver }
func (h *csmHost) Bus() plugins.Bus             { return nil }
func (h *csmHost) Logger() *slog.Logger         { return h.logger }
func (h *csmHost) Tracer() trace.Tracer         { return h.tracer }
func (h *csmHost) Metrics() *telemetry.Registry { return h.metr }
func (h *csmHost) Config() *config.Config       { return h.cfg }
func (h *csmHost) Plugin(string) plugins.Plugin { return nil }

// TestCustomSourceMapping_EndToEnd exercises the documented no-Go custom-source
// onboarding recipe end to end: a foreign JSON body is posted to the alert
// processor, a pre-seeded rule remaps the foreign field names onto canonical
// Snooze fields, and the `_preserve_raw` ingest hint archives the original
// payload in `raw` before the rule's DELETEs strip the foreign keys.
func TestCustomSourceMapping_EndToEnd(t *testing.T) {
	t.Parallel()

	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	// Real SQLite driver in a per-test temp file (same pattern as boot_test.go).
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(ctx, sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	// Seed the my-tool mapping rule: read the foreign keys (visible to the rule
	// via Extra) and write the canonical fields, then drop the foreign keys.
	_, err = drv.Write(ctx, "rule", []db.Document{{
		"name":      "my-tool-mapping",
		"condition": []any{"=", "source", "my-tool"},
		"modifications": []any{
			[]any{"SET", "host", "{{ node }}"},
			[]any{"SET", "severity", "{{ level }}"},
			[]any{"SET", "message", "{{ alertname }}"},
			[]any{"DELETE", "node"},
			[]any{"DELETE", "level"},
			[]any{"DELETE", "alertname"},
		},
	}}, db.WriteOptions{})
	require.NoError(t, err)

	// Build the real rule Processor via its registered factory and load the tree.
	host := &csmHost{
		driver: drv,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(prometheus.NewRegistry()),
		tracer: otel.Tracer("custom-source-test"),
	}
	rulePlugin := rule.Plugin{}
	require.NoError(t, rulePlugin.PostInit(ctx, host))

	// Wire a minimal Core with the real driver + the real rule plugin.
	c := &Core{
		Driver:  drv,
		Reg:     telemetry.NewRegistry(prometheus.NewRegistry()),
		Trc:     otel.Tracer("custom-source-test"),
		Loggers: &telemetry.Loggers{Snooze: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
	c.processOrder = []plugins.Processor{&rulePlugin}

	// Post a foreign-shaped alert with the preserve-raw hint.
	in := map[string]any{
		"source":        "my-tool",
		"alertname":     "DiskFull",
		"node":          "web-1",
		"level":         "critical",
		"_preserve_raw": true,
	}

	out, action, err := c.ProcessRecordMap(ctx, in)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, action)

	// Canonical fields populated by the rule.
	require.Equal(t, "web-1", out["host"])
	require.Equal(t, "critical", out["severity"])
	require.Equal(t, "DiskFull", out["message"])

	// Foreign keys are gone from the top-level record (DELETE'd by the rule).
	require.NotContains(t, out, "node")
	require.NotContains(t, out, "level")
	require.NotContains(t, out, "alertname")

	// The original payload was archived in raw BEFORE the rule deletes ran,
	// because _preserve_raw is consumed in mapToRecord (pre-pipeline).
	raw, ok := out["raw"].(map[string]any)
	require.True(t, ok, "raw must be a populated map[string]any, got %T", out["raw"])
	require.Equal(t, "web-1", raw["node"])
	require.Equal(t, "critical", raw["level"])
	require.Equal(t, "DiskFull", raw["alertname"])

	// The sentinel never surfaces anywhere in the output.
	require.NotContains(t, out, "_preserve_raw")
	require.NotContains(t, raw, "_preserve_raw")
}
