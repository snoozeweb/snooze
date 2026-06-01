package core

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
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
	ctx := context.Background()

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
	plugins.RecordStat(c, 1780302245, "alert_hit", map[string]string{"source": "syslog"}, 1)

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
