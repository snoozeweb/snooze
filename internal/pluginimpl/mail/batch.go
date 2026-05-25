// Batching for the mail notifier. Actions tagged `batch: true` accumulate
// rendered subject+body pairs in per-action buckets and flush them as one
// SMTP message whose body is the joined per-record bodies (with a separator)
// and whose subject is the first queued record's subject. A bucket flushes
// on the first of:
//
//   - batch_maxsize records queued, or
//   - batch_timer elapsed since the first record was queued.
//
// Both bounds must be positive (see parseConfig); a degenerate config falls
// back to immediate dispatch so we never silently buffer forever. The flush
// is fire-and-forget from Send's caller — errors are logged via the host
// logger but never propagate back to the notification dispatcher, which has
// already returned by the time the bucket flushes. This mirrors the webhook
// plugin's batch.go.
package mail

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// batchSeparator delimits per-record bodies inside the flushed email. Kept
// distinctive enough that operators can grep for it in delivered mail.
const batchSeparator = "\n\n--- next alert ---\n\n"

// batchBucket is the per-key accumulator. Created the first time a queueable
// record arrives for its key and deleted when it flushes; the next queue
// call recreates it.
type batchBucket struct {
	cfg     smtpConfig
	subject string
	bodies  []string
	timer   *time.Timer
}

// queueForBatch appends body to the bucket for cfg.batchKey, starting (or
// re-using) a flush timer. When the bucket reaches cfg.batchMaxsize the
// flush is triggered synchronously from this call site; otherwise the
// timer will fire it.
func (p *Plugin) queueForBatch(cfg smtpConfig, subject, body string) {
	p.bMu.Lock()
	if p.buckets == nil {
		p.buckets = make(map[string]*batchBucket)
	}
	b, ok := p.buckets[cfg.batchKey]
	if !ok {
		b = &batchBucket{cfg: cfg, subject: subject}
		p.buckets[cfg.batchKey] = b
		key := cfg.batchKey
		b.timer = time.AfterFunc(cfg.batchTimer, func() {
			p.flushBucket(key, "timer")
		})
	}
	b.bodies = append(b.bodies, body)
	count := len(b.bodies)
	full := count >= cfg.batchMaxsize
	p.bMu.Unlock()
	if lg := p.logger(); lg != nil {
		lg.Debug("mail: queued for batch",
			"key", cfg.batchKey, "count", count, "maxsize", cfg.batchMaxsize, "timer", cfg.batchTimer)
	}
	if full {
		p.flushBucket(cfg.batchKey, "size")
	}
}

// flushBucket atomically removes the bucket from p.buckets and sends its
// contents as one SMTP message. Safe to call from either the size-trigger or
// the timer goroutine — whichever arrives first wins; the loser sees an
// empty bucket and returns.
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
	subject := b.subject
	cfg := b.cfg
	p.bMu.Unlock()

	if len(bodies) == 0 {
		return
	}

	to := splitAddrs(cfg.to)
	cc := splitAddrs(cfg.cc)
	bcc := splitAddrs(cfg.bcc)
	if len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		if lg := p.logger(); lg != nil {
			lg.Warn("mail: batch flush dropped (no recipients)",
				"key", key, "reason", reason, "count", len(bodies))
		}
		return
	}

	// Decorate the subject so operators can see the batch size at a glance
	// when the rendered subject is generic.
	if len(bodies) > 1 {
		subject = formatBatchSubject(subject, len(bodies))
	}
	body := strings.Join(bodies, batchSeparator)
	msg := buildMessage(cfg, to, cc, subject, body)
	rcpts := append(append(append([]string{}, to...), cc...), bcc...)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	if lg := p.logger(); lg != nil {
		lg.Info("mail: flushing batch",
			"key", key, "reason", reason, "count", len(bodies), "bytes", len(body))
	}
	if err := p.deliver(ctx, cfg, rcpts, msg); err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("mail: batch flush failed",
				"key", key, "reason", reason, "count", len(bodies), "err", err)
		}
	}
}

// formatBatchSubject prefixes the rendered subject with the batch size so
// the recipient sees how many alerts the message represents.
func formatBatchSubject(base string, count int) string {
	if base == "" {
		return "[" + itoa(count) + "] alert batch"
	}
	return "[" + itoa(count) + "] " + base
}

// itoa is a tiny strconv.Itoa avoiding the import — kept inline so batch.go
// doesn't pick up strconv just for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Start is part of the plugins.LifecycleHook contract. Buckets are lazy, so
// there is no goroutine to launch up-front. The hook is here so the runtime
// calls Stop on shutdown to drain any in-flight batches.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop flushes every pending bucket before returning. Called by the runtime
// on graceful shutdown.
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

// logger returns the host's slog.Logger, or a default when the plugin was
// instantiated without a host (test paths).
func (p *Plugin) logger() *slog.Logger {
	if p.host == nil {
		return slog.Default()
	}
	return p.host.Logger()
}
