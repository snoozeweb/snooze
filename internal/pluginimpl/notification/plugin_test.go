package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	dbsqlite "github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// tctx returns a context scoped to the reserved default tenant. The driver
// layer fail-closes on tenant-scoped collections (notification, action, record,
// stats) when neither a tenant nor platform scope is present, so every test
// DB/plugin call that exercises a scoped collection must carry one. This mirrors
// real single-tenant behaviour.
func tctx() context.Context {
	return auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
}

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

// loopChainNotifier captures the X-Snooze-Loop chain observed on the context
// handed to Send. Used to assert that spawnCoordinator re-attaches the
// pipeline's loop chain onto the detached per-send goroutine's context, so a
// federation notifier's loop prevention keeps seeing the chain it needs.
type loopChainNotifier struct {
	name  string
	chain chan []string
}

func (n *loopChainNotifier) Name() string                                 { return n.name }
func (n *loopChainNotifier) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: n.name} }
func (n *loopChainNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (n *loopChainNotifier) Reload(context.Context) error                 { return nil }

func (n *loopChainNotifier) Send(ctx context.Context, _ snoozetypes.Record, _ plugins.NotificationPayload) error {
	n.chain <- auth.LoopChainFrom(ctx)
	return nil
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
	_, err := h.driver.Write(tctx(), collectionName, docs, db.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
}

// writeActions seeds the action collection.
func writeActions(t *testing.T, h *testHost, actions []map[string]any) {
	t.Helper()
	docs := make([]db.Document, 0, len(actions))
	for _, a := range actions {
		docs = append(docs, db.Document(a))
	}
	_, err := h.driver.Write(tctx(), actionCollectionName, docs, db.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
}

func newPlugin(t *testing.T, h *testHost) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "notification"}}
	require.NoError(t, p.PostInit(tctx(), h))
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
		res, err := p.Process(tctx(), rec)
		require.NoError(t, err)
		require.Equal(t, plugins.ActionContinue, res.Action)
		// The notification stamp adds Extra["notifications"] but must leave every
		// other record field intact.
		require.Equal(t, rec.UID, res.Record.UID)
		require.Equal(t, rec.Host, res.Record.Host)
		require.Equal(t, rec.Message, res.Record.Message)
		require.Equal(t, rec.Timestamp, res.Record.Timestamp)
		require.Equal(t, []string{"Notification1"}, res.Record.Extra["notifications"])

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

		_, err := p.Process(tctx(), snoozetypes.Record{UID: "in", Host: "myhost01", Timestamp: inside})
		require.NoError(t, err)
		_, err = p.Process(tctx(), snoozetypes.Record{UID: "out", Host: "myhost01", Timestamp: outside})
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
			_, err := p.Process(tctx(), snoozetypes.Record{UID: "uid-" + state, State: state, Timestamp: time.Now()})
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
		_, err := p.Process(tctx(), snoozetypes.Record{UID: "x", Host: "h", Timestamp: time.Now()})
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
		_, err := p.Process(tctx(), snoozetypes.Record{UID: "x", Host: "h", Timestamp: time.Now()})
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
		_, err := host.driver.Write(tctx(), recordCollectionName,
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
		_, err = p.Process(tctx(), rec)
		require.NoError(t, err)

		// The inject runs on a detached goroutine; poll the row by hash.
		deadline := time.Now().Add(time.Second)
		var got db.Document
		for time.Now().Before(deadline) {
			got, err = host.driver.GetOne(tctx(), recordCollectionName, db.Document{"hash": "h-first"})
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
		_, err := p.Process(tctx(), snoozetypes.Record{UID: "x", Host: "h", Timestamp: time.Now()})
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
	require.NoError(t, p.PostInit(tctx(), host))

	// eventEpoch 1780302245 → UTC hour bucket 1780300800
	const eventEpoch = int64(1780302245)
	rec := snoozetypes.Record{
		UID:       "uid-stats-test",
		Host:      "myhost01",
		Message:   "stats test",
		Timestamp: time.Now(),
		DateEpoch: eventEpoch,
	}

	_, err := p.Process(tctx(), rec)
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

// TestNotificationStats_TenantPartition verifies that the delivery-outcome
// counters (action_success / action_error) emitted from fireSend's detached
// goroutine land in the DISPATCH tenant's partition, not the platform/global
// bucket. Before the fix fireSend used a naked context.Background(), so
// cloneDocWithTenant baked no tenant_id and the counters leaked into the
// platform partition (tenant_id="") instead of the originating tenant's.
func TestNotificationStats_TenantPartition(t *testing.T) {
	host, capDrv := newMetricsHost(t)

	const tenant = "acme"
	tenantCtx := auth.WithTenant(context.Background(), tenant)

	writeActionsCtx := func(ctx context.Context, actions []map[string]any) {
		docs := make([]db.Document, 0, len(actions))
		for _, a := range actions {
			docs = append(docs, db.Document(a))
		}
		_, err := host.driver.Write(ctx, actionCollectionName, docs, db.WriteOptions{UpdateTime: true})
		require.NoError(t, err)
	}
	writeEntriesCtx := func(ctx context.Context, entries []map[string]any) {
		docs := make([]db.Document, 0, len(entries))
		for _, e := range entries {
			docs = append(docs, db.Document(e))
		}
		_, err := host.driver.Write(ctx, collectionName, docs, db.WriteOptions{UpdateTime: true})
		require.NoError(t, err)
	}

	writeActionsCtx(tenantCtx, []map[string]any{
		{"name": "GoodAction", "action": map[string]any{"selected": "good-notifier", "subcontent": map[string]any{}}},
		{"name": "BadAction", "action": map[string]any{"selected": "bad-notifier", "subcontent": map[string]any{}}},
	})
	writeEntriesCtx(tenantCtx, []map[string]any{
		{"name": "MyNotification", "condition": []any{}, "actions": []any{"GoodAction", "BadAction"}},
	})

	good := &recordingNotifier{name: "good-notifier"}
	bad := &failingNotifier{name: "bad-notifier"}
	host.plugins["good-notifier"] = good
	host.plugins["bad-notifier"] = bad

	p := &Plugin{meta: plugins.Metadata{Name: "notification"}}
	require.NoError(t, p.PostInit(tenantCtx, host))

	const eventEpoch = int64(1780302245)
	rec := snoozetypes.Record{
		UID:       "uid-tenant-stats",
		Host:      "myhost01",
		Message:   "stats test",
		Timestamp: time.Now(),
		DateEpoch: eventEpoch,
	}

	_, err := p.Process(tenantCtx, rec)
	require.NoError(t, err)

	waitForCalls(t, good, 1, time.Second)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if bad.total.Load() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.GreaterOrEqual(t, bad.total.Load(), int64(1), "bad notifier should have been called")

	require.NoError(t, host.writer.Flush(context.Background()))

	ops := capDrv.Captured()
	find := func(metric, nameKey string) []capturedInc {
		var found []capturedInc
		for _, op := range ops {
			if op.search["metric"] == metric && op.search["dim"] == "name" && op.search["key"] == nameKey {
				found = append(found, op)
			}
		}
		return found
	}

	successOps := find("action_success", "GoodAction")
	require.Len(t, successOps, 1, "expected exactly 1 action_success op for GoodAction")
	require.Equal(t, tenant, successOps[0].search["tenant_id"],
		"action_success counter must land in the dispatch tenant's partition, not the platform bucket")

	errorOps := find("action_error", "BadAction")
	require.Len(t, errorOps, 1, "expected exactly 1 action_error op for BadAction")
	require.Equal(t, tenant, errorOps[0].search["tenant_id"],
		"action_error counter must land in the dispatch tenant's partition, not the platform bucket")
}

// TestProcessPopulatesEscalationContext locks in the dispatcher half of the
// escalation contract: a notifier learns "this is re-escalation #2, because the
// ack timed out" from the payload alone, with no DB access of its own. The
// first-fire case must stay a zero Escalation so every notifier keeps its
// pre-escalation behaviour on records that never escalated.
func TestProcessPopulatesEscalationContext(t *testing.T) {
	t.Run("first_fire_is_zero", func(t *testing.T) {
		host := newHost(t)
		writeActions(t, host, []map[string]any{
			{"name": "Script", "action": map[string]any{"selected": "script", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host, []map[string]any{
			{"name": "N1", "condition": []any{"=", "host", "myhost01"}, "actions": []any{"Script"}},
		})
		notifier := &recordingNotifier{name: "script"}
		host.registerNotifier(notifier)
		p := newPlugin(t, host)

		_, err := p.Process(tctx(), snoozetypes.Record{UID: "u1", Host: "myhost01", Timestamp: time.Now()})
		require.NoError(t, err)

		calls := waitForCalls(t, notifier, 1, time.Second)
		require.Zero(t, calls[0].Payload.Escalation.Count)
		require.False(t, calls[0].Payload.Escalation.IsRe())
	})

	t.Run("re_escalation_carries_count_reason_and_severity_rise", func(t *testing.T) {
		host := newHost(t)
		writeActions(t, host, []map[string]any{
			{"name": "Script", "action": map[string]any{"selected": "script", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host, []map[string]any{
			{"name": "N1", "condition": []any{"=", "host", "myhost01"}, "actions": []any{"Script"}},
		})
		notifier := &recordingNotifier{name: "script"}
		host.registerNotifier(notifier)
		p := newPlugin(t, host)

		_, err := p.Process(tctx(), snoozetypes.Record{
			UID:              "u1",
			Host:             "myhost01",
			State:            "esc",
			Timestamp:        time.Now(),
			EscalationCount:  2,
			EscalationReason: "timeout",
			Severity:         "critical",
			Extra: map[string]any{
				// The REAL label aggregaterule stamps. SeverityRose compares the
				// severities, not this, precisely so an invented spelling can
				// never make a dead check look alive.
				"trend_indication":  "moreSevere",
				"previous_severity": "warning",
				"duplicates":        int64(5),
			},
		})
		require.NoError(t, err)

		calls := waitForCalls(t, notifier, 1, time.Second)
		esc := calls[0].Payload.Escalation
		require.True(t, esc.IsRe())
		require.Equal(t, 2, esc.Count)
		require.Equal(t, "#2", esc.Ordinal())
		require.Equal(t, "timeout", esc.Reason)
		require.True(t, esc.SeverityRose(), "warning -> critical must read as a rise")
		require.Equal(t, int64(5), esc.Duplicates)
	})
}

// A notification condition must be able to target re-escalations only, which
// requires escalation_count to reach the condition evaluator.
func TestProcessConditionCanMatchEscalationCount(t *testing.T) {
	host := newHost(t)
	writeActions(t, host, []map[string]any{
		{"name": "Script", "action": map[string]any{"selected": "script", "subcontent": map[string]any{}}},
	})
	writeEntries(t, host, []map[string]any{
		{"name": "OnlyEsc", "condition": []any{">", "escalation_count", 0}, "actions": []any{"Script"}},
	})
	notifier := &recordingNotifier{name: "script"}
	host.registerNotifier(notifier)
	p := newPlugin(t, host)

	// First fire: no escalation_count, so the condition must not match.
	_, err := p.Process(tctx(), snoozetypes.Record{UID: "u1", Host: "h", Timestamp: time.Now()})
	require.NoError(t, err)

	// Re-escalation: matches.
	_, err = p.Process(tctx(), snoozetypes.Record{
		UID: "u2", Host: "h", Timestamp: time.Now(), EscalationCount: 1,
	})
	require.NoError(t, err)

	calls := waitForCalls(t, notifier, 1, time.Second)
	require.Len(t, calls, 1, "only the re-escalation may match `escalation_count > 0`")
	require.Equal(t, "u2", calls[0].Record.UID)
}

func TestProcessStampsNotifications(t *testing.T) {
	h := newHost(t)
	writeEntries(t, h, []map[string]any{
		{"name": "n-alpha", "condition": []any{"=", "host", "web01"}, "actions": []any{}},
		{"name": "n-beta", "condition": []any{"=", "host", "web01"}, "actions": []any{}},
		{"name": "n-nomatch", "condition": []any{"=", "host", "db01"}, "actions": []any{}},
	})
	p := newPlugin(t, h)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "web01", Hash: "h1"})
	require.NoError(t, err)

	got, _ := res.Record.Extra["notifications"].([]string)
	require.ElementsMatch(t, []string{"n-alpha", "n-beta"}, got)
}

// boomNotifier always returns an error containing "boom" from Send.
// Distinct from the existing failingNotifier so the two test suites can
// coexist without conflicting error-message assertions.
type boomNotifier struct {
	name  string
	total atomic.Int64
}

func (n *boomNotifier) Name() string                                 { return n.name }
func (n *boomNotifier) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: n.name} }
func (n *boomNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (n *boomNotifier) Reload(context.Context) error                 { return nil }
func (n *boomNotifier) Send(context.Context, snoozetypes.Record, plugins.NotificationPayload) error {
	n.total.Add(1)
	return errors.New("boom: dial tcp timeout")
}

// recordActions reads the persisted record's `actions` field by hash.
func recordActions(t *testing.T, h *testHost, hash string) []map[string]any {
	t.Helper()
	docs, _, err := h.driver.Search(tctx(), recordCollectionName, condition.Equals("hash", hash), db.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	raw, _ := docs[0]["actions"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// recDoc projects a record into the loose doc shape the driver stores, enough
// for these tests (hash + Extra fields).
func recDoc(rec snoozetypes.Record) db.Document {
	d := db.Document{}
	if rec.Host != "" {
		d["host"] = rec.Host
	}
	if rec.Hash != "" {
		d["hash"] = rec.Hash
	}
	for k, v := range rec.Extra {
		d[k] = v
	}
	return d
}

func TestActionMisconfiguredStampsError(t *testing.T) {
	h := newHost(t)
	writeEntries(t, h, []map[string]any{
		{"name": "n1", "condition": []any{"=", "host", "web01"}, "actions": []any{"ghost"}},
	})
	p := newPlugin(t, h)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "web01", Hash: "h1"})
	require.NoError(t, err)

	acts, _ := res.Record.Extra["actions"].([]any)
	require.Len(t, acts, 1)
	a := acts[0].(map[string]any)
	require.Equal(t, "ghost", a["name"])
	require.Equal(t, "n1", a["notification"])
	require.Equal(t, "error", a["status"])
	require.Contains(t, a["error"], "not found")
}

func TestActionFrequencyOffStampsSkipped(t *testing.T) {
	h := newHost(t)
	writeActions(t, h, []map[string]any{
		{"name": "mail", "action": map[string]any{"selected": "mail-notif", "subcontent": map[string]any{}}},
	})
	writeEntries(t, h, []map[string]any{
		{"name": "n1", "condition": []any{"=", "host", "web01"}, "actions": []any{"mail"},
			"frequency": map[string]any{"total": 0}},
	})
	h.registerNotifier(&recordingNotifier{name: "mail-notif"})
	p := newPlugin(t, h)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "web01", Hash: "h1"})
	require.NoError(t, err)
	acts := res.Record.Extra["actions"].([]any)
	require.Equal(t, "skipped", acts[0].(map[string]any)["status"])
}

func TestActionResolvesSuccess(t *testing.T) {
	h := newHost(t)
	writeActions(t, h, []map[string]any{
		{"name": "mail", "action": map[string]any{"selected": "mail-notif", "subcontent": map[string]any{}}},
	})
	writeEntries(t, h, []map[string]any{
		{"name": "n1", "condition": []any{"=", "host", "web01"}, "actions": []any{"mail"}},
	})
	rec := &recordingNotifier{name: "mail-notif"}
	h.registerNotifier(rec)
	p := newPlugin(t, h)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "web01", Hash: "h1"})
	require.NoError(t, err)
	_, err = h.driver.Write(tctx(), recordCollectionName, []db.Document{recDoc(res.Record)}, db.WriteOptions{Primary: []string{"hash"}, UpdateTime: true})
	require.NoError(t, err)

	require.Equal(t, "pending", res.Record.Extra["actions"].([]any)[0].(map[string]any)["status"])

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		acts := recordActions(t, h, "h1")
		if len(acts) == 1 && acts[0]["status"] == "success" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("action never resolved to success; last = %v", recordActions(t, h, "h1"))
}

func TestActionResolvesError(t *testing.T) {
	h := newHost(t)
	writeActions(t, h, []map[string]any{
		{"name": "hook", "action": map[string]any{"selected": "hook-notif", "subcontent": map[string]any{}}},
	})
	writeEntries(t, h, []map[string]any{
		{"name": "n1", "condition": []any{"=", "host", "web01"}, "actions": []any{"hook"}},
	})
	h.plugins["hook-notif"] = &boomNotifier{name: "hook-notif"}
	p := newPlugin(t, h)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "web01", Hash: "h1"})
	require.NoError(t, err)
	_, err = h.driver.Write(tctx(), recordCollectionName, []db.Document{recDoc(res.Record)}, db.WriteOptions{Primary: []string{"hash"}, UpdateTime: true})
	require.NoError(t, err)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		acts := recordActions(t, h, "h1")
		if len(acts) == 1 && acts[0]["status"] == "error" {
			require.Contains(t, acts[0]["error"], "boom")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("action never resolved to error; last = %v", recordActions(t, h, "h1"))
}

func TestActionPersistOffStaysSent(t *testing.T) {
	h := newHost(t)
	h.cfg.Notification.PersistActionOutcomes = false
	writeActions(t, h, []map[string]any{
		{"name": "mail", "action": map[string]any{"selected": "mail-notif", "subcontent": map[string]any{}}},
	})
	writeEntries(t, h, []map[string]any{
		{"name": "n1", "condition": []any{"=", "host", "web01"}, "actions": []any{"mail"}},
	})
	rec := &recordingNotifier{name: "mail-notif"}
	h.registerNotifier(rec)
	p := newPlugin(t, h)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "web01", Hash: "h1"})
	require.NoError(t, err)
	_, err = h.driver.Write(tctx(), recordCollectionName, []db.Document{recDoc(res.Record)}, db.WriteOptions{Primary: []string{"hash"}, UpdateTime: true})
	require.NoError(t, err)

	require.Equal(t, "sent", res.Record.Extra["actions"].([]any)[0].(map[string]any)["status"])
	waitForCalls(t, rec, 1, time.Second)

	time.Sleep(100 * time.Millisecond)
	require.Equal(t, "sent", recordActions(t, h, "h1")[0]["status"])
}

