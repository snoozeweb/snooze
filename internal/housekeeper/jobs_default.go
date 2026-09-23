package housekeeper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

// lifecycleTimeouts is the narrow contract EscalateTimeoutJob reads the live
// ack-expiry and auto-escalation windows from. Declared locally (like
// auditRetention) to avoid importing the config package and its import cycle.
// Satisfied by *config.RuntimeSettings.
type lifecycleTimeouts interface {
	AckTimeout(ctx context.Context) time.Duration
	EscalateAfter(ctx context.Context) time.Duration
}

// recordCollection is the alert collection the escalate-timeout sweep operates
// on. commentCollection holds the timeline auto-comments it writes.
const (
	recordCollection  = "record"
	commentCollection = "comment"
)

// EscalateTimeoutJob enforces the server-controlled alert lifecycle every
// minute, per active tenant. It is REVERT-not-delete (do not confuse with
// CleanupTimeoutJob, which deletes TTL-expired rows). Two passes run inside the
// per-tenant context from ForEachTenant so the driver injects tenant_id —
// Search/UpdateOne are NEVER called under platform scope:
//
//  1. Expired-ack revert: every record with state=="ack" and
//     0 < ack_until <= now reverts to "open", ack_until clears to 0, and
//     escalate_at is (re-)armed when escalate_after>0 so the reopened alert can
//     escalate later. An auto open-comment records the revert on the timeline.
//  2. Escalate overdue (only when escalate_after>0): every record with
//     state=="open" and 0 < escalate_at <= now flips to "esc", escalate_at
//     clears to 0 (escalation is one-shot until a new transition re-arms it), an
//     auto esc-comment is written, and the injected notify callback re-fires the
//     notification dispatcher with the now-escalated record. notify errors are
//     logged, not fatal.
//
// Both passes also clear the record's owner in the same patch
// (internal/ownership.Clear, computed from the row the pass read): the alert
// is back to needing somebody, and the old owner stays as the previous-owner
// ghost. The unshelve sweep below leaves ownership alone.
//
// rs is read live on every fire (operators can retune the windows without a
// restart); a tenant whose settings are unreadable falls back to defaults via
// the accessor contract rather than failing the whole sweep. clk supplies the
// deadline reference time — no time.Now() in this core path.
func EscalateTimeoutJob(d db.Driver, clk Clock, rs lifecycleTimeouts, notify func(ctx context.Context, rec snoozetypes.Record) error) IntervalJob {
	return IntervalJob{
		Interval: time.Minute,
		Job: NewJobFunc("escalate_timeout", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				now := clk.Now().Unix()
				escalateAfter := time.Duration(0)
				if rs != nil {
					escalateAfter = rs.EscalateAfter(tctx)
				}
				if err := revertExpiredAcks(tctx, d, now, escalateAfter); err != nil {
					return err
				}
				return escalateOverdueOpens(tctx, d, now, escalateAfter, notify)
			})
		}),
	}
}

// expiredDeadlineCond builds the `state==s AND 0 < field <= now` predicate the
// sweep reuses for both passes.
func expiredDeadlineCond(state, field string, now int64) condition.Cond {
	return condition.And(
		condition.Equals("state", state),
		condition.Cond{Op: condition.OpLte, Field: field, Value: now},
		condition.Cond{Op: condition.OpGt, Field: field, Value: int64(0)},
	)
}

// revertExpiredAcks performs pass 1. Must run under a tenant-scoped tctx.
func revertExpiredAcks(tctx context.Context, d db.Driver, now int64, escalateAfter time.Duration) error {
	docs, _, err := d.Search(tctx, recordCollection, expiredDeadlineCond("ack", "ack_until", now), db.Page{})
	if err != nil {
		return fmt.Errorf("housekeeper: escalate_timeout: search expired acks: %w", err)
	}
	for _, doc := range docs {
		uid, _ := doc["uid"].(string)
		if uid == "" {
			continue
		}
		patch := db.Document{"state": "open", "ack_until": int64(0)}
		if escalateAfter > 0 {
			patch["escalate_at"] = now + int64(escalateAfter.Seconds())
		} else {
			patch["escalate_at"] = int64(0)
		}
		// An expired ack means nobody followed up: the owner is dropped (kept
		// as the previous-owner ghost), computed from the row just read.
		for k, v := range ownership.Clear(doc) {
			patch[k] = v
		}
		if err := d.UpdateOne(tctx, recordCollection, uid, patch, true); err != nil {
			return fmt.Errorf("housekeeper: escalate_timeout: revert ack %s: %w", uid, err)
		}
		writeLifecycleComment(tctx, d, uid, "open", "Acknowledgement expired — reverted to open", now)
	}
	return nil
}

