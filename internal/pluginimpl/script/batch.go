// Batching for the script notifier. Actions tagged `batch: true` accumulate
// per-record state (rendered argv, stdin, cwd, env) in per-action buckets
// and flush them as one process invocation. The flush uses the first
// record's argv/cwd/env (subsequent records' variants are ignored — they
// can't be merged into a single argv), with stdin set to:
//
//   - a JSON array of the queued stdins when each parses as JSON (the
//     typical case when the stdin template is `{{ .RecordJSON }}`); or
//   - the newline-joined concatenation of stdins otherwise.
//
// A bucket flushes on the first of:
//
//   - batch_maxsize records queued, or
//   - batch_timer elapsed since the first record was queued.
//
// Both bounds must be positive (see parseConfig); a degenerate config falls
// back to immediate dispatch so we never silently buffer forever. The flush
// is fire-and-forget from Send's caller — errors are logged via the host
// logger but never propagate back to the notification dispatcher, which has
// already returned by the time the bucket flushes. This mirrors the webhook
// and mail plugins' batch.go.
package script

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// batchBucket is the per-key accumulator. Created the first time a queueable
// record arrives for its key and deleted when it flushes; the next queue
// call recreates it.
type batchBucket struct {
	cfg    scriptConfig
	argv   []string          // from the first queued record
	cwd    string            // from the first queued record
	env    map[string]string // from the first queued record
	stdins []string
	timer  *time.Timer
}

// queueForBatch appends per-record state to the bucket for cfg.batchKey,
// starting (or re-using) a flush timer. When the bucket reaches
// cfg.batchMaxsize the flush is triggered synchronously from this call
// site; otherwise the timer will fire it.
func (p *Plugin) queueForBatch(cfg scriptConfig, argv []string, stdinData, cwd string, env map[string]string) {
	p.bMu.Lock()
	if p.buckets == nil {
		p.buckets = make(map[string]*batchBucket)
	}
	b, ok := p.buckets[cfg.batchKey]
	if !ok {
		b = &batchBucket{cfg: cfg, argv: argv, cwd: cwd, env: env}
		p.buckets[cfg.batchKey] = b
		key := cfg.batchKey
		b.timer = time.AfterFunc(cfg.batchTimer, func() {
			p.flushBucket(key, "timer")
		})
	}
	b.stdins = append(b.stdins, stdinData)
	count := len(b.stdins)
	full := count >= cfg.batchMaxsize
	p.bMu.Unlock()
	if lg := p.logger(); lg != nil {
		lg.Debug("script: queued for batch",
			"key", cfg.batchKey, "count", count, "maxsize", cfg.batchMaxsize, "timer", cfg.batchTimer)
	}
	if full {
		p.flushBucket(cfg.batchKey, "size")
	}
}

// flushBucket atomically removes the bucket from p.buckets and runs the
// command once with the joined stdin. Safe to call from either the
// size-trigger or the timer goroutine — whichever arrives first wins; the
// loser sees an empty bucket and returns.
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
	stdins := b.stdins
	cfg := b.cfg
	argv := b.argv
	cwd := b.cwd
	env := b.env
	p.bMu.Unlock()

	if len(stdins) == 0 {
		return
	}

	joined := joinStdins(stdins)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	if lg := p.logger(); lg != nil {
		lg.Info("script: flushing batch",
			"key", key, "reason", reason, "count", len(stdins), "bytes", len(joined))
	}
	if err := p.runCommand(ctx, cfg, argv, joined, cwd, env); err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("script: batch flush failed",
				"key", key, "reason", reason, "count", len(stdins), "err", err)
		}
	}
}

// joinStdins returns the per-record stdins as a single string. If every
// entry parses as JSON, the result is a JSON array (`[s1,s2,…]`) so that
// scripts written for `stdin: {{ .RecordJSON }}` receive a coherent array
// of records. Otherwise we fall back to newline-joining — the lowest
// common denominator for line-oriented scripts.
func joinStdins(stdins []string) string {
	allJSON := true
	for _, s := range stdins {
		if !json.Valid([]byte(strings.TrimSpace(s))) {
			allJSON = false
			break
		}
	}
	if !allJSON {
		return strings.Join(stdins, "\n")
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, s := range stdins {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(strings.TrimSpace(s))
	}
	buf.WriteByte(']')
	return buf.String()
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
