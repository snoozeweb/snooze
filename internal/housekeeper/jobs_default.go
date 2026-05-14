package housekeeper

import (
	"context"
	"fmt"
	"time"

	"github.com/japannext/snooze/internal/db"
)

// CleanupTimeoutJob deletes records past their TTL on the named collection
// every 5 minutes. Mirrors the Python `cleanup_alert` job (record-only).
func CleanupTimeoutJob(d db.Driver, collection string) IntervalJob {
	name := fmt.Sprintf("cleanup_timeout/%s", collection)
	return IntervalJob{
		Interval: 5 * time.Minute,
		Job: NewJobFunc(name, func(ctx context.Context) error {
			_, err := d.CleanupTimeout(ctx, collection)
			return err
		}),
	}
}

// CleanupAggregateJob drops the `aggregate` collection every minute, matching
// the Python `cleanup_aggregate` semantics (the collection is recomputed
// continuously by the aggregate plugin).
func CleanupAggregateJob(d db.Driver) IntervalJob {
	return IntervalJob{
		Interval: time.Minute,
		Job: NewJobFunc("cleanup_aggregate", func(ctx context.Context) error {
			return d.Drop(ctx, "aggregate")
		}),
	}
}

// CleanupCommentsJob removes orphaned comments daily.
func CleanupCommentsJob(d db.Driver) IntervalJob {
	return IntervalJob{
		Interval: 24 * time.Hour,
		Job: NewJobFunc("cleanup_comments", func(ctx context.Context) error {
			_, err := d.CleanupComments(ctx)
			return err
		}),
	}
}

// CleanupOrphansJob removes orphaned rows from the named collection daily.
func CleanupOrphansJob(d db.Driver, collection string) IntervalJob {
	name := fmt.Sprintf("cleanup_orphans/%s", collection)
	return IntervalJob{
		Interval: 24 * time.Hour,
		Job: NewJobFunc(name, func(ctx context.Context) error {
			_, err := d.CleanupOrphans(ctx, collection)
			return err
		}),
	}
}

// RenumberJob renumbers the integer-typed `field` on the named collection
// every day at midnight, ensuring a dense ordering.
func RenumberJob(d db.Driver, collection, field string) CronJob {
	name := fmt.Sprintf("renumber/%s/%s", collection, field)
	return CronJob{
		Cron: "0 0 * * *",
		Job: NewJobFunc(name, func(ctx context.Context) error {
			return d.RenumberField(ctx, collection, field)
		}),
	}
}

// CleanupAuditJob purges audit-log rows older than `olderThan` every day at
// midnight.
func CleanupAuditJob(d db.Driver, olderThan time.Duration) CronJob {
	return CronJob{
		Cron: "0 0 * * *",
		Job: NewJobFunc("cleanup_audit", func(ctx context.Context) error {
			_, err := d.CleanupAuditLogs(ctx, olderThan)
			return err
		}),
	}
}