// escalateOverdueOpens performs pass 2. A strict no-op when escalateAfter<=0:
// no Search, no writes, no notifies. Must run under a tenant-scoped tctx.
func escalateOverdueOpens(tctx context.Context, d db.Driver, now int64, escalateAfter time.Duration, notify func(ctx context.Context, rec snoozetypes.Record) error) error {
	if escalateAfter <= 0 {
		return nil
	}
	docs, _, err := d.Search(tctx, recordCollection, expiredDeadlineCond("open", "escalate_at", now), db.Page{})
	if err != nil {
		return fmt.Errorf("housekeeper: escalate_timeout: search overdue opens: %w", err)
	}
	for _, doc := range docs {
		uid, _ := doc["uid"].(string)
		if uid == "" {
			continue
		}
		// One-shot: clear escalate_at so the same record is not re-escalated
		// until a new transition re-arms it.
		//
		// The escalation counter is bumped in the same patch so the notifiers
		// this sweep is about to fire can tell a re-escalation from a first
		// delivery (and update the existing ticket / thread instead of opening
		// a second one). It is derived from the document we just read rather
		// than via an atomic increment because the same value has to reach the
		// in-memory record handed to notify below — the sweep is the only
		// writer of this field for a given record in a given pass, and it runs
		// single-threaded per tenant.
		count := int(docInt64(doc, "escalation_count")) + 1
		patch := db.Document{
			"state":             "esc",
			"escalate_at":       int64(0),
			"escalation_count":  count,
			"escalated_at":      now,
			"escalation_reason": escalationReasonTimeout,
		}
		// Escalating an unhandled alert drops its owner, like every other
		// automatic re-escalation. Kept separately so the in-memory record
		// below carries the same clear as the stored row.
		cleared := ownership.Clear(doc)
		for k, v := range cleared {
			patch[k] = v
		}
		if err := d.UpdateOne(tctx, recordCollection, uid, patch, true); err != nil {
			return fmt.Errorf("housekeeper: escalate_timeout: escalate open %s: %w", uid, err)
		}
		writeLifecycleComment(tctx, d, uid, "esc", "Auto-escalated: unacknowledged past deadline", now)
		if notify == nil {
			continue
		}
		rec := recordFromDoc(doc)
		rec.UID = uid
		rec.State = "esc"
		rec.EscalationCount = count
		rec.EscalatedAt = now
		rec.EscalationReason = escalationReasonTimeout
		// The ownership keys are untyped, so they live in Extra.
		if rec.Extra == nil {
			rec.Extra = map[string]any{}
		}
		for k, v := range cleared {
			rec.Extra[k] = v
		}
		if nerr := notify(tctx, rec); nerr != nil {
			// Best-effort: a re-notification failure must not abort the sweep.
			slog.Default().Warn("housekeeper: escalate_timeout: re-notify failed", "uid", uid, "err", nerr)
		}
	}
	return nil
}

// writeLifecycleComment appends an auto lifecycle comment to the timeline and
// bumps the record's comment_count, mirroring aggregaterule.queueAutoComment.
// The write goes straight to the driver (the comment plugin's AfterCreate is
// bypassed) so the counter bump is done here explicitly. Best-effort: a failed
// timeline write must never abort the sweep.
func writeLifecycleComment(tctx context.Context, d db.Driver, recordUID, ctype, message string, now int64) {
	if d == nil || recordUID == "" {
		return
	}
	doc := db.Document{
		"record_uid": recordUID,
		"type":       ctype,
		"message":    message,
		"date_epoch": now,
		"auto":       true,
	}
	if _, err := d.Write(tctx, commentCollection, []db.Document{doc}, db.WriteOptions{UpdateTime: true}); err != nil {
		slog.Default().Warn("housekeeper: escalate_timeout: write auto comment", "uid", recordUID, "type", ctype, "err", err)
		return
	}
	if _, err := d.IncMany(tctx, recordCollection, "comment_count", condition.Equals("uid", recordUID), 1); err != nil {
		slog.Default().Warn("housekeeper: escalate_timeout: bump comment_count", "uid", recordUID, "err", err)
	}
}

// escalationReasonTimeout is the escalation_reason this sweep stamps. The
// other producers use "manual" (an operator's state→esc comment) and
// "watchlist" (aggregaterule's field-change auto-re-escalation); notifiers and
// ticket comments surface the value so an operator can see why they were paged
// again.
const escalationReasonTimeout = "timeout"

