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

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/plugins"
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

	// members is the delivery-history snapshot of every alert in the bucket,
	// one per queued body and in the same order. The flush reports the whole
	// bucket as a single delivery row (contract D8: Send returned ErrBatched,
	// so nobody else has logged these alerts).
	members []plugins.DeliveryMember
	// firstQueued dates the row's queued_epoch: when the *first* member was
	// accepted, which is when the bucket (and its timer) started.
	firstQueued time.Time
}

// queueForBatch appends body to the bucket for cfg.batchKey, starting (or
// re-using) a flush timer. When the bucket reaches cfg.batchMaxsize the
// flush is triggered synchronously from this call site; otherwise the
// timer will fire it.
//
// It returns false when Stop has already run: the alert is neither appended nor
// used to create a bucket, because Stop has taken its snapshot of the keys and
// nothing will drain a bucket created after it. The caller must then send the
// email immediately instead of reporting ErrBatched.
func (p *Plugin) queueForBatch(cfg smtpConfig, subject, body string, member plugins.DeliveryMember) bool {
	p.bMu.Lock()
	if p.stopped {
		p.bMu.Unlock()
		return false
	}
	if p.buckets == nil {
		p.buckets = make(map[string]*batchBucket)
	}
	b, ok := p.buckets[cfg.batchKey]
	if !ok {
		b = &batchBucket{cfg: cfg, subject: subject, firstQueued: time.Now()}
		p.buckets[cfg.batchKey] = b
		key := cfg.batchKey
		b.timer = time.AfterFunc(cfg.batchTimer, func() {
			// The timer fires long after Send's request context died, so the
			// flush starts from a fresh background context and re-derives its
			// scope (tenant) and its deadlines itself.
			p.flushBucket(context.Background(), key, plugins.BatchReasonTimer)
		})
	}
	b.bodies = append(b.bodies, body)
	b.members = append(b.members, member)
	count := len(b.bodies)
	full := count >= cfg.batchMaxsize
	p.bMu.Unlock()
	if lg := p.logger(); lg != nil {
		lg.Debug("mail: queued for batch",
			"key", cfg.batchKey, "count", count, "maxsize", cfg.batchMaxsize, "timer", cfg.batchTimer)
	}
	if full {
		p.flushBucket(context.Background(), cfg.batchKey, plugins.BatchReasonSize)
	}
	return true
}

