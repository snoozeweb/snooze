package notification

import (
	"context"
	"errors"
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
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
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

func (n *recordingNotifier) Name() string                                 { return n.name }
func (n *recordingNotifier) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: n.name} }
func (n *recordingNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (n *recordingNotifier) Reload(context.Context) error                 { return nil }

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

// injectingNotifier is a fake Notifier that calls payload.Inject exactly once
// per Send, simulating webhook's `inject_response`. Tests use it to assert the
// dispatcher's inject closure writes the field back onto the originating record.
type injectingNotifier struct {
	name  string
	field string
	value any
}

func (n *injectingNotifier) Name() string                                 { return n.name }
func (n *injectingNotifier) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: n.name} }
func (n *injectingNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (n *injectingNotifier) Reload(context.Context) error                 { return nil }

func (n *injectingNotifier) Send(_ context.Context, _ snoozetypes.Record, payload plugins.NotificationPayload) error {
	plugins.InjectField(payload.Inject, n.field, n.value)
	return nil
}

// failingNotifier is a fake Notifier whose Send always returns a non-nil error.
// It counts calls via the same atomic counter used by recordingNotifier so that
// waitForCalls cannot be used on it (it has no Calls slice), but callers can
// wait on Total() directly.
type failingNotifier struct {
	name  string
	total atomic.Int64
}

func (n *failingNotifier) Name() string                                 { return n.name }
func (n *failingNotifier) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: n.name} }
func (n *failingNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (n *failingNotifier) Reload(context.Context) error                 { return nil }

func (n *failingNotifier) Send(_ context.Context, _ snoozetypes.Record, _ plugins.NotificationPayload) error {
	n.total.Add(1)
	return errors.New("send: simulated failure")
}

// incCaptureDriver wraps the SQLite driver and overrides BulkIncrement to
// capture increment operations so tests can inspect what RecordStat emits.
type incCaptureDriver struct {
	db.Driver
	mu    sync.Mutex
	calls []capturedInc
}

type capturedInc struct {
	collection, field string
	search            db.Document
	delta             int64
}

func (d *incCaptureDriver) BulkIncrement(ctx context.Context, collection string, ops []db.IncrementOp, upsert bool) error {
	d.mu.Lock()
	for _, op := range ops {
		for field, delta := range op.Deltas {
			d.calls = append(d.calls, capturedInc{
				collection: collection,
				field:      field,
				search:     op.Search,
				delta:      delta,
			})
		}
	}
	d.mu.Unlock()
	// Also write through so the test SQLite driver stays consistent.
	return d.Driver.BulkIncrement(ctx, collection, ops, upsert)
}

func (d *incCaptureDriver) Captured() []capturedInc {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]capturedInc, len(d.calls))
	copy(out, d.calls)
	return out
}

// metricsTestHost extends testHost with AsyncWriter support so that
// plugins.RecordStat routes writes through the capturing driver.
type metricsTestHost struct {
	*testHost
	writer *asyncwriter.Writer
}

func (h *metricsTestHost) AsyncWriter() *asyncwriter.Writer { return h.writer }

