package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/housekeeper"
)

// recordGaugeDesc is the metric descriptor for snooze_records. It is package-level
// so tests can build a collector directly without going through RegisterRecordGauge.
var recordGaugeDesc = prometheus.NewDesc(
	"snooze_records",
	"Current number of records in the database, grouped by state. Refreshed on every Prometheus scrape.",
	[]string{"state"},
	nil,
)

// recordGaugeStates is the bounded vocabulary of record states the gauge reports.
// Empty-state records normalise to "open" (matching reduceInGo in the stats
// plugin); the open bucket query is OR(state="open", state=""). Keeping this list
// fixed bounds the metric's label cardinality.
var recordGaugeStates = []string{"open", "ack", "close"}

// recordGaugeScanCap mirrors reduceInGo's page size: a defensive upper bound on
// the per-tenant fallback scan when a backend cannot report a cheap count.
const recordGaugeScanCap = 10000

// RecordGaugeCollector is a custom prometheus.Collector that, on every scrape,
// queries the database and emits a snooze_records gauge per record state, summed
// across all tenants. It carries no per-tenant label to keep cardinality bounded.
//
// Counts are gathered at scrape time, so there is no background goroutine and no
// stale snapshot. A short timeout caps how long a slow database may block the
// scrape; on any error the offending tenant is skipped (logged at Warn) and the
// scrape proceeds with whatever was counted.
type RecordGaugeCollector struct {
	desc    *prometheus.Desc
	drv     db.Driver
	lg      *slog.Logger
	timeout time.Duration
}

// RegisterRecordGauge builds a RecordGaugeCollector over drv and registers it on
// reg. The Registry struct gains no field — registration is fire-and-forget. lg
// may be nil to silence per-tenant error logging.
//
// It is called from main.go after core.New, once the driver exists (NewRegistry
// runs before the database is open).
func (*Registry) RegisterRecordGauge(reg prometheus.Registerer, drv db.Driver, lg *slog.Logger) {
	if reg == nil {
		return
	}
	reg.MustRegister(&RecordGaugeCollector{
		desc:    recordGaugeDesc,
		drv:     drv,
		lg:      lg,
		timeout: 5 * time.Second,
	})
}

// Describe implements prometheus.Collector.
func (c *RecordGaugeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect implements prometheus.Collector. It scopes a 5s timeout, sums record
// counts per state across every active tenant (or one implicit tenant on a
// single-tenant deploy), and emits one gauge sample per state.
func (c *RecordGaugeCollector) Collect(ch chan<- prometheus.Metric) {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	totals := map[string]float64{}
	for _, st := range recordGaugeStates {
		totals[st] = 0 // an all-zero family is emitted on an empty DB (not on error)
	}

	// counted tracks whether at least one tenant's records were read without
	// error. On a total failure (every path errored) we emit nothing, so a
	// transient DB outage does not flap the gauge to zero.
	counted := false
	visited := 0
	err := housekeeper.ForEachTenant(ctx, c.drv, func(tenantCtx context.Context, _ string) error {
		visited++
		if c.accumulate(tenantCtx, totals) {
			counted = true
		}
		return nil
	})
	switch {
	case err != nil:
		// Tenant enumeration failed (e.g. driver error). Treat the platform as a
		// single implicit tenant rather than emitting nothing.
		c.warn("record gauge: tenant enumeration failed; falling back to platform scope", err)
		if c.accumulate(auth.WithPlatformScope(ctx), totals) {
			counted = true
		}
	case visited == 0:
		// No tenant registry / no tenants: single-tenant deploy. Count once under
		// platform scope, treated as one implicit tenant.
		if c.accumulate(auth.WithPlatformScope(ctx), totals) {
			counted = true
		}
	}

	if !counted {
		return
	}
	for st, n := range totals {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, n, st)
	}
}

// accumulate adds one tenant's per-state record counts into totals. The
// RecordAggregator fast path is used when the driver implements it; otherwise a
// per-state Search reads the cheap count return value. It reports whether at
// least one count was read without error (so the caller can distinguish an
// empty database from a failed scrape).
func (c *RecordGaugeCollector) accumulate(ctx context.Context, totals map[string]float64) bool {
	if agg, ok := c.drv.(db.RecordAggregator); ok {
		// Fast path: one SQL GROUP BY over all states. from=Unix(0,0) means
		// "all time"; the backend's window is inclusive of epoch 0, so live
		// records are counted. Empty state is already normalised to "open".
		stats, err := agg.RecordStats(ctx, time.Unix(0, 0), time.Now(), 1)
		if err != nil {
			c.warn("record gauge: RecordStats failed", err)
			return false
		}
		for st, n := range stats.ByState {
			totals[st] += float64(n)
		}
		return true
	}

	ok := false
	for _, st := range recordGaugeStates {
		cond := condition.Equals("state", st)
		if st == "open" {
			// Empty-state records normalise to "open" (matching reduceInGo).
			cond = condition.Or(condition.Equals("state", "open"), condition.Equals("state", ""))
		}
		_, total, err := c.drv.Search(ctx, "record", cond, db.Page{PerPage: 0})
		if err != nil {
			c.warn("record gauge: search failed for state "+st, err)
			continue
		}
		// All three backends return a real count(*) here; -1 would mean "cannot
		// cheaply compute". Guard defensively and fall back to the (capped) scan
		// length if a future backend cannot count cheaply.
		if total < 0 {
			docs, _, scanErr := c.drv.Search(ctx, "record", cond, db.Page{PerPage: recordGaugeScanCap})
			if scanErr != nil {
				c.warn("record gauge: fallback scan failed for state "+st, scanErr)
				continue
			}
			total = len(docs)
		}
		totals[st] += float64(total)
		ok = true
	}
	return ok
}

func (c *RecordGaugeCollector) warn(msg string, err error) {
	if c.lg != nil {
		c.lg.Warn(msg, slog.Any("error", err))
	}
}
