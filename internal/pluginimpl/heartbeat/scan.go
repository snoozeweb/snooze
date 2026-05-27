package heartbeat

import (
	"context"
	"fmt"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ---- LifecycleHook: the scanner -------------------------------------------

// Start launches the single background scanner goroutine. It returns promptly;
// the goroutine ticks on p.interval and stops when Stop cancels it (or the
// supplied context is cancelled). Calling Start twice without an intervening
// Stop is a no-op on the second call (the first goroutine keeps running).
func (p *Plugin) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.cancel != nil {
		// Already running.
		p.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	p.cancel = cancel
	p.done = done
	interval := p.interval
	p.mu.Unlock()

	if lg := p.logger(); lg != nil {
		lg.Info("heartbeat: scanner started", "interval", interval.String())
	}

	go p.loop(runCtx, done, interval)
	return nil
}

// Stop cancels the scanner and blocks until the goroutine has exited. It is
// safe to call when Start was never called, and safe to call more than once.
func (p *Plugin) Stop(_ context.Context) error {
	p.mu.Lock()
	cancel := p.cancel
	done := p.done
	p.cancel = nil
	p.done = nil
	p.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	if done != nil {
		<-done
	}
	if lg := p.logger(); lg != nil {
		lg.Info("heartbeat: scanner stopped")
	}
	return nil
}

// loop is the scanner goroutine body. It ticks on a time.Ticker and calls scan
// once per tick until its context is cancelled. It never busy-loops and returns
// promptly on cancellation by closing done.
func (p *Plugin) loop(ctx context.Context, done chan struct{}, interval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.scan(ctx)
		}
	}
}

// scan reads the enabled heartbeats and, for each one that has been silent
// longer than its interval+grace, injects exactly one miss alert (deduped via
// the fired set). It is the unit the tests drive directly with a fake clock.
func (p *Plugin) scan(ctx context.Context) {
	driver := p.db()
	if driver == nil {
		return
	}

	// enabled defaults to true, so we cannot simply filter `enabled = true`
	// at the DB layer (a document that omits the field would be excluded).
	// Fetch all heartbeats and apply the enabled rule in Go.
	docs, _, err := driver.Search(ctx, collection, condition.Cond{}, db.Page{})
	if err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: scan search failed", "err", err)
		}
		return
	}

	proc := p.recordProcessor()
	now := p.now().UTC()

	for _, doc := range docs {
		hb, ok := parseHeartbeat(doc)
		if !ok || !hb.Enabled {
			continue
		}
		if !p.isOverdue(hb, now) {
			continue
		}
		// Dedup: fire once per (name, last_seen) window.
		if !p.markFired(hb.Name, hb.LastSeenRaw) {
			continue
		}
		rec := buildMissRecord(hb, now)
		if proc == nil {
			if !p.warnNoProcessorOnce() {
				if lg := p.logger(); lg != nil {
					lg.Warn("heartbeat: host has no recordProcessor; miss alert is a no-op",
						"name", hb.Name)
				}
			}
			continue
		}
		if _, _, perr := proc.ProcessRecord(ctx, rec); perr != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("heartbeat: pipeline rejected miss alert", "name", hb.Name, "err", perr)
			}
			// Leave the fired mark in place: ProcessRecord is best-effort and
			// re-firing every tick on a persistent pipeline error would be
			// noisier than a single dropped alert.
		}
	}
}

// isOverdue reports whether the heartbeat has been silent for longer than
// interval+grace as of now. A heartbeat that has never been pinged (no
// last_seen) is considered overdue once interval+grace has no anchor — we treat
// a missing last_seen as "overdue" so a freshly created heartbeat that is never
// pinged eventually fires.
func (p *Plugin) isOverdue(hb heartbeat, now time.Time) bool {
	deadline := hb.LastSeen.Add(hb.window())
	return now.After(deadline)
}

// markFired records that a miss for (name, lastSeen) has been fired and reports
// whether this call is the one that did the recording (true) or a duplicate
// within the same window (false).
func (p *Plugin) markFired(name, lastSeen string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fired[name] == lastSeen {
		return false
	}
	p.fired[name] = lastSeen
	return true
}

// warnNoProcessorOnce reports whether the no-processor warning has already been
// logged, and marks it logged. Returns the prior value.
func (p *Plugin) warnNoProcessorOnce() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	prior := p.warnedNoProcessor
	p.warnedNoProcessor = true
	return prior
}

// buildMissRecord constructs the alert injected for a missed heartbeat.
func buildMissRecord(hb heartbeat, now time.Time) snoozetypes.Record {
	severity := hb.Severity
	if severity == "" {
		severity = defaultSeverity
	}
	host := hb.Host
	if host == "" {
		host = hb.Name
	}

	lastSeenLabel := hb.LastSeenRaw
	if lastSeenLabel == "" {
		lastSeenLabel = "never"
	}

	message := hb.renderMessage(now, lastSeenLabel)

	raw := map[string]any{
		"name":      hb.Name,
		"interval":  hb.Interval,
		"grace":     hb.Grace,
		"last_seen": lastSeenLabel,
	}
	if hb.Environment != "" {
		raw["environment"] = hb.Environment
	}

	return snoozetypes.Record{
		Source:      "heartbeat",
		Host:        host,
		Process:     hb.Name,
		Severity:    severity,
		Message:     message,
		Environment: hb.Environment,
		Timestamp:   now,
		Raw:         raw,
	}
}

// renderMessage produces the human message for a miss. If the heartbeat carries
// a custom message it is used verbatim (callers may template it themselves
// before storing). Otherwise the default phrasing is used.
func (hb heartbeat) renderMessage(_ time.Time, lastSeenLabel string) string {
	if hb.Message != "" {
		return hb.Message
	}
	return fmt.Sprintf("heartbeat %s missed (last seen %s)", hb.Name, lastSeenLabel)
}