// docInt64 reads a counter out of a record document, tolerating the int /
// int64 / float64 shapes the Mongo and SQLite drivers decode numbers into.
func docInt64(doc db.Document, key string) int64 {
	switch v := doc[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return 0
	}
}

// recordFromDoc projects the loose record document the sweep read back into the
// typed Record the notification dispatcher consumes.
//
// Delegates to snoozetypes.RecordFromDocument so the UNTYPED fields survive.
// This function used to be a bare JSON round-trip, and because Record.Extra is
// `json:"-"` that silently dropped every one of them — including the
// notify_ref_<action> handle a notifier uses to find the ticket it already
// opened. The ack-timeout sweep is the most common escalation there is, so that
// made JIRA open a second ticket on exactly the path that matters most.
func recordFromDoc(doc db.Document) snoozetypes.Record {
	rec, err := snoozetypes.RecordFromDocument(doc)
	if err != nil {
		return snoozetypes.Record{}
	}
	return rec
}

// UnshelveTimeoutJob auto-returns time-boxed shelves every minute, per active
// tenant. Like EscalateTimeoutJob it is REVERT-not-delete. Inside the
// per-tenant context from ForEachTenant (so the driver injects tenant_id —
// Search/UpdateOne/Write/IncMany are NEVER called under platform scope) it
// reverts every record with state=="shelved" and 0 < shelve_until <= now back
// to "open": shelve_until clears to 0, an auto unshelve-comment records the
// revert on the timeline, and comment_count is bumped.
//
// The legacy permanent shelve (shelve_until==0) is deliberately excluded by the
// `shelve_until > 0` guard in expiredShelveQuery, so those alerts are never
// auto-unshelved. clk supplies the deadline reference time — no time.Now() in
// this core path. The deadline is the epoch already stamped on the record, so
// the job needs no timeout config at runtime.
func UnshelveTimeoutJob(d db.Driver, clk Clock) IntervalJob {
	return IntervalJob{
		Interval: time.Minute,
		Job: NewJobFunc("unshelve_timeout", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				now := clk.Now().Unix()
				return revertExpiredShelves(tctx, d, now)
			})
		}),
	}
}

// expiredShelveQuery builds the `state==state AND 0 < shelve_until <= now`
// predicate the unshelve sweep matches on. The `shelve_until > 0` lower bound
// excludes the legacy permanent shelve (shelve_until==0). Delegates to the
// shared expiredDeadlineCond used by the escalate sweep.
func expiredShelveQuery(state string, now int64) condition.Cond {
	return expiredDeadlineCond(state, "shelve_until", now)
}

// revertExpiredShelves reverts every shelved record past its deadline. Must run
// under a tenant-scoped tctx.
func revertExpiredShelves(tctx context.Context, d db.Driver, now int64) error {
	docs, _, err := d.Search(tctx, recordCollection, expiredShelveQuery("shelved", now), db.Page{})
	if err != nil {
		return fmt.Errorf("housekeeper: unshelve_timeout: search expired shelves: %w", err)
	}
	for _, doc := range docs {
		uid, _ := doc["uid"].(string)
		if uid == "" {
			continue
		}
		patch := db.Document{"state": "open", "shelve_until": int64(0)}
		if err := d.UpdateOne(tctx, recordCollection, uid, patch, true); err != nil {
			return fmt.Errorf("housekeeper: unshelve_timeout: revert shelve %s: %w", uid, err)
		}
		writeUnshelveAutoComment(tctx, d, uid, now)
	}
	return nil
}

