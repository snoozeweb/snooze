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
)

type incRequest struct {
	collection string
	field      string
	search     db.Document
	delta      int64
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
// Returns immediately; merges happen inside the flusher.
func (w *Writer) Increment(collection, field string, search db.Document, delta int64) {
	select {
	case <-w.closing:
		return
	case w.requests <- incRequest{collection: collection, field: field, search: search, delta: delta}:
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

func (w *Writer) flush(ctx context.Context) error {
	w.mu.Lock()
	if len(w.buckets) == 0 {
		w.mu.Unlock()
		return nil
	}
	pending := w.buckets
	w.buckets = map[string]map[string]*aggEntry{}
	w.mu.Unlock()
	for collection, entries := range pending {
		ops := make([]db.IncrementOp, 0, len(entries))
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
			ops = append(ops, db.IncrementOp{Search: e.search, Deltas: e.deltas})
		}
		if len(ops) == 0 {
			continue
		}
		if err := w.d.BulkIncrement(ctx, collection, ops, w.upsert); err != nil {
			return err
		}
	}
	return nil
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
