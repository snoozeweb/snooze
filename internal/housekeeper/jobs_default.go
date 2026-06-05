package housekeeper

import (
	"context"
	"fmt"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// CleanupTimeoutJob deletes records past their TTL on the named collection
// every 5 minutes, iterating every active tenant. Mirrors the Python
// `cleanup_alert` job (record-only).
func CleanupTimeoutJob(d db.Driver, collection string) IntervalJob {
	name := fmt.Sprintf("cleanup_timeout/%s", collection)
	return IntervalJob{
		Interval: 5 * time.Minute,
		Job: NewJobFunc(name, func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				_, err := d.CleanupTimeout(tctx, collection)
				return err
			})
		}),
	}
}

// CleanupAggregateJob drops the `aggregate` collection per tenant every minute,
// matching the Python `cleanup_aggregate` semantics (the collection is
// recomputed continuously by the aggregate plugin).
func CleanupAggregateJob(d db.Driver) IntervalJob {
	return IntervalJob{
		Interval: time.Minute,
		Job: NewJobFunc("cleanup_aggregate", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				return d.Drop(tctx, "aggregate")
			})
		}),
	}
}

// CleanupCommentsJob removes orphaned comments daily, per tenant.
func CleanupCommentsJob(d db.Driver) IntervalJob {
	return IntervalJob{
		Interval: 24 * time.Hour,
		Job: NewJobFunc("cleanup_comments", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				_, err := d.CleanupComments(tctx)
				return err
			})
		}),
	}
}

// CleanupOrphansJob removes orphaned rows from the named collection daily, per tenant.
func CleanupOrphansJob(d db.Driver, collection string) IntervalJob {
	name := fmt.Sprintf("cleanup_orphans/%s", collection)
	return IntervalJob{
		Interval: 24 * time.Hour,
		Job: NewJobFunc(name, func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				_, err := d.CleanupOrphans(tctx, collection)
				return err
			})
		}),
	}
}

// CleanupAuditJob purges audit-log rows older than `olderThan` every day at
// midnight, per tenant.
func CleanupAuditJob(d db.Driver, olderThan time.Duration) CronJob {
	return CronJob{
		Cron: "0 0 * * *",
		Job: NewJobFunc("cleanup_audit", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				_, err := d.CleanupAuditLogs(tctx, olderThan)
				return err
			})
		}),
	}
}

// CleanupAuditAsIntervalJob wraps CleanupAuditLogs with an interval cadence
// (default 28 days). Unlike the cron variant this one reads the retention
// window from the supplied RuntimeSettings on every fire, so an operator
// who shortens housekeeping.cleanup_audit in the UI sees the new
// threshold applied to the next purge — no restart needed.
func CleanupAuditAsIntervalJob(d db.Driver, rs auditRetention) IntervalJob {
	return IntervalJob{
		Interval: 28 * 24 * time.Hour,
		Job: NewJobFunc("cleanup_audit", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				retention := 28 * 24 * time.Hour
				if rs != nil {
					if v := rs.AuditRetention(tctx); v > 0 {
						retention = v
					}
				}
				_, err := d.CleanupAuditLogs(tctx, retention)
				return err
			})
		}),
	}
}

// auditRetention is the narrow contract CleanupAuditAsIntervalJob needs from
// the config layer: the current audit-retention window. We declare it
// locally instead of importing config to avoid an internal-package cycle
// (the config package's RuntimeSettings type lives upstream of this one).
type auditRetention interface {
	AuditRetention(ctx context.Context) time.Duration
}

// CleanupSnoozeJob deletes snooze rows whose time-constraint datetime entries
// are all in the past. Matches the Python `cleanup_snooze` semantics
// (cron-driven, daily). The interval argument tunes the cadence; the cron
// expression is hardcoded to the daily-midnight slot to match
// `cleanup_audit`'s pattern.
func CleanupSnoozeJob(d db.Driver) IntervalJob {
	return IntervalJob{
		Interval: 72 * time.Hour,
		Job: NewJobFunc("cleanup_snooze", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				_, err := d.CleanupSnooze(tctx)
				return err
			})
		}),
	}
}

// CleanupNotificationJob mirrors CleanupSnoozeJob for the `notification`
// collection.
func CleanupNotificationJob(d db.Driver) IntervalJob {
	return IntervalJob{
		Interval: 72 * time.Hour,
		Job: NewJobFunc("cleanup_notification", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				_, err := d.CleanupNotification(tctx)
				return err
			})
		}),
	}
}

// statsRetention is the narrow contract the cleanup_stats job needs from the
// config layer (declared locally to avoid importing config, like auditRetention).
type statsRetention interface {
	StatsRetention(ctx context.Context) time.Duration
}

// CleanupStatsAsIntervalJob deletes counter docs in the `stats` collection
// whose hour bucket is older than the operator-configured retention window
// (default 400d), read fresh from RuntimeSettings on each fire. Daily cadence.
func CleanupStatsAsIntervalJob(d db.Driver, rs statsRetention) IntervalJob {
	return IntervalJob{
		Interval: 24 * time.Hour,
		Job: NewJobFunc("cleanup_stats", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				retention := 400 * 24 * time.Hour
				if rs != nil {
					if v := rs.StatsRetention(tctx); v > 0 {
						retention = v
					}
				}
				cutoff := time.Now().Add(-retention).Unix()
				cond := condition.Cond{Op: condition.OpLt, Field: "bucket", Value: cutoff}
				_, err := d.Delete(tctx, "stats", cond, true)
				return err
			})
		}),
	}
}