// TestNotification_TenantIsolation verifies that entries and actions loaded for
// tenant A are not visible when Reload is called for tenant B.
func TestNotification_TenantIsolation(t *testing.T) {
	t.Parallel()
	h := newHost(t)

	ctxA := auth.WithTenant(context.Background(), "acme")
	ctxB := auth.WithTenant(context.Background(), "beta")

	_, err := h.DB().Write(ctxA, "notification", []db.Document{{
		"name":    "notify-acme",
		"enabled": true,
		"actions": []any{},
	}}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: false})
	require.NoError(t, err)

	p := &Plugin{meta: plugins.Metadata{}}
	p.host = h
	require.NoError(t, p.Reload(ctxA))

	p.mu.RLock()
	acmeEntries := p.entries["acme"]
	p.mu.RUnlock()
	require.Len(t, acmeEntries, 1)

	require.NoError(t, p.Reload(ctxB))
	p.mu.RLock()
	betaEntries := p.entries["beta"]
	p.mu.RUnlock()
	require.Empty(t, betaEntries)
}

// TestSpawnCoordinatorPropagatesLoopChain verifies that the X-Snooze-Loop
// chain attached to the pipeline/request context (as the ingestion handler
// does for a relayed alert) survives into the context passed to
// Notifier.Send. Before the fix, spawnCoordinator rebuilt each sendCtx from
// context.Background() and re-attached only the tenant, silently dropping
// the loop chain — which would defeat a federation notifier's loop
// prevention.
func TestSpawnCoordinatorPropagatesLoopChain(t *testing.T) {
	h := newHost(t)
	writeActions(t, h, []map[string]any{
		{"name": "Federate", "action": map[string]any{"selected": "loopy", "subcontent": map[string]any{}}},
	})
	writeEntries(t, h, []map[string]any{
		{"name": "n1", "condition": []any{"=", "host", "web01"}, "actions": []any{"Federate"}},
	})
	notifier := &loopChainNotifier{name: "loopy", chain: make(chan []string, 1)}
	h.plugins[notifier.name] = notifier

	p := newPlugin(t, h)

	ctx := auth.WithLoopChain(tctx(), []string{"upstream-node"})
	_, err := p.Process(ctx, snoozetypes.Record{Host: "web01", Hash: "h1"})
	require.NoError(t, err)

	select {
	case got := <-notifier.chain:
		require.Equal(t, []string{"upstream-node"}, got)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notifier.Send to observe the loop chain")
	}
}

