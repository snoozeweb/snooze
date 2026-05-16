package snooze

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// stubHost is a Host that only wires the bits the snooze plugin reads: the
// driver, a logger, the metrics registry, the OTEL tracer and the immutable
// config. Bus is unused; sibling-plugin lookup is unused.
type stubHost struct {
	driver db.Driver
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
	tracer trace.Tracer
}

func newStubHost(t *testing.T) *stubHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	d, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return &stubHost{
		driver: d,
		logger: slog.Default(),
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(nil),
		tracer: otel.Tracer("snooze-plugin-test"),
	}
}

func (h *stubHost) DB() db.Driver                { return h.driver }
func (h *stubHost) Bus() plugins.Bus             { return nil }
func (h *stubHost) Logger() *slog.Logger         { return h.logger }
func (h *stubHost) Tracer() trace.Tracer         { return h.tracer }
func (h *stubHost) Metrics() *telemetry.Registry { return h.metr }
func (h *stubHost) Config() *config.Config       { return h.cfg }
func (h *stubHost) Plugin(string) plugins.Plugin { return nil }

// writeRule inserts a snooze record built from a free-form Document. Returns
// the assigned uid so tests can poke at the row afterwards.
func writeRule(t *testing.T, h *stubHost, doc db.Document) string {
	t.Helper()
	res, err := h.driver.Write(context.Background(), "snooze",
		[]db.Document{doc}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: true})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	return res.Added[0]
}

// newPlugin builds a Plugin wired to h, with an injectable clock.
func newPlugin(t *testing.T, h *stubHost, now func() time.Time) *Plugin {
	t.Helper()
	p := &Plugin{Now: now}
	require.NoError(t, p.PostInit(context.Background(), h))
	return p
}

// TestSnoozeMatch_AbortWrite matches the Python `test_snooze_1`: condition
// matches, no `discard`, so the plugin returns ActionAbortWrite and tags the
// record with the rule name.
func TestSnoozeMatch_AbortWrite(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
	})
	p := newPlugin(t, h, nil)

	rec := snoozetypes.Record{Extra: map[string]any{"a": "1", "b": "2"}}
	res, err := p.Process(context.Background(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, res.Action)
	require.Equal(t, "Filter 1", res.Record.Extra["snoozed"])
}

// TestSnoozeMiss_Continue matches the Python `test_snooze_2`: no rule
// matches, the plugin votes Continue and leaves the record alone.
func TestSnoozeMiss_Continue(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
	})
	writeRule(t, h, db.Document{
		"name":      "Filter 2",
		"condition": []any{"=", "a", "3"},
		"discard":   true,
	})
	p := newPlugin(t, h, nil)

	rec := snoozetypes.Record{Extra: map[string]any{"a": "2", "b": "2"}}
	res, err := p.Process(context.Background(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
	require.Nil(t, res.Record.Extra["snoozed"])
}

// TestSnoozeDiscard_Abort matches the Python `test_snooze_3`: a discard rule
// matches, the plugin returns ActionAbort (drop without persisting).
func TestSnoozeDiscard_Abort(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 2",
		"condition": []any{"=", "a", "3"},
		"discard":   true,
	})
	p := newPlugin(t, h, nil)

	rec := snoozetypes.Record{Extra: map[string]any{"a": "3", "b": "2"}}
	res, err := p.Process(context.Background(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, res.Action)
	require.Equal(t, "Filter 2", res.Record.Extra["snoozed"])
}

// TestSnoozeDisabled covers a defaulted-on rule explicitly disabled: it must
// not fire even when the condition would otherwise match.
func TestSnoozeDisabled(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
		"enabled":   false,
	})
	p := newPlugin(t, h, nil)

	rec := snoozetypes.Record{Extra: map[string]any{"a": "1"}}
	res, err := p.Process(context.Background(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
}

// TestSnoozeTimeConstraints covers the time-constraint gate. Wednesday
// 12:00 should match a Mon-Thu window; Saturday should not.
func TestSnoozeTimeConstraints(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Snooze rule 1",
		"condition": []any{"=", "host", "myhost01"},
		"time_constraints": map[string]any{
			"weekdays": []any{
				map[string]any{"weekdays": []any{1, 2, 3, 4}},
			},
			"time": []any{
				map[string]any{"from": "10:00", "until": "14:00"},
			},
		},
	})

	rec := snoozetypes.Record{Host: "myhost01"}

	// 2021-07-07 was a Wednesday (weekday 3).
	wed := time.Date(2021, 7, 7, 12, 0, 0, 0, time.UTC)
	p := newPlugin(t, h, func() time.Time { return wed })
	res, err := p.Process(context.Background(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, res.Action)
	require.Equal(t, "Snooze rule 1", res.Record.Extra["snoozed"])

	// 2021-07-10 was a Saturday (weekday 6) - outside the window.
	sat := time.Date(2021, 7, 10, 12, 0, 0, 0, time.UTC)
	p2 := newPlugin(t, h, func() time.Time { return sat })
	res2, err := p2.Process(context.Background(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res2.Action)
}

// TestSnoozeReload exercises the cache-refresh path: a rule added after
// PostInit becomes effective after a Reload call.
func TestSnoozeReload(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	p := newPlugin(t, h, nil)
	require.Empty(t, p.cachedRules())

	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
	})

	// Stale cache: no match yet.
	rec := snoozetypes.Record{Extra: map[string]any{"a": "1"}}
	res, err := p.Process(context.Background(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	// After Reload the new rule is picked up.
	require.NoError(t, p.Reload(context.Background()))
	require.Len(t, p.cachedRules(), 1)
	res, err = p.Process(context.Background(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, res.Action)
}

// TestSnoozeHitsCounter covers the synchronous hit-counter bump performed on
// each match. The Python version uses an AsyncIncrement; we trade off
// throughput for simplicity (see plugin.go's package doc).
func TestSnoozeHitsCounter(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	uid := writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
	})
	p := newPlugin(t, h, nil)

	rec := snoozetypes.Record{Extra: map[string]any{"a": "1"}}
	for i := 0; i < 3; i++ {
		_, err := p.Process(context.Background(), rec)
		require.NoError(t, err)
	}

	got, err := h.driver.GetOne(context.Background(), "snooze", db.Document{"uid": uid})
	require.NoError(t, err)
	hits, ok := toInt64(got["hits"])
	require.True(t, ok, "hits field missing or non-numeric: %#v", got["hits"])
	require.EqualValues(t, 3, hits)
}
