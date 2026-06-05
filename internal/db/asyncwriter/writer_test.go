package asyncwriter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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
	tenant     string // tenant slug resolved from the call's ctx ("" when none)
}

func newStubDriver() *stubDriver { return &stubDriver{signal: make(chan struct{}, 16)} }

func (s *stubDriver) BulkIncrement(ctx context.Context, collection string, ops []db.IncrementOp, upsert bool) error {
	tenant, _ := snoozetypes.TenantFrom(ctx)
	s.mu.Lock()
	s.calls = append(s.calls, bulkCall{collection: collection, ops: cloneOps(ops), upsert: upsert, tenant: tenant})
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
func (s *stubDriver) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
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
func (s *stubDriver) Watcher() syncer.Bus { return nil }
func (s *stubDriver) Close() error        { return nil }

func TestWriter_MergesDeltas(t *testing.T) {
	d := newStubDriver()
	clock := NewMockClock(time.Unix(0, 0))
	w := New(d, time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- w.Run(ctx) }()

	// Enqueue increments before advancing.
	w.Increment(context.Background(), "snooze", "hits", db.Document{"name": "rule 01"}, 1)
	w.Increment(context.Background(), "snooze", "hits", db.Document{"name": "rule 01"}, 2)
	w.Increment(context.Background(), "snooze", "hits", db.Document{"name": "rule 02"}, 5)
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

// TestWriter_PartitionsFlushByTenant reproduces the cross-tenant data-loss bug:
// when two tenants increment the SAME collection within one flush period, the
// flusher must issue a separate, tenant-scoped BulkIncrement per tenant. If it
// instead lumps both tenants' ops into a single BulkIncrement under only the
// first tenant's context, the downstream driver fences the other tenant's op
// against the wrong tenant_id and silently drops it.
func TestWriter_PartitionsFlushByTenant(t *testing.T) {
	d := newStubDriver()
	clock := NewMockClock(time.Unix(0, 0))
	w := New(d, time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- w.Run(ctx) }()

	ctxAlpha := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxBeta := snoozetypes.WithTenant(context.Background(), "beta")

	// Same collection, same search, two different tenants, one flush window.
	w.Increment(ctxAlpha, "stats", "count", db.Document{"name": "hits"}, 5)
	w.Increment(ctxBeta, "stats", "count", db.Document{"name": "hits"}, 9)
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.buckets["stats"]) == 2
	}, time.Second, time.Millisecond)
	clock.Advance(time.Second)

	select {
	case <-d.signal:
	case <-time.After(time.Second):
		t.Fatal("flush did not fire")
	}
	// Wait until both per-tenant BulkIncrements have landed.
	require.Eventually(t, func() bool {
		return len(d.Snapshot()) == 2
	}, time.Second, time.Millisecond)

	cancel()
	require.NoError(t, <-doneCh)

	// Every op must be carried under a ctx scoped to the tenant baked into its
	// own search doc — never another tenant's. Collect (tenant -> delta).
	gotByTenant := map[string]int64{}
	for _, call := range d.Snapshot() {
		require.Equal(t, "stats", call.collection)
		for _, op := range call.ops {
			baked, _ := op.Search["tenant_id"].(string)
			require.NotEmpty(t, call.tenant, "BulkIncrement issued with no tenant scope")
			require.Equal(t, baked, call.tenant,
				"op baked tenant_id %q flushed under ctx tenant %q (cross-tenant leak)", baked, call.tenant)
			gotByTenant[call.tenant] += op.Deltas["count"]
		}
	}
	require.Equal(t, map[string]int64{"alpha": 5, "beta": 9}, gotByTenant)
}

func TestWriter_TenantPartitionedSearch(t *testing.T) {
	// Two tenants enqueue increments for the same logical (collection, field,
	// search). The writer must partition them by tenant_id so the two increments
	// land in distinct buckets and are not coalesced.
	d := newStubDriver()
	clock := NewMockClock(time.Unix(0, 0))
	w := New(d, time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- w.Run(ctx) }()

	ctxA := auth.WithTenant(context.Background(), "acme")
	ctxB := auth.WithTenant(context.Background(), "beta")

	w.Increment(ctxA, "stats", "value", db.Document{"metric": "alert_hit", "dim": "source", "key": "syslog", "bucket": int64(0)}, 1)
	w.Increment(ctxB, "stats", "value", db.Document{"metric": "alert_hit", "dim": "source", "key": "syslog", "bucket": int64(0)}, 1)

	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.buckets["stats"]) == 2
	}, time.Second, time.Millisecond)
	clock.Advance(time.Second)

	select {
	case <-d.signal:
	case <-time.After(time.Second):
		t.Fatal("flush did not fire")
	}

	// The flusher partitions each collection's ops by their baked-in tenant_id
	// and issues one tenant-scoped BulkIncrement per tenant (see
	// TestWriter_PartitionsFlushByTenant and the cross-tenant data-loss fix), so
	// two tenants yield two single-op calls rather than one two-op call. Wait for
	// both to land, then assert exactly two ops total, one per tenant, each
	// carrying its own tenant_id in the search doc.
	require.Eventually(t, func() bool {
		return len(d.Snapshot()) == 2
	}, time.Second, time.Millisecond)

	tenants := map[string]bool{}
	ops := 0
	for _, call := range d.Snapshot() {
		for _, op := range call.ops {
			ops++
			tid, ok := op.Search["tenant_id"].(string)
			require.True(t, ok, "tenant_id must be present in search")
			tenants[tid] = true
		}
	}
	require.Equal(t, 2, ops, "the two tenants' increments must not coalesce")
	require.Equal(t, map[string]bool{"acme": true, "beta": true}, tenants)

	cancel()
	require.NoError(t, <-doneCh)
}

func TestWriter_DrainOnShutdown(t *testing.T) {
	d := newStubDriver()
	clock := NewMockClock(time.Unix(0, 0))
	w := New(d, 10*time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- w.Run(ctx) }()

	w.Increment(context.Background(), "snooze", "hits", db.Document{"name": "rule"}, 7)
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
