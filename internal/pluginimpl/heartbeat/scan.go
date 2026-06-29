package heartbeat

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ScanTenant reads the calling tenant's enabled heartbeats and, for each one
// silent longer than interval+grace, injects exactly one miss alert into that
// tenant's pipeline (deduped per (tenant,name)). It is driven once per active
// tenant per tick by the core heartbeat job. The tenant is taken from ctx.
func (p *Plugin) ScanTenant(ctx context.Context) error {
	driver := p.db()
	if driver == nil {
		return nil
	}
	tenant, ok := snoozetypes.TenantFrom(ctx)
	if !ok {
		// Called without a tenant in context — a programmer error. Fail closed:
		// skip rather than fire alerts under an empty-tenant dedup key.
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: ScanTenant called without a tenant in context; skipping")
		}
		return nil
	}

	// enabled defaults to true, so fetch all and apply the rule in Go.
	docs, _, err := driver.Search(ctx, collection, condition.Cond{}, db.Page{})
	if err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: scan search failed", "tenant", tenant, "err", err)
		}
		return err
	}

	proc := p.recordProcessor()
	now := p.now().UTC()

	for _, doc := range docs {
		hb, ok := parseHeartbeat(doc)
		if !ok || !hb.Enabled {
			continue
		}
		// Overdue takes priority over slow: the two are mutually exclusive. An
		// overdue heartbeat fires the (full-severity) miss; a within-window
		// heartbeat whose last ping was too slow fires the downgraded slow
		// alert. isSlow already excludes the overdue case, but the explicit
		// guard keeps the priority obvious at the call site.
		switch {
		case p.isOverdue(hb, now):
			if !p.markFiredKey(firedKey(tenant, hb.Name), hb.LastSeenRaw) {
				continue
			}
			p.dispatch(ctx, proc, tenant, hb.Name, buildMissRecord(hb, now), "miss")
		case isSlow(hb, now):
			// The slow signal is deduped under its own NUL-delimited key so a
			// later miss for the same window can still fire independently. The
			// sentinel combines last_seen and last_latency: a fresh ping
			// changes both and re-arms the slow alert.
			if !p.markFiredKey(slowFiredKey(tenant, hb.Name), slowSentinel(hb)) {
				continue
			}
			p.dispatch(ctx, proc, tenant, hb.Name, buildSlowRecord(hb, now), "slow")
		}
	}
	return nil
}

// dispatch injects rec into the pipeline, handling the no-processor case once
// and logging a best-effort pipeline rejection without clearing the fired mark.
func (p *Plugin) dispatch(ctx context.Context, proc recordProcessor, tenant, name string, rec snoozetypes.Record, kind string) {
	if proc == nil {
		if !p.warnNoProcessorOnce() {
			if lg := p.logger(); lg != nil {
				lg.Warn("heartbeat: host has no recordProcessor; alert is a no-op",
					"name", name, "kind", kind)
			}
		}
		return
	}
	if _, _, perr := proc.ProcessRecord(ctx, rec); perr != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: pipeline rejected alert", "tenant", tenant, "name", name, "kind", kind, "err", perr)
		}
		// Leave the fired mark in place: ProcessRecord is best-effort and
		// re-firing every tick on a persistent pipeline error would be noisier
		// than a single dropped alert.
	}
}

// ScanInterval is the per-tenant scan cadence the core heartbeat job ticks on.
func (p *Plugin) ScanInterval() time.Duration {
	if p.interval <= 0 {
		return defaultScanInterval
	}
	return p.interval
}

// isOverdue reports whether the heartbeat has been silent for longer than
// interval+grace as of now. A heartbeat that has never been pinged (no
// last_seen) is considered overdue once interval+grace has no anchor — we treat
// a missing last_seen as "overdue" so a freshly created heartbeat that is never
// pinged eventually fires. It delegates to the package-level overdue predicate
// so the scanner and the read-time computeStatus share one implementation.
func (p *Plugin) isOverdue(hb heartbeat, now time.Time) bool {
	return overdue(hb, now)
}

// firedKey composes the miss dedup key. NUL separates the parts so a tenant id
// and a name can never alias across the boundary.
func firedKey(tenant, name string) string { return tenant + "\x00" + name }

// slowFiredKey composes the slow-signal dedup key. It appends a NUL-delimited
// "slow" suffix to the miss key so the slow and miss fires for the same
// (tenant,name) get independent fired entries. NUL delimiting (not a literal
// ":slow") makes the key collision-proof even for a heartbeat whose literal
// name contains "slow" or ":slow".
func slowFiredKey(tenant, name string) string { return firedKey(tenant, name) + "\x00slow" }

// slowSentinel is the dedup sentinel for the slow signal. It combines last_seen
// and last_latency so a fresh ping — which rewrites both — re-arms the slow
// alert, while repeated scans of the same ping dedup.
func slowSentinel(hb heartbeat) string {
	return hb.LastSeenRaw + "\x00" + strconv.FormatInt(hb.LastLatency, 10)
}

// markFiredKey records that an alert for the given dedup key has been fired with
// the given sentinel, and reports whether this call did the recording (true) or
// is a duplicate within the same window (false).
func (p *Plugin) markFiredKey(key, sentinel string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fired[key] == sentinel {
		return false
	}
	p.fired[key] = sentinel
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

// buildSlowRecord constructs the early-warning alert injected when a heartbeat's
// most recent ping latency exceeded its max_latency while still within its
// window. It mirrors buildMissRecord but downgrades the severity one tier and
// uses a distinct message, and carries last_latency/max_latency in Raw.
func buildSlowRecord(hb heartbeat, now time.Time) snoozetypes.Record {
	severity := hb.Severity
	if severity == "" {
		severity = defaultSeverity
	}
	severity = downgradeSeverity(severity)

	host := hb.Host
	if host == "" {
		host = hb.Name
	}

	message := fmt.Sprintf("heartbeat %s slow (latency %dms > max %dms)",
		hb.Name, hb.LastLatency, hb.MaxLatency)

	raw := map[string]any{
		"name":         hb.Name,
		"interval":     hb.Interval,
		"grace":        hb.Grace,
		"last_seen":    hb.LastSeenRaw,
		"last_latency": hb.LastLatency,
		"max_latency":  hb.MaxLatency,
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

// downgradeSeverity maps the miss severity down one tier so a slow alert is
// distinct from a true miss without a second severity field on the document.
// Any severity outside the known ladder falls back to "warning" (documented in
// the heartbeat integration docs).
func downgradeSeverity(s string) string {
	switch s {
	case "critical":
		return "major"
	case "major":
		return "minor"
	case "minor":
		return "warning"
	default:
		return "warning"
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
