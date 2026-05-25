package notification

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	dbsqlite "github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// recordingNotifier captures every Send call the dispatcher makes. It is the
// fake outbound endpoint the tests assert against.
type recordingNotifier struct {
	name string

	mu    sync.Mutex
	calls []notifierCall
	total atomic.Int64
}

type notifierCall struct {
	Record  snoozetypes.Record
	Payload plugins.NotificationPayload
}

func (n *recordingNotifier) Name() string                              { return n.name }
func (n *recordingNotifier) Metadata() plugins.Metadata                { return plugins.Metadata{Name: n.name} }
func (n *recordingNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (n *recordingNotifier) Reload(context.Context) error              { return nil }

func (n *recordingNotifier) Send(_ context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	n.mu.Lock()
	n.calls = append(n.calls, notifierCall{Record: rec, Payload: payload})
	n.mu.Unlock()
	n.total.Add(1)
	return nil
}

func (n *recordingNotifier) Calls() []notifierCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]notifierCall, len(n.calls))
	copy(out, n.calls)
	return out
}

// testHost is a minimal plugins.Host suitable for the notification plugin.
type testHost struct {
	driver  db.Driver
	logger  *slog.Logger
	cfg     *config.Config
	metr    *telemetry.Registry
	tracer  trace.Tracer
	plugins map[string]plugins.Plugin
}

func (h *testHost) DB() db.Driver                { return h.driver }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return h.logger }
func (h *testHost) Tracer() trace.Tracer         { return h.tracer }
func (h *testHost) Metrics() *telemetry.Registry { return h.metr }
func (h *testHost) Config() *config.Config       { return h.cfg }
func (h *testHost) Plugin(name string) plugins.Plugin {
	if h.plugins == nil {
		return nil
	}
	return h.plugins[name]
}