// newMetricsHost wires a fresh SQLite driver wrapped in incCaptureDriver,
// then builds a metricsTestHost whose asyncwriter.Writer flushes into it.
func newMetricsHost(t *testing.T) (*metricsTestHost, *incCaptureDriver) {
	t.Helper()
	inner := newHost(t)
	capDrv := &incCaptureDriver{Driver: inner.driver}
	inner.driver = capDrv
	w := asyncwriter.New(capDrv, time.Hour, asyncwriter.NewMockClock(time.Unix(0, 0)),
		asyncwriter.WithUpsert(true))
	return &metricsTestHost{testHost: inner, writer: w}, capDrv
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

	t.Run("declares_action_collection_as_reload_dependency", func(t *testing.T) {
		// The dispatcher caches the `action` collection in memory; it owns the
		// `notification` collection. Without declaring `action` as a reload
		// dependency, an action edit (URL/payload/…) silently never reaches the
		// running dispatcher until a restart. The syncer reads this list.
		p := &Plugin{meta: plugins.Metadata{Name: "notification"}}
		require.Contains(t, p.ReloadCollections(), actionCollectionName)
	})

	t.Run("injects_response_by_hash_on_first_fire_without_uid", func(t *testing.T) {
		host := newHost(t)
		// Simulate the pipeline's final write: the record row exists, keyed by
		// hash, with a DB-minted uid. On a genuine first fire the in-memory
		// record handed to Process has NO uid yet (aggregaterule mints it only
		// when an existing aggregate is found), so the inject must key on hash.
		_, err := host.driver.Write(context.Background(), recordCollectionName,
			[]db.Document{{"hash": "h-first", "host": "myhost01", "message": "boom"}},
			db.WriteOptions{Primary: []string{"hash"}, UpdateTime: true})
		require.NoError(t, err)

		writeActions(t, host, []map[string]any{
			{"name": "Teams", "action": map[string]any{"selected": "webhook", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host, []map[string]any{
			{"name": "N1", "condition": []any{"=", "host", "myhost01"}, "actions": []any{"Teams"}},
		})
		injectVal := map[string]any{"message_ids": map[string]any{"ch": "42"}}
		host.plugins["webhook"] = &injectingNotifier{name: "webhook", field: "response_Teams", value: injectVal}

		p := newPlugin(t, host)

		// First-fire shape: hash present, uid empty.
		rec := snoozetypes.Record{Hash: "h-first", Host: "myhost01", Message: "boom", Timestamp: time.Now()}
		_, err = p.Process(context.Background(), rec)
		require.NoError(t, err)

		// The inject runs on a detached goroutine; poll the row by hash.
		deadline := time.Now().Add(time.Second)
		var got db.Document
		for time.Now().Before(deadline) {
			got, err = host.driver.GetOne(context.Background(), recordCollectionName, db.Document{"hash": "h-first"})
			require.NoError(t, err)
			if got["response_Teams"] != nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		require.NotNil(t, got["response_Teams"], "response_Teams must be injected on the first fire (keyed by hash)")
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

// TestNotificationStats verifies that Process increments notification_sent once
// per matched notification entry, and action_success / action_error once per
// action (keyed by action name) after each Notifier.Send returns.
func TestNotificationStats(t *testing.T) {
	host, capDrv := newMetricsHost(t)

	// Two actions: one whose notifier succeeds, one whose notifier fails.
	writeActions(t, host.testHost, []map[string]any{
		{
			"name": "GoodAction",
			"action": map[string]any{
				"selected":   "good-notifier",
				"subcontent": map[string]any{},
			},
		},
		{
			"name": "BadAction",
			"action": map[string]any{
				"selected":   "bad-notifier",
				"subcontent": map[string]any{},
			},
		},
	})
	writeEntries(t, host.testHost, []map[string]any{
		{
			"name":      "MyNotification",
			"condition": []any{},
			"actions":   []any{"GoodAction", "BadAction"},
		},
	})

	good := &recordingNotifier{name: "good-notifier"}
	bad := &failingNotifier{name: "bad-notifier"}
	host.plugins["good-notifier"] = good
	host.plugins["bad-notifier"] = bad

	p := &Plugin{meta: plugins.Metadata{Name: "notification"}}
	require.NoError(t, p.PostInit(context.Background(), host))

	// eventEpoch 1780302245 → UTC hour bucket 1780300800
	const eventEpoch = int64(1780302245)
	rec := snoozetypes.Record{
		UID:       "uid-stats-test",
		Host:      "myhost01",
		Message:   "stats test",
		Timestamp: time.Now(),
		DateEpoch: eventEpoch,
	}

	_, err := p.Process(context.Background(), rec)
	require.NoError(t, err)

	// Wait for the good send to complete, then also wait for the bad send.
	waitForCalls(t, good, 1, time.Second)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if bad.total.Load() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.GreaterOrEqual(t, bad.total.Load(), int64(1), "bad notifier should have been called")

	// Flush the async writer so the incCaptureDriver sees all BulkIncrement ops.
	require.NoError(t, host.writer.Flush(context.Background()))

	ops := capDrv.Captured()

	// Helper: find ops matching a given metric+name combination.
	find := func(metric, nameKey string) []capturedInc {
		var found []capturedInc
		for _, op := range ops {
			if op.search["metric"] == metric &&
				op.search["dim"] == "name" &&
				op.search["key"] == nameKey {
				found = append(found, op)
			}
		}
		return found
	}

	const wantBucket = int64(1780300800)

	// Exactly one notification_sent for "MyNotification".
	sentOps := find("notification_sent", "MyNotification")
	require.Len(t, sentOps, 1, "expected exactly 1 notification_sent op for MyNotification")
	require.Equal(t, "stats", sentOps[0].collection)
	require.Equal(t, "value", sentOps[0].field)
	require.Equal(t, int64(1), sentOps[0].delta)
	require.Equal(t, wantBucket, sentOps[0].search["bucket"])

	// Exactly one action_success for "GoodAction".
	successOps := find("action_success", "GoodAction")
	require.Len(t, successOps, 1, "expected exactly 1 action_success op for GoodAction")
	require.Equal(t, int64(1), successOps[0].delta)
	require.Equal(t, wantBucket, successOps[0].search["bucket"])

	// Exactly one action_error for "BadAction".
	errorOps := find("action_error", "BadAction")
	require.Len(t, errorOps, 1, "expected exactly 1 action_error op for BadAction")
	require.Equal(t, int64(1), errorOps[0].delta)
	require.Equal(t, wantBucket, errorOps[0].search["bucket"])
}
