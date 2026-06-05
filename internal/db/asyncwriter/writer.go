// Package asyncwriter batches database mutations and flushes them on a fixed
// cadence. Modelled after src/snooze/db/database.py's AsyncIncrement /
// AsyncDatabase pair.
package asyncwriter

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

type incRequest struct {
	collection string
	field      string
	search     db.Document
	delta      int64
	ctx        context.Context // carries tenant slug for partitioned coalescing
}

// Writer batches increment mutations and flushes them periodically. Pass
// requests via Increment; the goroutine launched by Run drains the queue,
// merges by (collection, search, field), and emits one BulkIncrement per
// collection per flush.
type Writer struct {
	d      db.Driver
	period time.Duration
	clock  Clock
	upsert bool

	mu       sync.Mutex
	buckets  map[string]map[string]*aggEntry // collection → hashKey → entry
	closing  chan struct{}
	closed   chan struct{}
	requests chan incRequest
}

type aggEntry struct {
	search db.Document
	deltas map[string]int64
}

// Option configures Writer construction.
type Option func(*Writer)

// WithUpsert flips the BulkIncrement upsert flag for all flushes from this writer.
func WithUpsert(v bool) Option { return func(w *Writer) { w.upsert = v } }

// New returns a Writer. The caller is responsible for invoking Run in a
// goroutine and cancelling its context to trigger a final flush.
func New(d db.Driver, period time.Duration, clock Clock, opts ...Option) *Writer {
	if clock == nil {
		clock = SystemClock{}
	}
	w := &Writer{
		d:        d,
		period:   period,
		clock:    clock,
		buckets:  map[string]map[string]*aggEntry{},
		closing:  make(chan struct{}),
		closed:   make(chan struct{}),
		requests: make(chan incRequest, 1024),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Increment queues a single (collection, search, field, delta) update.
// ctx must carry the tenant (via snoozetypes.WithTenant) for tenant-partitioned
// coalescing and for the downstream BulkIncrement call. Returns immediately;
// merges happen inside the flusher.
func (w *Writer) Increment(ctx context.Context, collection, field string, search db.Document, delta int64) {
	select {
	case <-w.closing:
		return
	case w.requests <- incRequest{
		collection: collection,
		field:      field,
		search:     cloneDocWithTenant(ctx, search),
		delta:      delta,
		ctx:        ctx,
	}:
	}
}

// Run drains requests and flushes on the configured period. Returns after a
// final flush triggered by ctx.Done().
func (w *Writer) Run(ctx context.Context) error {
	defer close(w.closed)
	for {
		select {
		case <-ctx.Done():
			close(w.closing)
			w.drain()
			if err := w.flush(context.Background()); err != nil {
				return fmt.Errorf("asyncwriter: final flush: %w", err)
			}
			return nil
		case r := <-w.requests:
			w.accept(r)
		case <-w.clock.After(w.period):
			if err := w.flush(ctx); err != nil {
				return fmt.Errorf("asyncwriter: flush: %w", err)
			}
		}
	}
}

func (w *Writer) drain() {
	for {
		select {
		case r := <-w.requests:
			w.accept(r)
		default:
			return
		}
	}
}

func (w *Writer) accept(r incRequest) {
	w.mu.Lock()
	defer w.mu.Unlock()
	bucket, ok := w.buckets[r.collection]
	if !ok {
		bucket = map[string]*aggEntry{}
		w.buckets[r.collection] = bucket
	}
	key := hashSearch(r.search)
	entry, ok := bucket[key]
	if !ok {
		entry = &aggEntry{search: cloneDoc(r.search), deltas: map[string]int64{}}
		bucket[key] = entry
	}
	entry.deltas[r.field] += r.delta
}

// flush drains the pending buckets to the driver. A single collection bucket can
// hold entries for MULTIPLE tenants (the Writer is a process-wide singleton, and
// cloneDocWithTenant bakes each request's tenant_id into its own search doc), so
// ops must be partitioned by their per-op tenant before being handed to the
// driver. BulkIncrement folds the ctx tenant onto every op it receives, so a
// single call may only ever carry ops belonging to one tenant — otherwise a
// non-matching tenant's op resolves to `tenant_id="X" AND tenant_id="Y"` and is
// silently dropped (or, with upsert, mis-stamped). We therefore issue one
// BulkIncrement per (tenant, collection), each under a ctx scoped to that
// tenant (or platform scope when an entry carries no tenant_id). The caller's
// ctx is intentionally unused.
func (w *Writer) flush(_ context.Context) error {
	w.mu.Lock()
	if len(w.buckets) == 0 {
		w.mu.Unlock()
		return nil
	}
	pending := w.buckets
	w.buckets = map[string]map[string]*aggEntry{}
	w.mu.Unlock()
	for collection, entries := range pending {
		// Partition this collection's entries by the tenant baked into each
		// search doc. "" is the platform/global bucket (no tenant_id present).
		opsByTenant := map[string][]db.IncrementOp{}
		for _, e := range entries {
			// Skip zero-net updates: matches Python's `if value > 0` short-circuit,
			// generalised to any non-zero delta (we accept negative deltas too).
			hasNonZero := false
			for _, v := range e.deltas {
				if v != 0 {
					hasNonZero = true
					break
				}
			}
			if !hasNonZero {
				continue
			}
			tenant, _ := e.search["tenant_id"].(string)
			opsByTenant[tenant] = append(opsByTenant[tenant], db.IncrementOp{Search: e.search, Deltas: e.deltas})
		}
		for tenant, ops := range opsByTenant {
			if len(ops) == 0 {
				continue
			}
			// No tenant baked into the search docs (global collection or platform
			// scope at enqueue time): flush under platform scope so BulkIncrement
			// does not fail closed. Otherwise scope to the partition's tenant so
			// BulkIncrement's own TenantScope fences each op against the right tenant.
			var flushCtx context.Context
			if tenant == "" {
				flushCtx = snoozetypes.WithPlatformScope(context.Background())
			} else {
				flushCtx = snoozetypes.WithTenant(context.Background(), tenant)
			}
			if err := w.d.BulkIncrement(flushCtx, collection, ops, w.upsert); err != nil {
				return err
			}
		}
	}
	return nil
}

// Flush drains queued increments to the driver synchronously. Primarily a test
// seam; the Run loop flushes on its own cadence in production.
func (w *Writer) Flush(ctx context.Context) error {
	w.drain()
	return w.flush(ctx)
}

// hashSearch returns a stable hash key for a search dict. Keys are sorted,
// values are stringified — collisions are vanishingly unlikely for the
// typical {name:str} or {key:str} searches that Snooze uses.
func hashSearch(d db.Document) string {
	if len(d) == 0 {
		return ""
	}
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for i, k := range keys {
		if i > 0 {
			b = append(b, '\x00')
		}
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, fmt.Sprintf("%v", d[k])...)
	}
	return string(b)
}

func cloneDoc(d db.Document) db.Document {
	out := make(db.Document, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

// cloneDocWithTenant clones the search doc and bakes tenant_id into it (when ctx
// carries a tenant) so hashSearch partitions coalescing by tenant automatically
// and the downstream BulkIncrement filter is tenant-scoped.
func cloneDocWithTenant(ctx context.Context, d db.Document) db.Document {
	out := cloneDoc(d)
	if t, ok := snoozetypes.TenantFrom(ctx); ok && t != "" {
		out["tenant_id"] = t
	}
	return out
}