// ---------------------------------------------------------------------------
// Delivery history (notificationlog) — Task 4 / Task 6
// ---------------------------------------------------------------------------

// batchingNotifier models a notifier that queues the alert into a batch bucket
// and defers the outcome to the flush: it returns plugins.ErrBatched, the D8
// sentinel the coordinator must treat as "accepted, not delivered".
type batchingNotifier struct {
	name  string
	total atomic.Int64
}

func (n *batchingNotifier) Name() string                                 { return n.name }
func (n *batchingNotifier) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: n.name} }
func (n *batchingNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (n *batchingNotifier) Reload(context.Context) error                 { return nil }

func (n *batchingNotifier) Send(context.Context, snoozetypes.Record, plugins.NotificationPayload) error {
	n.total.Add(1)
	// Wrapped so the coordinator's errors.Is (not ==) is what is exercised.
	return fmt.Errorf("script: %w", plugins.ErrBatched)
}

// refNotifier models jira: it stores an external handle through the payload's
// inject callback during Send.
type refNotifier struct {
	name string
	ref  map[string]any
}

func (n *refNotifier) Name() string                                 { return n.name }
func (n *refNotifier) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: n.name} }
func (n *refNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (n *refNotifier) Reload(context.Context) error                 { return nil }

func (n *refNotifier) Send(_ context.Context, _ snoozetypes.Record, payload plugins.NotificationPayload) error {
	plugins.StoreNotifyRef(payload, payload.ActionName(), n.ref)
	return nil
}

// deliveryRows reads every row currently in the delivery-history collection,
// oldest first.
func deliveryRows(t *testing.T, h *testHost) []db.Document {
	t.Helper()
	docs, _, err := h.driver.Search(tctx(), plugins.NotificationLogCollection, condition.Cond{}, db.Page{})
	if err != nil {
		// The collection is created lazily on first write; before that a
		// search may legitimately report "unknown collection" on some drivers.
		return nil
	}
	return docs
}

// waitForDeliveryRows polls until the delivery log holds want rows, then keeps
// them. Rows are written from the detached coordinator goroutine, so no test
// may assume Process persisted them.
func waitForDeliveryRows(t *testing.T, h *testHost, want int, timeout time.Duration) []db.Document {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var rows []db.Document
	for time.Now().Before(deadline) {
		rows = deliveryRows(t, h)
		if len(rows) >= want {
			return rows
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("delivery log holds %d rows, want %d within %s", len(rows), want, timeout)
	return nil
}

// entryUID reads back the server-assigned uid of a seeded notification entry.
func entryUID(t *testing.T, h *testHost, name string) string {
	t.Helper()
	doc, err := h.driver.GetOne(tctx(), collectionName, db.Document{"name": name})
	require.NoError(t, err)
	uid, _ := doc["uid"].(string)
	require.NotEmpty(t, uid, "seeded notification %q has no uid", name)
	return uid
}

// seedRecord writes an alert row so the coordinator's hash-keyed write-back and
// uid resolution have something to find, and returns its server-assigned uid.
func seedRecord(t *testing.T, h *testHost, hash string) string {
	t.Helper()
	_, err := h.driver.Write(tctx(), recordCollectionName,
		[]db.Document{{"hash": hash, "host": "db-01", "message": "disk 98%", "severity": "critical", "state": "open"}},
		db.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
	doc, err := h.driver.GetOne(tctx(), recordCollectionName, db.Document{"hash": hash})
	require.NoError(t, err)
	uid, _ := doc["uid"].(string)
	require.NotEmpty(t, uid)
	return uid
}

// strList flattens a stored []any array of strings.
func strList(t *testing.T, v any) []string {
	t.Helper()
	raw, ok := v.([]any)
	require.True(t, ok, "expected a list, got %T", v)
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		s, _ := e.(string)
		out = append(out, s)
	}
	return out
}

// firstAlert returns the first member snapshot of a stored delivery row.
func firstAlert(t *testing.T, row db.Document) map[string]any {
	t.Helper()
	alerts, ok := row["alerts"].([]any)
	require.True(t, ok, "row has no alerts array: %#v", row["alerts"])
	require.NotEmpty(t, alerts)
	m, ok := alerts[0].(map[string]any)
	require.True(t, ok)
	return m
}

// getOneCountDriver counts hash-keyed GetOne reads of the record collection —
// i.e. the D9 uid resolution — so a test can prove it did NOT happen.
type getOneCountDriver struct {
	db.Driver
	n atomic.Int64
}

func (d *getOneCountDriver) GetOne(ctx context.Context, collection string, search db.Document) (db.Document, error) {
	if collection == recordCollectionName {
		if _, byHash := search["hash"]; byHash {
			d.n.Add(1)
		}
	}
	return d.Driver.GetOne(ctx, collection, search)
}

// newPluginWithBudget is newPlugin with a shrunken write-back deadline, for the
// tests that exercise "the record never lands" without waiting ten seconds.
func newPluginWithBudget(t *testing.T, h *testHost, budget time.Duration) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "notification"}, writeBackBudget: budget}
	require.NoError(t, p.PostInit(tctx(), h))
	return p
}

