package notification

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/japannext/snooze/internal/config"
	"github.com/japannext/snooze/internal/db"
	dbsqlite "github.com/japannext/snooze/internal/db/sqlite"
	"github.com/japannext/snooze/internal/mq"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/telemetry"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// recordingBus wraps an mq.Bus and counts every Publish call. It is the
// fake Notifier-side endpoint the tests assert against.
type recordingBus struct {
	inner mq.Bus

	mu       sync.Mutex
	calls    []recordedCall
	total    atomic.Int64
}

type recordedCall struct {
	Queue   string
	Payload Payload
}

func (b *recordingBus) Publish(ctx context.Context, queue string, payload any) error {
	// Round-trip the payload through JSON so the test sees what a
	// downstream consumer would deserialize, not the in-memory pointer.
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	b.mu.Lock()
	b.calls = append(b.calls, recordedCall{Queue: queue, Payload: p})
	b.mu.Unlock()
	b.total.Add(1)
	return b.inner.Publish(ctx, queue, payload)
}

func (b *recordingBus) Subscribe(ctx context.Context, queue string, opts mq.SubscribeOpts, h mq.Handler) error {
	return b.inner.Subscribe(ctx, queue, opts, h)
}

func (b *recordingBus) Close() error { return b.inner.Close() }

func (b *recordingBus) Calls() []recordedCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]recordedCall, len(b.calls))
	copy(out, b.calls)
	return out
}

// testHost is a minimal plugins.Host suitable for the notification plugin.
type testHost struct {
	driver db.Driver
	bus    mq.Bus
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
	tracer trace.Tracer
}

func (h *testHost) DB() db.Driver                { return h.driver }
func (h *testHost) Bus() plugins.Bus             { return h.bus }
func (h *testHost) Logger() *slog.Logger         { return h.logger }
func (h *testHost) Tracer() trace.Tracer         { return h.tracer }
func (h *testHost) Metrics() *telemetry.Registry { return h.metr }
func (h *testHost) Config() *config.Config       { return h.cfg }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

// newHost wires a fresh SQLite driver + recording bus.
func newHost(t *testing.T) (*testHost, *recordingBus) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := dbsqlite.New(context.Background(), dbsqlite.Config{Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	inner := mq.NewInproc(mq.InprocConfig{QueueBuffer: 64})
	t.Cleanup(func() { _ = inner.Close() })
	rb := &recordingBus{inner: inner}

	return &testHost{
		driver: drv,
		bus:    rb,
		logger: slog.Default(),
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(nil),
		tracer: otel.Tracer("notification-test"),
	}, rb
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

func newPlugin(t *testing.T, h *testHost) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "notification"}}
	require.NoError(t, p.PostInit(context.Background(), h))
	return p
}

func TestNotification(t *testing.T) {
	t.Run("notification_echo_publishes_one_per_action", func(t *testing.T) {
		// Mirrors test_notification_echo: a single matching notification
		// with a single action emits exactly one bus message.
		host, bus := newHost(t)
		writeEntries(t, host, []map[string]any{
			{
				"name":      "Notification1",
				"condition": []any{"=", "host", "myhost01"},
				"actions":   []any{"Script"},
			},
		})

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

		calls := bus.Calls()
		require.Len(t, calls, 1)
		require.Equal(t, "notification.Script", calls[0].Queue)
		require.Equal(t, "Notification1", calls[0].Payload.NotificationName)
		require.Equal(t, "Script", calls[0].Payload.Action)
		require.Equal(t, "myhost01", calls[0].Payload.Record.Host)
	})

	t.Run("match_true_with_time_constraint", func(t *testing.T) {
		// Mirrors test_match_true: a record arriving inside the daily
		// time-window and on an allowed weekday is dispatched, while a
		// record outside either family is skipped. We seed two
		// notifications to exercise both branches in one DB round-trip.
		host, bus := newHost(t)

		// Wednesday 2021-07-07 11:00 UTC — inside the Time window
		// (10:00 -> 14:00) and on a Wednesday (weekday=3 in
		// Sun=0..Sat=6).
		inside := time.Date(2021, 7, 7, 11, 0, 0, 0, time.UTC)
		// Same daily window but a Saturday → weekday match fails.
		outside := time.Date(2021, 7, 10, 11, 0, 0, 0, time.UTC)

		writeEntries(t, host, []map[string]any{
			{
				"name":      "Notification 1",
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

		p := newPlugin(t, host)

		// In-window record dispatches.
		_, err := p.Process(context.Background(), snoozetypes.Record{
			UID:       "uid-in",
			Host:      "myhost01",
			Message:   "my message",
			Timestamp: inside,
		})
		require.NoError(t, err)

		// Out-of-window record does not.
		_, err = p.Process(context.Background(), snoozetypes.Record{
			UID:       "uid-out",
			Host:      "myhost01",
			Message:   "my message",
			Timestamp: outside,
		})
		require.NoError(t, err)

		calls := bus.Calls()
		require.Len(t, calls, 1, "expected exactly one dispatch for the in-window record")
		require.Equal(t, "uid-in", calls[0].Payload.Record.UID)
	})

	t.Run("ack_close_records_skip_dispatch", func(t *testing.T) {
		// Python's `process` short-circuits on state in {'ack','close'}.
		host, bus := newHost(t)
		writeEntries(t, host, []map[string]any{
			{
				"name":      "Always",
				"condition": []any{}, // AlwaysTrue
				"actions":   []any{"Script"},
			},
		})
		p := newPlugin(t, host)

		for _, state := range []string{"ack", "close"} {
			_, err := p.Process(context.Background(), snoozetypes.Record{
				UID:       "uid-" + state,
				State:     state,
				Timestamp: time.Now(),
			})
			require.NoError(t, err)
		}
		require.Empty(t, bus.Calls(), "no dispatch expected for ack/close records")
	})
}