// writeUnshelveAutoComment appends an auto unshelve comment to the timeline and
// bumps the record's comment_count. Adapted from writeLifecycleComment /
// aggregaterule.queueAutoComment: the write goes straight to the driver (the
// comment plugin's AfterCreate is bypassed) so the counter bump is explicit.
// Best-effort: a failed timeline write must never abort the sweep.
func writeUnshelveAutoComment(tctx context.Context, d db.Driver, recordUID string, now int64) {
	if d == nil || recordUID == "" {
		return
	}
	doc := db.Document{
		"record_uid": recordUID,
		"type":       "unshelve",
		"message":    "Shelve expired — reverted to open",
		"date_epoch": now,
		"auto":       true,
	}
	if _, err := d.Write(tctx, commentCollection, []db.Document{doc}, db.WriteOptions{UpdateTime: true}); err != nil {
		slog.Default().Warn("housekeeper: unshelve_timeout: write auto comment", "uid", recordUID, "err", err)
		return
	}
	if _, err := d.IncMany(tctx, recordCollection, "comment_count", condition.Equals("uid", recordUID), 1); err != nil {
		slog.Default().Warn("housekeeper: unshelve_timeout: bump comment_count", "uid", recordUID, "err", err)
	}
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

// ReconcileSuppressionJob runs the suppression owner's reconcile (the snooze
// plugin's plugins.SuppressionOwner.ReconcileSuppression) for every active
// tenant on a fixed minute cadence.
//
// A record's `snoozed` attribution is re-decided on each occurrence, which
// cannot reach a row that never fires again. Without this sweep such a row
// stays hidden for good once its filter can no longer silence anything: a
// time-boxed filter whose window ran out ("snooze for 2h" from the MCP or
// Teams bridges), the one cleanup_snooze above deletes straight in the
// database, or a name a cluster peer re-stamped after an API delete had
// already reconciled. The minute cadence keeps a short snooze ending close to
// on time; the reconcile itself is one filter read and one conditional unset
// per tenant.
//
// One tenant failing does not stop the others; the errors are joined.
func ReconcileSuppressionJob(d db.Driver, reconcile func(ctx context.Context) (int, error)) IntervalJob {
	return IntervalJob{
		Interval: time.Minute,
		Job: NewJobFunc("reconcile_suppression", func(ctx context.Context) error {
			var errs []error
			if err := ForEachTenant(ctx, d, func(tctx context.Context, tid string) error {
				if _, err := reconcile(tctx); err != nil {
					errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
				}
				return nil
			}); err != nil {
				errs = append(errs, err)
			}
			return errors.Join(errs...)
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

// notificationLogRetention is the narrow contract the cleanup_notificationlog
// job needs from the config layer (declared locally to avoid importing
// config, like statsRetention/auditRetention).
type notificationLogRetention interface {
	NotificationLogRetention(ctx context.Context) time.Duration
}

// CleanupNotificationLogAsIntervalJob deletes rows in the `notificationlog`
// collection whose send-completion time (`date_epoch`) is older than the
// operator-configured retention window (default 30d / 720h), read fresh
// from RuntimeSettings on each fire. Daily cadence.
func CleanupNotificationLogAsIntervalJob(d db.Driver, rs notificationLogRetention) IntervalJob {
	return IntervalJob{
		Interval: 24 * time.Hour,
		Job: NewJobFunc("cleanup_notificationlog", func(ctx context.Context) error {
			return ForEachTenant(ctx, d, func(tctx context.Context, _ string) error {
				retention := 720 * time.Hour
				if rs != nil {
					if v := rs.NotificationLogRetention(tctx); v > 0 {
						retention = v
					}
				}
				cutoff := time.Now().Add(-retention).Unix()
				cond := condition.Cond{Op: condition.OpLt, Field: "date_epoch", Value: cutoff}
				_, err := d.Delete(tctx, "notificationlog", cond, true)
				return err
			})
		}),
	}
}

// apikeyCleanup is the narrow contract CleanupAPIKeyJob needs from the auth
// layer. Satisfied by *auth.APIKeyStore. Declared as its own type (separate
// from refreshCleanup, despite the identical signature) to document intent at
// the call site.
type apikeyCleanup interface {
	Cleanup(ctx context.Context) (int, error)
}

// refreshCleanup is the narrow contract CleanupRefreshTokenJob needs from the
// auth layer. Satisfied by *auth.RefreshTokenStore.
type refreshCleanup interface {
	Cleanup(ctx context.Context) (int, error)
}

// CleanupAPIKeyJob purges expired API-key rows hourly. APIKeyStore.Cleanup
// already wraps the call in WithPlatformScope, so this job passes ctx through
// unchanged and sweeps every tenant in one pass.
func CleanupAPIKeyJob(s apikeyCleanup) IntervalJob {
	return IntervalJob{
		Interval: time.Hour,
		Job: NewJobFunc("cleanup_apikey", func(ctx context.Context) error {
			_, err := s.Cleanup(ctx)
			return err
		}),
	}
}

// CleanupRefreshTokenJob purges expired refresh-token rows hourly. Unlike the
// API-key store, RefreshTokenStore.Cleanup forwards ctx to the driver as-is,
// so this job applies WithPlatformScope here to sweep across all tenants.
func CleanupRefreshTokenJob(s refreshCleanup) IntervalJob {
	return IntervalJob{
		Interval: time.Hour,
		Job: NewJobFunc("cleanup_refresh_token", func(ctx context.Context) error {
			_, err := s.Cleanup(auth.WithPlatformScope(ctx))
			return err
		}),
	}
}