// TestNotificationCountersAreStrippedFromWrites is the server-side half of the
// `readOnly: true` promise the OpenAPI makes for hits / last_sent: the schema
// documents it, TransformWrite is what enforces it.
func TestNotificationCountersAreStrippedFromWrites(t *testing.T) {
	host := newHost(t)
	p := newPlugin(t, host)

	// The generic CRUD create/replace/patch handlers only call the hook when
	// the plugin implements it.
	var _ plugins.WriteTransformer = p

	writeEntries(t, host, []map[string]any{{"name": "PageOncall", "condition": []any{}}})
	uid := entryUID(t, host, "PageOncall")

	// The dispatcher stamped a real counter value.
	_, err := host.driver.SetFields(tctx(), collectionName,
		db.Document{"hits": 7, "last_sent": 111}, condition.Equals("uid", uid))
	require.NoError(t, err)

	// What patchHandler does: stamp the URL uid, run TransformWrite, merge.
	patch := db.Document{"uid": uid, "name": "PageOncall", "hits": 999, "last_sent": 999}
	require.NoError(t, p.TransformWrite(tctx(), patch))
	require.NotContains(t, patch, "hits", "a client-supplied counter never reaches the DB")
	require.NotContains(t, patch, "last_sent")
	require.Equal(t, "PageOncall", patch["name"], "everything else is left alone")
	require.NoError(t, host.driver.UpdateOne(tctx(), collectionName, uid, patch, true))

	doc, err := host.driver.GetOne(tctx(), collectionName, db.Document{"uid": uid})
	require.NoError(t, err)
	hits, _ := plugins.CounterValue(doc["hits"])
	require.Equal(t, int64(7), hits, "PATCH hits: 999 must leave the stored counter untouched")
	last, _ := plugins.CounterValue(doc["last_sent"])
	require.Equal(t, int64(111), last)

	// A body that never mentions the counters is unchanged.
	clean := db.Document{"uid": uid, "name": "Renamed"}
	require.NoError(t, p.TransformWrite(tctx(), clean))
	require.Equal(t, db.Document{"uid": uid, "name": "Renamed"}, clean)
}

