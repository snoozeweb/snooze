// Batching for the webhook notifier. Actions tagged `batch: true` accumulate
// rendered bodies in per-(URL, action_name) buckets and flush them as a
// single `[obj1, obj2, ...]` JSON array. A bucket flushes on the first of:
//
//   - batch_maxsize records queued, or
//   - batch_timer elapsed since the first record was queued.
//
// Both bounds must be positive (see configFromPayload); a degenerate config
// falls back to immediate dispatch so we never silently buffer forever.
//
// The flush is fire-and-forget from Send's caller — errors are logged via
// the host logger but never propagate back to the notification dispatcher,
// which has already returned by the time the bucket flushes. This mirrors
// the Python plugin's behaviour.

package webhook

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// batchBucket is the per-key accumulator. A bucket is created the first
// time a queueable record arrives for its key and deleted when it flushes;
// the next queue call recreates it.
type batchBucket struct {
	cfg    Config
	bodies [][]byte
	timer  *time.Timer

	// members is the delivery-history snapshot of every alert in the bucket,
	// one per queued body and in the same order. The flush reports the whole
	// bucket as a single delivery row (contract D8: Send returned ErrBatched,
	// so nobody else has logged these alerts).
	members []plugins.DeliveryMember
	// firstQueued dates the row's queued_epoch: when the *first* member was
	// accepted, which is when the bucket (and its timer) started.
	firstQueued time.Time
}

// queueForBatch appends body to the bucket for cfg.BatchKey, starting (or
// re-using) a flush timer. When the bucket reaches cfg.BatchMaxsize the
// flush is triggered synchronously from this call site; otherwise the
// timer will fire it.
//
// It returns false when Stop has already run: the alert is neither appended nor
// used to create a bucket, because Stop has taken its snapshot of the keys and
// nothing will drain a bucket created after it. The caller must then deliver
// immediately instead of reporting ErrBatched.
func (p *Plugin) queueForBatch(cfg Config, body []byte, member plugins.DeliveryMember) bool {
	p.bMu.Lock()
	if p.stopped {
		p.bMu.Unlock()
		return false
	}
	if p.buckets == nil {
		p.buckets = make(map[string]*batchBucket)
	}
	b, ok := p.buckets[cfg.BatchKey]
	if !ok {
		b = &batchBucket{cfg: cfg, firstQueued: time.Now()}
		p.buckets[cfg.BatchKey] = b
		key := cfg.BatchKey
		b.timer = time.AfterFunc(cfg.BatchTimer, func() {
			// The timer fires long after Send's request context died, so the
			// flush starts from a fresh background context and re-derives its
			// scope (tenant) and its deadlines itself.
			p.flushBucket(context.Background(), key, plugins.BatchReasonTimer)
		})
	}
	b.bodies = append(b.bodies, body)
	b.members = append(b.members, member)
	full := len(b.bodies) >= cfg.BatchMaxsize
	count := len(b.bodies)
	p.bMu.Unlock()
	if lg := p.logger(); lg != nil {
		lg.Debug("webhook: queued for batch",
			"key", cfg.BatchKey, "count", count, "maxsize", cfg.BatchMaxsize, "timer", cfg.BatchTimer)
	}
	if full {
		p.flushBucket(context.Background(), cfg.BatchKey, plugins.BatchReasonSize)
	}
	return true
}

