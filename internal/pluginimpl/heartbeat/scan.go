package heartbeat

import (
	"context"
	"fmt"
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
		if !p.isOverdue(hb, now) {
			continue
		}
		if !p.markFired(tenant, hb.Name, hb.LastSeenRaw) {
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
				lg.Warn("heartbeat: pipeline rejected miss alert", "tenant", tenant, "name", hb.Name, "err", perr)
			}
			// Leave the fired mark in place: ProcessRecord is best-effort and
			// re-firing every tick on a persistent pipeline error would be
			// noisier than a single dropped alert.
		}
	}
	return nil
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
// pinged eventually fires.
func (p *Plugin) isOverdue(hb heartbeat, now time.Time) bool {
	deadline := hb.LastSeen.Add(hb.window())
	return now.After(deadline)
}

// firedKey composes the dedup key. NUL separates the parts so a tenant id and a
// name can never alias across the boundary.
func firedKey(tenant, name string) string { return tenant + "\x00" + name }

// markFired records that a miss for (tenant, name, lastSeen) has been fired and
// reports whether this call did the recording (true) or is a duplicate within
// the same window (false).
func (p *Plugin) markFired(tenant, name, lastSeen string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := firedKey(tenant, name)
	if p.fired[k] == lastSeen {
		return false
	}
	p.fired[k] = lastSeen
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