// flushBucket atomically removes the bucket from p.buckets and sends its
// contents as one SMTP message. Safe to call from either the size-trigger or
// the timer goroutine — whichever arrives first wins; the loser sees an
// empty bucket and returns.
//
// base is the context the flush hangs off: context.Background() for the size
// and timer triggers (whose request context is long gone) and Stop's own ctx
// for the shutdown drain, so a bounded shutdown really is bounded.
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
	subject := b.subject
	cfg := b.cfg
	members := b.members
	queued := b.firstQueued
	p.bMu.Unlock()

	if len(bodies) == 0 {
		return
	}

	// The flush goroutine has no request context, so the tenant is re-stamped
	// from the members: the uid backfill read, the delivery-row write and the
	// counter bump are all tenant-scoped and the driver fails closed on a naked
	// context.
	if tenant := plugins.MembersTenant(members); tenant != "" {
		base = auth.WithTenant(base, tenant)
	}

	to := splitAddrs(cfg.to)
	cc := splitAddrs(cfg.cc)
	bcc := splitAddrs(cfg.bcc)
	if len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		if lg := p.logger(); lg != nil {
			lg.Warn("mail: batch flush dropped (no recipients)",
				"key", key, "reason", reason, "count", len(bodies))
		}
		// The alerts were accepted and never delivered, which is exactly the
		// question the delivery history exists to answer — log the drop as a
		// failed row rather than letting it vanish into the server log. The
		// counters stay untouched: nothing was sent.
		plugins.RecordStat(base, p.host, queued.Unix(), "action_error",
			map[string]string{"name": cfg.actionName}, int64(len(members)))
		dropCtx, cancelDrop := context.WithTimeout(base, plugins.DeliveryWriteTimeout)
		defer cancelDrop()
		// Backfill first: the row is the only trace these alerts leave, so it
		// must carry the uids the UI links on, exactly like a delivered batch.
		plugins.BackfillMemberUIDs(dropCtx, p.host, members)
		plugins.RecordDelivery(dropCtx, p.host, plugins.DeliveryRow{
			CompletedAt: time.Now(),
			QueuedAt:    queued,
			Status:      plugins.DeliveryStatusError,
			Error:       "mail: no recipients (to/cc/bcc are all empty)",
			Action:      cfg.actionName,
			Notifier:    "mail",
			Batch:       true,
			BatchReason: reason,
			Members:     members,
		})
		return
	}

	// Decorate the subject so operators can see the batch size at a glance
	// when the rendered subject is generic.
	if len(bodies) > 1 {
		subject = formatBatchSubject(subject, len(bodies))
	}
	body := strings.Join(bodies, batchSeparator)
	// A batched send coalesces several records, so there is no single alert to
	// thread under — the batch goes out as its own message.
	msg := buildMessage(cfg, to, cc, subject, body, mailThread{})
	rcpts := append(append(append([]string{}, to...), cc...), bcc...)

	ctx, cancel := context.WithTimeout(base, cfg.timeout)
	defer cancel()
	if lg := p.logger(); lg != nil {
		lg.Info("mail: flushing batch",
			"key", key, "reason", reason, "count", len(bodies), "bytes", len(body))
	}
	start := time.Now()
	err := p.deliver(ctx, cfg, rcpts, msg)
	elapsed := time.Since(start)

	status := plugins.DeliveryStatusSuccess
	errText, metric := "", "action_success"
	if err != nil {
		status = plugins.DeliveryStatusError
		errText, metric = err.Error(), "action_error"
		if lg := p.logger(); lg != nil {
			lg.Warn("mail: batch flush failed",
				"key", key, "reason", reason, "count", len(bodies), "err", err)
		}
	}

	// One stat increment PER MEMBER, not per flush. Before delivery history
	// existed, Send recorded action_success at queue time — once per alert — so
	// counting the members here keeps the per-alert cardinality operators'
	// dashboards are built on. The event epoch is the bucket's first-queued
	// time, which is the hour bucket the old queue-time stat landed in.
	plugins.RecordStat(base, p.host, queued.Unix(), metric,
		map[string]string{"name": cfg.actionName}, int64(len(members)))

	// Everything below touches the DB from a detached goroutine: bound it so a
	// wedged driver cannot pin this goroutine (or Stop) indefinitely.
	dbCtx, cancelDB := context.WithTimeout(base, plugins.DeliveryWriteTimeout)
	defer cancelDB()
	plugins.BackfillMemberUIDs(dbCtx, p.host, members)
	plugins.RecordDelivery(dbCtx, p.host, plugins.DeliveryRow{
		CompletedAt: time.Now(),
		QueuedAt:    queued,
		Duration:    elapsed,
		Status:      status,
		Error:       errText,
		Action:      cfg.actionName,
		Notifier:    "mail",
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
//
// It re-opens the door Stop closed: `stopped` is what makes a post-shutdown
// Send fall through to immediate delivery, so leaving it set would make a
// Stop → Start cycle silently disable batching for the rest of the process.
func (p *Plugin) Start(_ context.Context) error {
	p.bMu.Lock()
	p.stopped = false
	p.bMu.Unlock()
	return nil
}

// Stop flushes every pending bucket before returning. Called by the runtime
// on graceful shutdown.
//
// ctx is the parent of every drain, so a caller that gives Stop a deadline
// gets one: the SMTP delivery and the per-flush DB work both inherit it, and
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
			lg.Warn("mail: batch drain did not finish before the shutdown deadline",
				"pending", len(keys), "err", ctx.Err())
		}
	}
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