// flushBucket atomically removes the bucket from p.buckets and delivers
// its contents as one `[obj1, obj2, ...]` HTTP POST. Safe to call from
// either the size-trigger goroutine or the timer goroutine — whichever
// arrives first wins; the loser sees an empty bucket and returns.
//
// base is the context the flush hangs off: context.Background() for the size
// and timer triggers (whose request context is long gone) and Stop's own ctx
// for the shutdown drain, so a bounded shutdown really is bounded.
//
// The HTTP call happens after the lock is released so concurrent appends
// to a *different* key are not blocked on this flush's network latency.
func (p *Plugin) flushBucket(base context.Context, key, reason string) {
	p.bMu.Lock()
	b, ok := p.buckets[key]
	if !ok {
		p.bMu.Unlock()
		return
	}
	delete(p.buckets, key)
	if b.timer != nil {
		b.timer.Stop()
	}
	bodies := b.bodies
	members := b.members
	queued := b.firstQueued
	cfg := b.cfg
	p.bMu.Unlock()

	if len(bodies) == 0 {
		return
	}

	payload := joinJSONArray(bodies)
	// The flush goroutine has no request context, so the tenant is re-stamped
	// from the members: everything downstream (the inject closure, the uid
	// backfill read, the delivery-row write, the counter bump) is tenant-scoped
	// and the driver fails closed on a naked context.
	ctx := base
	if tenant := plugins.MembersTenant(members); tenant != "" {
		ctx = auth.WithTenant(ctx, tenant)
	}

	if lg := p.logger(); lg != nil {
		lg.Info("webhook: flushing batch",
			"key", key, "reason", reason, "count", len(bodies), "bytes", len(payload))
	}
	start := time.Now()
	err := p.deliver(ctx, cfg, payload, "application/json", snoozetypes.Record{}, plugins.NotificationPayload{})
	elapsed := time.Since(start)

	status := plugins.DeliveryStatusSuccess
	errText, metric := "", "action_success"
	if err != nil {
		status = plugins.DeliveryStatusError
		errText, metric = err.Error(), "action_error"
		if lg := p.logger(); lg != nil {
			lg.Warn("webhook: batch flush failed",
				"key", key, "reason", reason, "count", len(bodies), "err", err)
		}
	}

	// One stat increment PER MEMBER, not per flush. Before delivery history
	// existed, Send recorded action_success at queue time — once per alert — so
	// counting the members here keeps the per-alert cardinality operators'
	// dashboards are built on. The event epoch is the bucket's first-queued
	// time, which is the hour bucket the old queue-time stat landed in.
	plugins.RecordStat(ctx, p.host, queued.Unix(), metric,
		map[string]string{"name": cfg.ActionName}, int64(len(members)))

	// Everything below touches the DB from a detached goroutine: bound it so a
	// wedged driver cannot pin this goroutine (or Stop) indefinitely.
	dbCtx, cancel := context.WithTimeout(ctx, plugins.DeliveryWriteTimeout)
	defer cancel()
	plugins.BackfillMemberUIDs(dbCtx, p.host, members)
	plugins.RecordDelivery(dbCtx, p.host, plugins.DeliveryRow{
		CompletedAt: time.Now(),
		QueuedAt:    queued,
		Duration:    elapsed,
		Status:      status,
		Error:       errText,
		Action:      cfg.ActionName,
		Notifier:    "webhook",
		Batch:       true,
		BatchReason: reason,
		Members:     members,
	})
	if err == nil {
		// The dispatcher only bumps the counters for sends it saw complete;
		// a batched send completes here, so this is the only place a batching
		// action's notification can advance `hits` / `last_sent`.
		plugins.BumpNotificationCounters(dbCtx, p.host, members)
	}
}

// joinJSONArray returns `[bodies[0], bodies[1], …]` as a single buffer.
// Each body is assumed to already be valid JSON — the caller checks via
// bodyIsJSON before queueing.
func joinJSONArray(bodies [][]byte) []byte {
	if len(bodies) == 0 {
		return []byte("[]")
	}
	var buf bytes.Buffer
	buf.Grow(len(bodies) * 64) // rough lower bound; saves a few reallocs
	buf.WriteByte('[')
	for i, body := range bodies {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(bytes.TrimSpace(body))
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// Start is part of the plugins.LifecycleHook contract. The batch buckets
// are lazy — they only exist while at least one record is pending — so
// there is no goroutine to launch up-front. The hook is here so the
// runtime calls Stop on shutdown to drain any in-flight batches.
//
// It re-opens the door Stop closed: `stopped` is what makes a post-shutdown
// Send fall through to immediate delivery, so leaving it set would make a
// Stop → Start cycle (a config reload that re-registers the plugin, or any
// test that restarts it) silently disable batching for the rest of the
// process — every batched action degrading to one send per alert.
func (p *Plugin) Start(_ context.Context) error {
	p.bMu.Lock()
	p.stopped = false
	p.bMu.Unlock()
	return nil
}

// Stop flushes every pending bucket before returning. Called by the
// runtime on graceful shutdown.
//
// ctx is the parent of every drain, so a caller that gives Stop a deadline
// gets one: the HTTP delivery and the per-flush DB work both inherit it, and
// Stop stops waiting when it expires instead of blocking on a wedged bucket.
// The drain goroutines are left to unwind on their own — a shutdown that is
// already over its budget must not be held up further.
func (p *Plugin) Stop(ctx context.Context) error {
	p.bMu.Lock()
	// Close the door before snapshotting: a Send that queued between the
	// snapshot and the drain would leave an orphan bucket behind.
	p.stopped = true
	keys := make([]string, 0, len(p.buckets))
	for k := range p.buckets {
		keys = append(keys, k)
	}
	p.bMu.Unlock()

	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			p.flushBucket(ctx, key, plugins.BatchReasonShutdown)
		}(k)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		if lg := p.logger(); lg != nil {
			lg.Warn("webhook: batch drain did not finish before the shutdown deadline",
				"pending", len(keys), "err", ctx.Err())
		}
	}
	return nil
}

// logger returns the host's slog.Logger, or a default when the plugin
// was instantiated without a host (test paths). Mirrors the helper in
// notification/plugin.go.
func (p *Plugin) logger() *slog.Logger {
	if p.host == nil {
		return slog.Default()
	}
	return p.host.Logger()
}