func TestDeliveryLog(t *testing.T) {
	const (
		goodAction = "MailOncall"
		badAction  = "MailBroken"
		notifName  = "PageOncall"
	)

	// seed installs one notification with the given actions and returns the
	// host, the plugin and the notification's uid.
	seed := func(t *testing.T, host *testHost, actions []map[string]any, entryActions []any) (*Plugin, string) {
		t.Helper()
		writeActions(t, host, actions)
		writeEntries(t, host, []map[string]any{
			{"name": notifName, "condition": []any{}, "actions": entryActions},
		})
		return newPlugin(t, host), entryUID(t, host, notifName)
	}

	t.Run("success_send_writes_one_row", func(t *testing.T) {
		host := newHost(t)
		notifier := &recordingNotifier{name: "mail"}
		host.registerNotifier(notifier)
		p, notifUID := seed(t, host,
			[]map[string]any{{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
			[]any{goodAction})

		uid := seedRecord(t, host, "hash-success")
		_, err := p.Process(tctx(), snoozetypes.Record{
			Hash: "hash-success", UID: uid, Host: "db-01", Severity: "critical",
			Message: "disk 98%", State: "open", Timestamp: time.Now(),
		})
		require.NoError(t, err)

		rows := waitForDeliveryRows(t, host, 1, 2*time.Second)
		require.Len(t, rows, 1)
		row := rows[0]
		require.Equal(t, "success", row["status"])
		require.Equal(t, goodAction, row["action"])
		require.Equal(t, "mail", row["notifier"])
		require.Equal(t, false, row["batch"])
		require.NotContains(t, row, "error")
		require.Equal(t, []string{notifUID}, strList(t, row["notification_uids"]))
		require.Equal(t, []string{notifName}, strList(t, row["notification_names"]))
		require.Equal(t, []string{uid}, strList(t, row["alert_uids"]))

		epoch, ok := plugins.CounterValue(row["date_epoch"])
		require.True(t, ok)
		require.Greater(t, epoch, int64(0))
		queued, ok := plugins.CounterValue(row["queued_epoch"])
		require.True(t, ok)
		require.Greater(t, queued, int64(0))
		durMS, ok := plugins.CounterValue(row["duration_ms"])
		require.True(t, ok)
		require.GreaterOrEqual(t, durMS, int64(0))
		count, ok := plugins.CounterValue(row["alert_count"])
		require.True(t, ok)
		require.Equal(t, int64(1), count)

		alert := firstAlert(t, row)
		require.Equal(t, "db-01", alert["host"])
		require.Equal(t, "critical", alert["severity"])
		require.Equal(t, "disk 98%", alert["message"])
		require.Equal(t, "open", alert["state"])
		require.Equal(t, notifName, alert["notification"])
		require.Equal(t, uid, alert["uid"])
	})

	t.Run("failed_send_writes_error_row", func(t *testing.T) {
		host := newHost(t)
		host.plugins["mail"] = &failingNotifier{name: "mail"}
		p, _ := seed(t, host,
			[]map[string]any{{"name": badAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
			[]any{badAction})

		seedRecord(t, host, "hash-fail")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-fail", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		rows := waitForDeliveryRows(t, host, 1, 2*time.Second)
		require.Len(t, rows, 1)
		require.Equal(t, "error", rows[0]["status"])
		require.Equal(t, "send: simulated failure", rows[0]["error"])
		require.Equal(t, badAction, rows[0]["action"])
	})

	t.Run("misconfigured_action_writes_error_row_off_the_ingest_path", func(t *testing.T) {
		host := newHost(t)
		writeEntries(t, host, []map[string]any{
			{"name": notifName, "condition": []any{}, "actions": []any{"DoesNotExist"}},
		})
		p := newPlugin(t, host)

		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-missing", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		// D11: no delivery write happens on the pipeline goroutine, not even
		// for an action that never resolved — so the row shows up shortly
		// after Process returned, not during it.
		rows := waitForDeliveryRows(t, host, 1, 2*time.Second)
		require.Len(t, rows, 1)
		require.Equal(t, "error", rows[0]["status"])
		require.Equal(t, `action "DoesNotExist" not found`, rows[0]["error"])
		require.Equal(t, "DoesNotExist", rows[0]["action"])
		require.Equal(t, "", rows[0]["notifier"], "action doc was never found, so no notifier key is known")
	})

	t.Run("misconfigured_notifier_row_keeps_the_selected_key", func(t *testing.T) {
		host := newHost(t)
		p, _ := seed(t, host,
			[]map[string]any{{"name": badAction, "action": map[string]any{"selected": "nope", "subcontent": map[string]any{}}}},
			[]any{badAction})

		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-noplug", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		rows := waitForDeliveryRows(t, host, 1, 2*time.Second)
		require.Len(t, rows, 1)
		require.Equal(t, `notifier "nope" not registered`, rows[0]["error"])
		require.Equal(t, "nope", rows[0]["notifier"])
	})

	t.Run("misconfigured_rows_are_deduped_per_notification_and_action", func(t *testing.T) {
		host := newHost(t)
		writeEntries(t, host, []map[string]any{
			{"name": notifName, "condition": []any{}, "actions": []any{"DoesNotExist"}},
		})
		p := newPlugin(t, host)

		// A broken action sits on a condition that keeps matching. Fifty alerts
		// must not become fifty identical rows.
		for i := range 50 {
			_, err := p.Process(tctx(), snoozetypes.Record{
				Hash: fmt.Sprintf("hash-dupe-%d", i), Host: "db-01", Timestamp: time.Now(),
			})
			require.NoError(t, err)
		}

		waitForDeliveryRows(t, host, 1, 2*time.Second)
		time.Sleep(100 * time.Millisecond) // let any stragglers land
		rows := deliveryRows(t, host)
		require.Len(t, rows, 1, "one row per (tenant, notification, action) per window")
		require.Equal(t, "DoesNotExist", rows[0]["action"])

		// The window is per combination, so a DIFFERENT broken action still
		// gets its own row.
		writeEntries(t, host, []map[string]any{
			{"name": "OtherNotification", "condition": []any{}, "actions": []any{"AlsoMissing"}},
		})
		require.NoError(t, p.Reload(tctx()))
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-dupe-other", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)
		rows = waitForDeliveryRows(t, host, 2, 2*time.Second)
		require.Len(t, rows, 2)
	})

	t.Run("misconfigured_row_reappears_once_the_window_expires", func(t *testing.T) {
		host := newHost(t)
		writeEntries(t, host, []map[string]any{
			{"name": notifName, "condition": []any{}, "actions": []any{"DoesNotExist"}},
		})
		p := newPlugin(t, host)
		e := Entry{Name: notifName, UID: "n-1"}

		now := time.Now()
		require.True(t, p.allowMisconfiguredRow(snoozetypes.DefaultTenant, e, "A", now))
		require.False(t, p.allowMisconfiguredRow(snoozetypes.DefaultTenant, e, "A", now.Add(time.Minute)))
		require.True(t, p.allowMisconfiguredRow(snoozetypes.DefaultTenant, e, "A", now.Add(misconfiguredRowWindow)),
			"the rate limit is a window, not a mute button")

		// Another tenant hitting the same notification+action is a separate
		// deployment's problem and gets its own row.
		require.True(t, p.allowMisconfiguredRow("acme", e, "A", now.Add(time.Minute)))

		// The sweep keeps the map bounded: the stale entry from the first call
		// is gone once a later insert runs.
		p.misconfMu.Lock()
		size := len(p.misconfSeen)
		p.misconfMu.Unlock()
		require.LessOrEqual(t, size, 2, "expired entries are pruned on insert")
	})

	t.Run("frequency_skipped_writes_nothing", func(t *testing.T) {
		host := newHost(t)
		notifier := &recordingNotifier{name: "mail"}
		host.registerNotifier(notifier)
		writeActions(t, host, []map[string]any{
			{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host, []map[string]any{
			{"name": notifName, "condition": []any{}, "actions": []any{goodAction},
				"frequency": map[string]any{"total": 0}},
		})
		p := newPlugin(t, host)

		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-skip", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)
		require.Empty(t, deliveryRows(t, host), "a frequency-suppressed action is not a delivery")
	})

	t.Run("batched_send_writes_no_row_no_stat_and_stamps_sent", func(t *testing.T) {
		host, capDrv := newMetricsHost(t)
		host.cfg.General.MetricsEnabled = true
		batching := &batchingNotifier{name: "script"}
		host.plugins["script"] = batching
		writeActions(t, host.testHost, []map[string]any{
			{"name": goodAction, "action": map[string]any{"selected": "script", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host.testHost, []map[string]any{
			{"name": notifName, "condition": []any{}, "actions": []any{goodAction}},
		})
		p := newPlugin(t, host.testHost)

		seedRecord(t, host.testHost, "hash-batch")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-batch", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		// Wait for the write-back so the coordinator has definitely finished.
		var actions []any
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			doc, err := host.driver.GetOne(tctx(), recordCollectionName, db.Document{"hash": "hash-batch"})
			require.NoError(t, err)
			if raw, ok := doc["actions"].([]any); ok && len(raw) > 0 {
				actions = raw
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		require.Len(t, actions, 1, "the coordinator never wrote the actions array back")
		entry, ok := actions[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, actionSent, entry["status"],
			"a batched send is dispatched, not delivered: the flush owns the outcome")

		require.Empty(t, deliveryRows(t, host.testHost), "the flush writes the row, not the dispatcher")
		require.NoError(t, host.writer.Flush(context.Background()))
		// No action_success / action_error FROM THE COORDINATOR: the outcome is
		// the flush's to report, and the flush (a real batching notifier's
		// flushBucket, not this fake) records one stat per member there. The
		// fake never flushes, so nothing at all should be here.
		for _, op := range capDrv.Captured() {
			require.NotEqual(t, "action_error", op.search["metric"], "ErrBatched is not a failure")
			require.NotEqual(t, "action_success", op.search["metric"], "ErrBatched is not a delivery")
		}
	})

	t.Run("member_uid_resolved_from_hash_when_record_has_none", func(t *testing.T) {
		host := newHost(t)
		notifier := &recordingNotifier{name: "mail"}
		host.registerNotifier(notifier)
		p, _ := seed(t, host,
			[]map[string]any{{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
			[]any{goodAction})

		// The record already exists in the DB (aggregated duplicate landing
		// mid-pipeline) but the in-memory copy the dispatcher sees has no uid,
		// exactly like a first occurrence.
		uid := seedRecord(t, host, "hash-nouid")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-nouid", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		rows := waitForDeliveryRows(t, host, 1, 2*time.Second)
		require.Equal(t, uid, firstAlert(t, rows[0])["uid"], "uid must be resolved by hash after the write-back")
		require.Equal(t, []string{uid}, strList(t, rows[0]["alert_uids"]))
	})

	t.Run("rows_written_when_action_outcomes_are_not_persisted", func(t *testing.T) {
		host := newHost(t)
		host.cfg.Notification.PersistActionOutcomes = false
		notifier := &recordingNotifier{name: "mail"}
		host.registerNotifier(notifier)
		p, _ := seed(t, host,
			[]map[string]any{{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
			[]any{goodAction})

		uid := seedRecord(t, host, "hash-nopersist")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-nopersist", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		rows := waitForDeliveryRows(t, host, 1, 2*time.Second)
		require.Equal(t, "success", rows[0]["status"])
		require.Equal(t, uid, firstAlert(t, rows[0])["uid"])

		doc, err := host.driver.GetOne(tctx(), recordCollectionName, db.Document{"hash": "hash-nopersist"})
		require.NoError(t, err)
		require.NotContains(t, doc, "actions", "persist_action_outcomes=false must still skip the write-back")
	})

	t.Run("counters_bump_once_per_notification_per_record", func(t *testing.T) {
		host := newHost(t)
		host.registerNotifier(&recordingNotifier{name: "mail"})
		host.registerNotifier(&recordingNotifier{name: "chat"})
		p, notifUID := seed(t, host, []map[string]any{
			{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}},
			{"name": "ChatOncall", "action": map[string]any{"selected": "chat", "subcontent": map[string]any{}}},
		}, []any{goodAction, "ChatOncall"})

		before := time.Now().Unix()
		seedRecord(t, host, "hash-counters")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-counters", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		waitForDeliveryRows(t, host, 2, 2*time.Second)
		// The counter write happens before the rows are persisted, so once both
		// rows are visible the bump has landed.
		doc, err := host.driver.GetOne(tctx(), collectionName, db.Document{"uid": notifUID})
		require.NoError(t, err)
		hits, ok := plugins.CounterValue(doc["hits"])
		require.True(t, ok, "hits missing: %#v", doc)
		require.Equal(t, int64(1), hits, "two actions of one notification are one notification hit")
		lastSent, ok := plugins.CounterValue(doc["last_sent"])
		require.True(t, ok)
		require.GreaterOrEqual(t, lastSent, before)
	})

	t.Run("counters_untouched_on_failed_delivery", func(t *testing.T) {
		host := newHost(t)
		host.plugins["mail"] = &failingNotifier{name: "mail"}
		p, notifUID := seed(t, host,
			[]map[string]any{{"name": badAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
			[]any{badAction})

		seedRecord(t, host, "hash-nocounter")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-nocounter", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		waitForDeliveryRows(t, host, 1, 2*time.Second)
		doc, err := host.driver.GetOne(tctx(), collectionName, db.Document{"uid": notifUID})
		require.NoError(t, err)
		require.NotContains(t, doc, "hits")
		require.NotContains(t, doc, "last_sent")
	})

	t.Run("delivery_log_disabled_writes_no_rows_but_still_stamps_outcomes", func(t *testing.T) {
		host := newHost(t)
		host.cfg.Notification.DeliveryLog = false
		notifier := &recordingNotifier{name: "mail"}
		host.registerNotifier(notifier)
		p, _ := seed(t, host,
			[]map[string]any{{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
			[]any{goodAction})

		seedRecord(t, host, "hash-off")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-off", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		var actions []any
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			doc, err := host.driver.GetOne(tctx(), recordCollectionName, db.Document{"hash": "hash-off"})
			require.NoError(t, err)
			if raw, ok := doc["actions"].([]any); ok && len(raw) > 0 {
				actions = raw
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		require.Len(t, actions, 1)
		entry, _ := actions[0].(map[string]any)
		require.Equal(t, actionSuccess, entry["status"], "the outcome stamp is independent of the delivery log")
		require.Empty(t, deliveryRows(t, host), "delivery_log=false must suppress every row")
	})

	t.Run("captures_notify_ref_and_derives_the_jira_url", func(t *testing.T) {
		host := newHost(t)
		host.plugins["jira"] = &refNotifier{name: "jira", ref: map[string]any{"issue_key": "OPS-123"}}
		p, _ := seed(t, host, []map[string]any{
			{"name": "JiraOncall", "action": map[string]any{
				"selected":   "jira",
				"subcontent": map[string]any{"jira_url": "https://jira.example.com/"},
			}},
		}, []any{"JiraOncall"})

		seedRecord(t, host, "hash-jira")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-jira", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		rows := waitForDeliveryRows(t, host, 1, 2*time.Second)
		ref, ok := rows[0]["ref"].(map[string]any)
		require.True(t, ok, "row carries no ref: %#v", rows[0])
		require.Equal(t, "OPS-123", ref["issue_key"])
		require.Equal(t, "https://jira.example.com/browse/OPS-123", ref["url"])

		// The wrapper must not change what the notifier writes to the record.
		doc, err := host.driver.GetOne(tctx(), recordCollectionName, db.Document{"hash": "hash-jira"})
		require.NoError(t, err)
		stored, ok := doc["notify_ref_JiraOncall"].(map[string]any)
		require.True(t, ok, "inject_response must still land on the record: %#v", doc)
		require.Equal(t, "OPS-123", stored["issue_key"])
	})

	t.Run("row_is_written_even_when_the_record_never_lands", func(t *testing.T) {
		host := newHost(t)
		host.registerNotifier(&recordingNotifier{name: "mail"})
		writeActions(t, host, []map[string]any{
			{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}},
		})
		writeEntries(t, host, []map[string]any{
			{"name": notifName, "condition": []any{}, "actions": []any{goodAction}},
		})
		// A tight budget so the hash write-back exhausts it quickly. The record
		// is deliberately never seeded, so the write-back polls until it gives
		// up — which used to leave the delivery row to be written on a context
		// that was already dead, silently losing the history of a send that
		// really happened.
		p := newPluginWithBudget(t, host, 150*time.Millisecond)

		_, err := p.Process(tctx(), snoozetypes.Record{
			Hash: "hash-never-lands", Host: "db-01", Severity: "critical",
			Message: "disk 98%", State: "open", Timestamp: time.Now(),
		})
		require.NoError(t, err)

		rows := waitForDeliveryRows(t, host, 1, 3*time.Second)
		require.Len(t, rows, 1)
		require.Equal(t, "success", rows[0]["status"])
		alert := firstAlert(t, rows[0])
		require.Equal(t, "hash-never-lands", alert["hash"], "the hash is what the UI links on")
		require.Nil(t, alert["uid"], "no record, no uid — the row still has to exist")
		require.Empty(t, strList(t, rows[0]["alert_uids"]))
	})

	t.Run("delivery_log_disabled_skips_the_uid_read", func(t *testing.T) {
		// The switch is meant to make dispatch cheaper, not just quieter: with
		// the log off the coordinator must do no hash lookup and no row write.
		run := func(t *testing.T, enabled bool) (int64, int) {
			t.Helper()
			host := newHost(t)
			host.cfg.Notification.DeliveryLog = enabled
			counting := &getOneCountDriver{Driver: host.driver}
			host.driver = counting
			host.registerNotifier(&recordingNotifier{name: "mail"})
			p, _ := seed(t, host,
				[]map[string]any{{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
				[]any{goodAction})

			seedRecord(t, host, "hash-uidread")
			counting.n.Store(0) // the seeding helper reads by hash too

			// No uid on the in-memory record: exactly the first-occurrence
			// shape that makes the coordinator resolve one.
			_, err := p.Process(tctx(), snoozetypes.Record{
				Hash: "hash-uidread", Host: "db-01", Timestamp: time.Now(),
			})
			require.NoError(t, err)

			// Wait for the write-back so the coordinator has certainly reached
			// (or skipped) the delivery phase.
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				doc, err := host.driver.GetOne(tctx(), recordCollectionName, db.Document{"hash": "hash-uidread"})
				require.NoError(t, err)
				if raw, ok := doc["actions"].([]any); ok && len(raw) > 0 {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			counting.n.Store(0) // the poll above reads by hash as well
			time.Sleep(150 * time.Millisecond)
			return counting.n.Load(), len(deliveryRows(t, host))
		}

		reads, rows := run(t, false)
		require.Zero(t, reads, "delivery_log=false must not read the record by hash")
		require.Zero(t, rows)
	})

	t.Run("counters_bump_even_when_the_delivery_log_is_off", func(t *testing.T) {
		// hits / last_sent are a separate feature with no switch of their own:
		// an operator who turned the history off still sees "Sent 142×".
		host := newHost(t)
		host.cfg.Notification.DeliveryLog = false
		host.registerNotifier(&recordingNotifier{name: "mail"})
		p, notifUID := seed(t, host,
			[]map[string]any{{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
			[]any{goodAction})

		seedRecord(t, host, "hash-counters-nolog")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-counters-nolog", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			doc, err := host.driver.GetOne(tctx(), collectionName, db.Document{"uid": notifUID})
			require.NoError(t, err)
			hits, ok := plugins.CounterValue(doc["hits"])
			return ok && hits == 1
		}, 2*time.Second, 10*time.Millisecond, "the counters do not depend on the delivery log")
		require.Empty(t, deliveryRows(t, host))
	})

	t.Run("no_ref_for_a_notifier_that_stores_nothing", func(t *testing.T) {
		host := newHost(t)
		host.registerNotifier(&recordingNotifier{name: "mail"})
		p, _ := seed(t, host,
			[]map[string]any{{"name": goodAction, "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}}},
			[]any{goodAction})

		seedRecord(t, host, "hash-noref")
		_, err := p.Process(tctx(), snoozetypes.Record{Hash: "hash-noref", Host: "db-01", Timestamp: time.Now()})
		require.NoError(t, err)

		rows := waitForDeliveryRows(t, host, 1, 2*time.Second)
		require.NotContains(t, rows[0], "ref")
	})
}
