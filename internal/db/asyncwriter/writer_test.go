package asyncwriter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
)

// stubDriver implements just enough of db.Driver to record BulkIncrement calls.
type stubDriver struct {
	mu     sync.Mutex
	calls  []bulkCall
	signal chan struct{}
}

type bulkCall struct {
	collection string
	ops        []db.IncrementOp
	upsert     bool
}

func newStubDriver() *stubDriver { return &stubDriver{signal: make(chan struct{}, 16)} }

func (s *stubDriver) BulkIncrement(_ context.Context, collection string, ops []db.IncrementOp, upsert bool) error {
	s.mu.Lock()
	s.calls = append(s.calls, bulkCall{collection: collection, ops: cloneOps(ops), upsert: upsert})
	s.mu.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
	return nil
}

func cloneOps(ops []db.IncrementOp) []db.IncrementOp {
	out := make([]db.IncrementOp, len(ops))
	for i, op := range ops {
		deltas := make(map[string]int64, len(op.Deltas))
		for k, v := range op.Deltas {
			deltas[k] = v
		}
		search := make(db.Document, len(op.Search))
		for k, v := range op.Search {
			search[k] = v
		}
		out[i] = db.IncrementOp{Search: search, Deltas: deltas}
	}
	return out
}

func (s *stubDriver) Snapshot() []bulkCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]bulkCall, len(s.calls))
	copy(out, s.calls)
	return out
}

// The remaining Driver methods are unused by Writer and return zero values.

func (s *stubDriver) Search(context.Context, string, condition.Cond, db.Page) ([]db.Document, int, error) {
	return nil, 0, nil
}
func (s *stubDriver) GetOne(context.Context, string, db.Document) (db.Document, error) {
	return nil, nil
}
func (s *stubDriver) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, nil
}
func (s *stubDriver) Write(context.Context, string, []db.Document, db.WriteOptions) (db.WriteResult, error) {
	return db.WriteResult{}, nil
}
func (s *stubDriver) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, nil
}
func (s *stubDriver) UpdateOne(context.Context, string, string, db.Document, bool) error {
	return nil
}
func (s *stubDriver) Delete(context.Context, string, condition.Cond, bool) (int, error) {
	return 0, nil
}
func (s *stubDriver) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, nil
}
func (s *stubDriver) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, nil
}
func (s *stubDriver) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (s *stubDriver) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (s *stubDriver) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (s *stubDriver) CreateIndex(context.Context, string, []string) error { return nil }
func (s *stubDriver) ListCollections(context.Context) ([]string, error)   { return nil, nil }
func (s *stubDriver) Drop(context.Context, string) error                  { return nil }
func (s *stubDriver) Backup(context.Context, string, []string) error      { return nil }
func (s *stubDriver) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (s *stubDriver) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (s *stubDriver) CleanupOrphans(context.Context, string) (int, error) { return 0, nil }
func (s *stubDriver) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (s *stubDriver) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (s *stubDriver) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (s *stubDriver) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (s *stubDriver) RenumberField(context.Context, string, string) error { return nil }
func (s *stubDriver) Watcher() syncer.Bus                                 { return nil }
func (s *stubDriver) Close() error                                        { return nil }

func TestWriter_MergesDeltas(t *testing.T) {
	d := newStubDriver()
	clock := NewMockClock(time.Unix(0, 0))
	w := New(d, time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- w.Run(ctx) }()

	// Enqueue increments before advancing.
	w.Increment("snooze", "hits", db.Document{"name": "rule 01"}, 1)
	w.Increment("snooze", "hits", db.Document{"name": "rule 01"}, 2)
	w.Increment("snooze", "hits", db.Document{"name": "rule 02"}, 5)
	// Spin until accept has drained the queue, otherwise advancing the clock
	// would race the Increment send.
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.buckets["snooze"]) == 2
	}, time.Second, time.Millisecond)
	clock.Advance(time.Second)

	select {
	case <-d.signal:
	case <-time.After(time.Second):
		t.Fatal("flush did not fire")
	}

	calls := d.Snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "snooze", calls[0].collection)
	require.Len(t, calls[0].ops, 2)
	// Find rule 01 / 02
	got := map[string]int64{}
	for _, op := range calls[0].ops {
		got[op.Search["name"].(string)] = op.Deltas["hits"]
	}
	require.Equal(t, map[string]int64{"rule 01": 3, "rule 02": 5}, got)

	cancel()
	err := <-doneCh
	require.NoError(t, err)
}

func TestWriter_DrainOnShutdown(t *testing.T) {
	d := newStubDriver()
	clock := NewMockClock(time.Unix(0, 0))
	w := New(d, 10*time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- w.Run(ctx) }()

	w.Increment("snooze", "hits", db.Document{"name": "rule"}, 7)
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.buckets["snooze"]) == 1
	}, time.Second, time.Millisecond)

	cancel()
	err := <-doneCh
	require.NoError(t, err)

	calls := d.Snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, int64(7), calls[0].ops[0].Deltas["hits"])
}

func TestWriter_NoFlushWhenEmpty(t *testing.T) {
	d := newStubDriver()
	clock := NewMockClock(time.Unix(0, 0))
	w := New(d, time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-doneCh
	}()

	clock.Advance(time.Second)
	// No requests outstanding — no calls expected. We can only assert
	// absence by waiting a brief window; the writer is fast enough that
	// even a few ms is enough to surface a spurious flush.
	time.Sleep(20 * time.Millisecond)
	require.Empty(t, d.Snapshot())
}
