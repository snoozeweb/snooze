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

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// batchBucket is the per-key accumulator. A bucket is created the first
// time a queueable record arrives for its key and deleted when it flushes;
// the next queue call recreates it.
type batchBucket struct {
	cfg    Config
	bodies [][]byte
	timer  *time.Timer
}

// queueForBatch appends body to the bucket for cfg.BatchKey, starting (or
// re-using) a flush timer. When the bucket reaches cfg.BatchMaxsize the
// flush is triggered synchronously from this call site; otherwise the
// timer will fire it.
func (p *Plugin) queueForBatch(cfg Config, body []byte) {
	p.bMu.Lock()
	if p.buckets == nil {
		p.buckets = make(map[string]*batchBucket)
	}
	b, ok := p.buckets[cfg.BatchKey]
	if !ok {
		b = &batchBucket{cfg: cfg}
		p.buckets[cfg.BatchKey] = b
		key := cfg.BatchKey
		b.timer = time.AfterFunc(cfg.BatchTimer, func() {
			p.flushBucket(key, "timer")
		})
	}
	b.bodies = append(b.bodies, body)
	full := len(b.bodies) >= cfg.BatchMaxsize
	count := len(b.bodies)
	p.bMu.Unlock()
	if lg := p.logger(); lg != nil {
		lg.Debug("webhook: queued for batch",
			"key", cfg.BatchKey, "count", count, "maxsize", cfg.BatchMaxsize, "timer", cfg.BatchTimer)
	}
	if full {
		p.flushBucket(cfg.BatchKey, "size")
	}
}

// flushBucket atomically removes the bucket from p.buckets and delivers
// its contents as one `[obj1, obj2, ...]` HTTP POST. Safe to call from
// either the size-trigger goroutine or the timer goroutine — whichever
// arrives first wins; the loser sees an empty bucket and returns.
//
// The HTTP call happens after the lock is released so concurrent appends
// to a *different* key are not blocked on this flush's network latency.
func (p *Plugin) flushBucket(key, reason string) {
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
	p.bMu.Unlock()

	if len(bodies) == 0 {
		return
	}

	payload := joinJSONArray(bodies)
	ctx := context.Background()
	if lg := p.logger(); lg != nil {
		lg.Info("webhook: flushing batch",
			"key", key, "reason", reason, "count", len(bodies), "bytes", len(payload))
	}
	if err := p.deliver(ctx, b.cfg, payload, "application/json", snoozetypes.Record{}); err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("webhook: batch flush failed",
				"key", key, "reason", reason, "count", len(bodies), "err", err)
		}
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
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop flushes every pending bucket before returning. Called by the
// runtime on graceful shutdown; ctx is honoured for the per-bucket
// delivery timeout via the underlying deliver call.
func (p *Plugin) Stop(_ context.Context) error {
	p.bMu.Lock()
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
			p.flushBucket(key, "shutdown")
		}(k)
	}
	wg.Wait()
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