// newHost wires a fresh SQLite driver and an empty plugin registry.
func newHost(t *testing.T) *testHost {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := dbsqlite.New(context.Background(), dbsqlite.Config{Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	return &testHost{
		driver:  drv,
		logger:  slog.Default(),
		cfg:     config.Default(),
		metr:    telemetry.NewRegistry(nil),
		tracer:  otel.Tracer("notification-test"),
		plugins: map[string]plugins.Plugin{},
	}
}

// registerNotifier installs n into the host's plugin registry under its name.
func (h *testHost) registerNotifier(n *recordingNotifier) {
	h.plugins[n.name] = n
}

// writeEntries seeds the notification collection with raw entry documents.
func writeEntries(t *testing.T, h *testHost, entries []map[string]any) {
	t.Helper()
	docs := make([]db.Document, 0, len(entries))
	for _, e := range entries {
		docs = append(docs, db.Document(e))
	}
	_, err := h.driver.Write(context.Background(), collectionName, docs, db.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
}

// writeActions seeds the action collection.
func writeActions(t *testing.T, h *testHost, actions []map[string]any) {
	t.Helper()
	docs := make([]db.Document, 0, len(actions))
	for _, a := range actions {
		docs = append(docs, db.Document(a))
	}
	_, err := h.driver.Write(context.Background(), actionCollectionName, docs, db.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
}

func newPlugin(t *testing.T, h *testHost) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "notification"}}
	require.NoError(t, p.PostInit(context.Background(), h))
	return p
}

// waitForCalls polls until the recorder has at least want calls or fails the
// test on timeout. Sends are dispatched on a detached goroutine so the test
// cannot assume synchronous completion of Process.
func waitForCalls(t *testing.T, n *recordingNotifier, want int, timeout time.Duration) []notifierCall {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		calls := n.Calls()
		if len(calls) >= want {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("notifier %q recorded %d calls, want %d within %s", n.name, len(n.Calls()), want, timeout)
	return nil
}

func TestNotification(t *testing.T) {
	t.Run("dispatches_to_notifier_for_matching_action", func(t *testing.T) {
		host := newHost(t)
		writeActions(t, host, []map[string]any{
			{
				"name": "Script",
				"action": map[string]any{
					"selected":   "script",
					"subcontent": map[string]any{"path": "/usr/bin/true"},
				},
			},
		})
		writeEntries(t, host, []map[string]any{
			{
				"name":      "Notification1",
				"condition": []any{"=", "host", "myhost01"},
				"actions":   []any{"Script"},
			},
		})
		notifier := &recordingNotifier{name: "script"}
		host.registerNotifier(notifier)

		p := newPlugin(t, host)

		rec := snoozetypes.Record{
			UID:       "uid-1",
			Host:      "myhost01",
			Message:   "hello",
			Timestamp: time.Now(),
		}
		res, err := p.Process(context.Background(), rec)
		require.NoError(t, err)
		require.Equal(t, plugins.ActionContinue, res.Action)
		require.Equal(t, rec, res.Record)

		calls := waitForCalls(t, notifier, 1, time.Second)
		require.Len(t, calls, 1)
		require.Equal(t, "myhost01", calls[0].Record.Host)
		require.Equal(t, "Script", calls[0].Payload.Meta["action_name"])
		require.Equal(t, "Notification1", calls[0].Payload.Meta["notification_name"])
		require.Equal(t, "/usr/bin/true", calls[0].Payload.Meta["path"])
		require.Equal(t, "script", calls[0].Payload.Template)
	})

	t.Run("respects_time_constraint", func(t *testing.T) {
		host := newHost(t)
		writeActions(t, host, []map[string]any{
			{"name": "Script", "action": map[string]any{"selected": "script", "subcontent": map[string]any{}}},
		})

		// Wednesday 2021-07-07 11:00 UTC — inside the window.
		inside := time.Date(2021, 7, 7, 11, 0, 0, 0, time.UTC)
		// Saturday 2021-07-10 11:00 UTC — weekday match fails.
		outside := time.Date(2021, 7, 10, 11, 0, 0, 0, time.UTC)

		writeEntries(t, host, []map[string]any{
			{
				"name":      "N1",
				"condition": []any{"=", "host", "myhost01"},
				"time_constraints": map[string]any{
					"weekdays": []any{
						map[string]any{"weekdays": []any{1, 2, 3, 4}},
					},
					"time": []any{
						map[string]any{"from": "10:00", "until": "14:00"},
					},
				},
				"actions": []any{"Script"},
			},
		})
		notifier := &recordingNotifier{name: "script"}
		host.registerNotifier(notifier)

		p := newPlugin(t, host)

		_, err := p.Process(context.Background(), snoozetypes.Record{UID: "in", Host: "myhost01", Timestamp: inside})
		require.NoError(t, err)
		_, err = p.Process(context.Background(), snoozetypes.Record{UID: "out", Host: "myhost01", Timestamp: outside})
		require.NoError(t, err)

		calls := waitForCalls(t, notifier, 1, time.Second)
		// Sleep a little longer to confirm the outside-window record didn't
		// sneak in late.
		time.Sleep(50 * time.Millisecond)
		require.Len(t, notifier.Calls(), 1, "only the in-window record should dispatch")
		require.Equal(t, "in", calls[0].Record.UID)
	})

	t.Run("ack_close_records_skip_dispatch", func(t *testing.T) {
		host := newHost(t)
		writeActions(t, host, []map[string]any{
			{"name": "Always", "action": map[string]any{"selected": "script", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host, []map[string]any{
			{"name": "AlwaysFires", "condition": []any{}, "actions": []any{"Always"}},
		})
		notifier := &recordingNotifier{name: "script"}
		host.registerNotifier(notifier)

		p := newPlugin(t, host)

		for _, state := range []string{"ack", "close"} {
			_, err := p.Process(context.Background(), snoozetypes.Record{UID: "uid-" + state, State: state, Timestamp: time.Now()})
			require.NoError(t, err)
		}
		// Allow time for any erroneous dispatch to fire.
		time.Sleep(50 * time.Millisecond)
		require.Empty(t, notifier.Calls(), "no dispatch expected for ack/close records")
	})

	t.Run("missing_action_logs_and_skips", func(t *testing.T) {
		host := newHost(t)
		// No action documents — the lookup miss should be tolerated.
		writeEntries(t, host, []map[string]any{
			{"name": "N1", "condition": []any{}, "actions": []any{"DoesNotExist"}},
		})
		notifier := &recordingNotifier{name: "script"}
		host.registerNotifier(notifier)

		p := newPlugin(t, host)
		_, err := p.Process(context.Background(), snoozetypes.Record{UID: "x", Host: "h", Timestamp: time.Now()})
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
		require.Empty(t, notifier.Calls())
	})

	t.Run("missing_notifier_plugin_logs_and_skips", func(t *testing.T) {
		host := newHost(t)
		writeActions(t, host, []map[string]any{
			{"name": "X", "action": map[string]any{"selected": "nonexistent", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host, []map[string]any{
			{"name": "N1", "condition": []any{}, "actions": []any{"X"}},
		})
		// Do not register a "nonexistent" notifier.
		notifier := &recordingNotifier{name: "script"}
		host.registerNotifier(notifier)

		p := newPlugin(t, host)
		_, err := p.Process(context.Background(), snoozetypes.Record{UID: "x", Host: "h", Timestamp: time.Now()})
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
		require.Empty(t, notifier.Calls())
	})

	t.Run("frequency_total_zero_skips", func(t *testing.T) {
		host := newHost(t)
		writeActions(t, host, []map[string]any{
			{"name": "Script", "action": map[string]any{"selected": "script", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host, []map[string]any{
			{
				"name":      "N1",
				"condition": []any{},
				"actions":   []any{"Script"},
				"frequency": map[string]any{"total": 0},
			},
		})
		notifier := &recordingNotifier{name: "script"}
		host.registerNotifier(notifier)

		p := newPlugin(t, host)
		_, err := p.Process(context.Background(), snoozetypes.Record{UID: "x", Host: "h", Timestamp: time.Now()})
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
		require.Empty(t, notifier.Calls(), "frequency.total == 0 must suppress the send")
	})
}
