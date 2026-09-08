package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// NotificationLogCollection is the tenant-scoped collection holding the
// per-send delivery history. One row = one actual send attempt by one action,
// covering 1..N alerts (a batch flush writes a single row listing every
// member). Owned by internal/pluginimpl/notificationlog.
const NotificationLogCollection = "notificationlog"

// deliveryMessageMaxRunes caps the alert message snapshot stored on a delivery
// member. The snapshot exists so a row still renders after the alert's own TTL
// expires; it is not meant to be a second copy of the alert.
const deliveryMessageMaxRunes = 512

// Delivery outcome values written to the row's `status` field. The set is
// deliberately closed: a send either left the box or it did not. Queued-in-a-
// batch is not a status — it is ErrBatched, and the flush writes the row.
const (
	DeliveryStatusSuccess = "success"
	DeliveryStatusError   = "error"
)

// Reasons a batch bucket flushed. Empty on an unbatched row.
const (
	BatchReasonSize  = "size"
	BatchReasonTimer = "timer"
	// BatchReasonShutdown is used when the server drains its pending buckets
	// on graceful shutdown. It is not a configured trigger like the other two
	// — the row records why the bucket flushed, and "the server is going
	// down" is a third answer the UI shows verbatim.
	BatchReasonShutdown = "shutdown"
)

// DeliveryWriteTimeout bounds the DB work a batch flush does after the send
// itself: the uid backfill, the delivery-row write and the counter bump. The
// flush runs on a detached timer goroutine (or on the shutdown drain), so
// without a deadline a wedged driver would pin that goroutine — and Stop —
// forever.
const DeliveryWriteTimeout = 5 * time.Second

// recordCollection is the alert collection BackfillMemberUIDs reads to recover
// the uid a member did not have at queue time.
const recordCollection = "record"

// notificationCollection is the collection holding notification entries, whose
// `hits` / `last_sent` counters BumpNotificationCounters advances.
const notificationCollection = "notification"

// ErrBatched is returned by a Notifier.Send that accepted the alert into a
// batch bucket: the send is deferred and its outcome will be reported by the
// flush, so the dispatcher must neither log a delivery row nor count a
// success/error stat for it. (Contract D8 in the plan.)
//
// The dispatcher tests it with errors.Is and stamps the record's
// `actions[].status` as "sent" — dispatched, outcome not tracked here. The
// batching notifier is then responsible for calling RecordDelivery exactly
// once per flush with every member of the bucket.
var ErrBatched = errors.New("queued for batch delivery")

// DeliveryMember is one alert's snapshot inside a delivery row. Everything the
// UI needs to render the line is copied at send time so the row survives the
// alert's record TTL (2 days by default vs 30 days of delivery history).
//
// Tenant and QueuedAt exist for the batch path: a flush runs on a detached
// timer goroutine with no request context, so it re-stamps the tenant from the
// members and dates the row's `queued_epoch` from the earliest of them.
type DeliveryMember struct {
	// UID is the alert record uid. It may be empty: a first-occurrence record
	// gets its uid assigned by the driver at write time, which happens after
	// the notification dispatcher has already run. Callers resolve it later by
	// hash where they can; the UI falls back to the hash link when they can't.
	UID string
	// Hash is the alert's dedup hash. Always present.
	Hash string
	// Host, Severity, Message and State are the display snapshot.
	Host     string
	Severity string
	Message  string
	State    string
	// Notification is the name of the notification entry that routed this
	// alert to the action, and NotificationUID its uid (the stable key every
	// UI filter uses).
	Notification    string
	NotificationUID string
	// Tenant is the tenant the alert belongs to, used to re-stamp a naked
	// context at batch-flush time.
	Tenant string
	// QueuedAt is when dispatch was decided for this member.
	QueuedAt time.Time
	// Escalation is the re-escalation context of this delivery.
	Escalation Escalation
}

// MemberFromRecord builds a delivery member from the record being dispatched.
// The tenant is read from ctx, the escalation context from the record, and
// QueuedAt is stamped now — call it at the moment dispatch is decided, not at
// flush time.
//
// The message is truncated to deliveryMessageMaxRunes runes (an ellipsis marks
// the cut) so one pathological alert body cannot blow up a batch row.
func MemberFromRecord(ctx context.Context, rec snoozetypes.Record, notificationName, notificationUID string) DeliveryMember {
	tenant, _ := auth.TenantFrom(ctx)
	return DeliveryMember{
		UID:             rec.UID,
		Hash:            rec.Hash,
		Host:            rec.Host,
		Severity:        rec.Severity,
		Message:         truncateRunes(rec.Message, deliveryMessageMaxRunes),
		State:           rec.State,
		Notification:    notificationName,
		NotificationUID: notificationUID,
		Tenant:          tenant,
		QueuedAt:        time.Now(),
		Escalation:      EscalationFrom(rec),
	}
}

// truncateRunes cuts s so the RESULT is at most maxRunes runes, ellipsis
// included — the ellipsis replaces the last kept rune rather than being added
// on top of the budget, so the documented 512-rune cap is a real cap.
// Rune-based so a multi-byte body is never sliced mid-character.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}

// DeliveryRow is one persisted delivery-history entry, mirroring the
// `NotificationLogEntry` OpenAPI schema. Build it at the call site and hand it
// to RecordDelivery; Doc() owns the wire shape.
type DeliveryRow struct {
	// CompletedAt is when the send finished — the row's sort and retention
	// key (`date_epoch`). Defaults to now when zero.
	CompletedAt time.Time
	// QueuedAt is when dispatch was decided. Ignored when the members carry
	// their own (earlier) queue times.
	QueuedAt time.Time
	// Duration is how long the send itself took.
	Duration time.Duration
	// Status is DeliveryStatusSuccess or DeliveryStatusError.
	Status string
	// Error is the failure text; empty on success.
	Error string
	// Action is the stored action entry's name, Notifier the registry key of
	// the notifier it selected ("mail", "webhook", …).
	Action   string
	Notifier string
	// Batch reports whether this row describes a batch flush, and BatchReason
	// why the bucket flushed (BatchReasonSize / BatchReasonTimer).
	Batch       bool
	BatchReason string
	// Members are the alerts this send covered. A row with no members is
	// never written.
	Members []DeliveryMember
	// Ref is an optional external handle the notifier produced (jira issue
	// key, slack thread ts, …) plus a `url` when one is derivable.
	Ref map[string]any
}

// Doc renders the row into the storage document.
//
// Arrays are built as []any rather than []string on purpose: the in-memory
// condition evaluator's Flatten only descends into []any, so a []string would
// make `notification_uids CONTAINS "x"` silently false on every non-SQL path.
//
// Derived fields:
//   - queued_epoch: the earliest member QueuedAt, else the row's own QueuedAt.
//   - notification_uids / notification_names: distinct, first-seen order.
//   - alert_uids / alert_hashes: distinct, empties skipped (an unresolved uid
//     must not become an empty-string entry the DSL could match).
//   - alerts[]: one entry per DISTINCT member (keyed on uid, falling back to
//     hash), first occurrence kept, so the snapshot list and the flat arrays
//     agree on how many alerts the send covered. alert_count is that same
//     deduped length.
//   - escalation_count: the max over members; escalation_reason: the reason of
//     the first member holding that max.
//
// An alerts[] entry OMITS its `uid` key when the uid could not be resolved
// (a first-occurrence alert whose record had not landed yet); the `hash` is
// always there and is what the UI links on in that case.
//
// Empty string fields are elided; status, action, notifier, batch, alert_count
// and date_epoch are always present so the collection has a stable shape.
func (r DeliveryRow) Doc() db.Document {
	completed := r.CompletedAt
	if completed.IsZero() {
		completed = time.Now()
	}

	members := dedupeMembers(r.Members)

	queued := r.QueuedAt
	for _, m := range members {
		if m.QueuedAt.IsZero() {
			continue
		}
		if queued.IsZero() || m.QueuedAt.Before(queued) {
			queued = m.QueuedAt
		}
	}

	notifUIDs := newDistinct()
	notifNames := newDistinct()
	escCount := 0
	escReason := ""
	// Notification attribution and the escalation summary come from EVERY
	// queued member, not the deduped list: when two notifications route the
	// same alert into one batching action, both of their Deliveries tabs have
	// to find this row, and the row still describes the highest escalation it
	// carried.
	for _, m := range r.Members {
		notifUIDs.add(m.NotificationUID)
		notifNames.add(m.Notification)
		if m.Escalation.Count > escCount {
			escCount = m.Escalation.Count
			escReason = m.Escalation.Reason
		}
	}

	alertUIDs := newDistinct()
	alertHashes := newDistinct()
	alerts := make([]any, 0, len(members))

	for _, m := range members {
		alertUIDs.add(m.UID)
		alertHashes.add(m.Hash)

		entry := map[string]any{}
		putNonEmpty(entry, "uid", m.UID)
		putNonEmpty(entry, "hash", m.Hash)
		putNonEmpty(entry, "host", m.Host)
		putNonEmpty(entry, "severity", m.Severity)
		putNonEmpty(entry, "message", m.Message)
		putNonEmpty(entry, "state", m.State)
		putNonEmpty(entry, "notification", m.Notification)
		alerts = append(alerts, entry)
	}

	durMS := r.Duration.Milliseconds()
	if durMS < 0 {
		durMS = 0
	}

	doc := db.Document{
		"date_epoch":  completed.Unix(),
		"duration_ms": durMS,
		"status":      r.Status,
		"action":      r.Action,
		"notifier":    r.Notifier,
		"batch":       r.Batch,

		"notification_uids":  notifUIDs.values(),
		"notification_names": notifNames.values(),

		"alert_count":  len(members),
		"alert_uids":   alertUIDs.values(),
		"alert_hashes": alertHashes.values(),
		"alerts":       alerts,

		"escalation_count": escCount,
	}
	if !queued.IsZero() {
		doc["queued_epoch"] = queued.Unix()
	}
	putNonEmpty(doc, "error", r.Error)
	putNonEmpty(doc, "batch_reason", r.BatchReason)
	putNonEmpty(doc, "escalation_reason", escReason)
	if len(r.Ref) > 0 {
		doc["ref"] = r.Ref
	}
	return doc
}

// dedupeMembers drops repeated alerts from a row, keeping the first occurrence.
// Identity is the uid when it is known and the hash otherwise — a member whose
// uid was resolved and one that only ever had the hash are the same alert, but
// the row cannot know that, so uid-bearing members are compared on uid.
//
// The same alert reaches a bucket twice when two notifications route it to one
// batching action; without this, alerts[] and alert_count would say "2 alerts"
// while alert_uids (already distinct) says one.
//
// Members with neither uid nor hash are kept as-is: there is nothing to key on
// and dropping them would lose the snapshot.
func dedupeMembers(members []DeliveryMember) []DeliveryMember {
	if len(members) < 2 {
		return members
	}
	seen := make(map[string]struct{}, len(members))
	out := make([]DeliveryMember, 0, len(members))
	for _, m := range members {
		key := m.UID
		if key == "" {
			key = m.Hash
		}
		if key != "" {
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, m)
	}
	return out
}

// putNonEmpty writes v under key only when v is not the empty string.
func putNonEmpty(m map[string]any, key, v string) {
	if v == "" {
		return
	}
	m[key] = v
}

// distinct collects non-empty strings in first-seen order.
type distinct struct {
	seen map[string]struct{}
	out  []any
}

func newDistinct() *distinct {
	return &distinct{seen: map[string]struct{}{}, out: []any{}}
}

func (d *distinct) add(s string) {
	if s == "" {
		return
	}
	if _, ok := d.seen[s]; ok {
		return
	}
	d.seen[s] = struct{}{}
	d.out = append(d.out, s)
}

func (d *distinct) values() []any { return d.out }

// DeliveryLogEnabled reports whether delivery-history rows should be written.
//
// The live runtime setting wins when the host exposes one (operators flip it in
// Settings without a restart); otherwise the boot-time file config decides. It
// fails open — a host with neither wired still logs, because losing the history
// silently is worse than the write.
func DeliveryLogEnabled(ctx context.Context, host Host) bool {
	if host == nil {
		return true
	}
	if rsh, ok := host.(RuntimeSettingsHost); ok {
		if rs := rsh.RuntimeSettings(); rs != nil {
			return rs.DeliveryLog(ctx)
		}
	}
	if cfg := host.Config(); cfg != nil {
		return cfg.Notification.DeliveryLog
	}
	return true
}

// RecordDelivery best-effort persists one delivery-history row. It is a no-op
// when the host or driver is missing, when the operator turned the log off, or
// when the row covers no alerts.
//
// The batch-flush path runs on a detached goroutine whose context carries no
// tenant; the members do, so the tenant is re-stamped from the first member
// before the write (the driver fails closed on a naked context for a scoped
// collection). A write failure is logged and swallowed — the delivery already
// happened, and losing its history must never surface as a send error.
func RecordDelivery(ctx context.Context, host Host, row DeliveryRow) {
	if host == nil || len(row.Members) == 0 {
		return
	}
	drv := host.DB()
	if drv == nil {
		return
	}
	// Re-stamp the tenant BEFORE the gate, not after: DeliveryLogEnabled reads
	// the live per-tenant runtime setting, so evaluating it on the naked
	// flush-goroutine context would answer for the wrong scope.
	if tenant, ok := auth.TenantFrom(ctx); !ok || tenant == "" {
		if t := row.Members[0].Tenant; t != "" {
			ctx = auth.WithTenant(ctx, t)
		}
	}
	if !DeliveryLogEnabled(ctx, host) {
		return
	}
	if _, err := drv.Write(ctx, NotificationLogCollection, []db.Document{row.Doc()}, db.WriteOptions{UpdateTime: false}); err != nil {
		if lg := host.Logger(); lg != nil {
			lg.Warn("notificationlog: write failed",
				"action", row.Action,
				"notifier", row.Notifier,
				"status", row.Status,
				"alerts", len(row.Members),
				"err", err)
		}
	}
}

// MembersTenant returns the tenant a set of delivery members belongs to.
//
// Batch buckets are keyed by tenant (every batching notifier prefixes its key
// with the tenant read off the Send context), so all members of one bucket
// share it and the first non-empty value is the answer. The batch flush runs on
// a detached timer goroutine with no request context, so this is how the
// tenant-scoped reads and writes downstream get their scope back.
func MembersTenant(members []DeliveryMember) string {
	for _, m := range members {
		if m.Tenant != "" {
			return m.Tenant
		}
	}
	return ""
}

// deliveryBackfillWindow bounds how long BackfillMemberUIDs waits for records
// that have not landed yet, deliveryBackfillRetryInterval is the FIRST pause
// between rounds (same cadence as the dispatcher's own write-back retry, then
// doubling), and maxBackfillReads is the hard ceiling on GetOne calls one
// backfill may issue.
//
// The wait is needed because a SIZE-triggered flush runs synchronously inside
// the Send of the last member — i.e. on the dispatcher's goroutine, racing the
// pipeline's own write of that very record. A single GetOne would lose that
// race often enough for the freshest alert in a batch to degrade to a hash-only
// link. The window is shared by the whole backfill (attempts go in rounds over
// the still-unresolved hashes), so a hash that will never resolve costs the
// window once, not once per member.
//
// The two caps exist because the window alone bounds the LATENCY, not the
// fan-out: with a fixed 20ms retry a bucket of N never-landing hashes fired
// ~25*N point reads at the driver inside half a second, and batch_maxsize is
// operator-configured. The geometric backoff cuts the rounds to ~5 and
// maxBackfillReads caps the total regardless of bucket size — a member the
// budget did not reach simply keeps its empty uid, which is the same outcome as
// a hash that never resolves.
const (
	deliveryBackfillWindow        = 500 * time.Millisecond
	deliveryBackfillRetryInterval = 20 * time.Millisecond
	maxBackfillReads              = 200
)

// BackfillMemberUIDs resolves the alert uids that were unknown at queue time,
// mutating members in place.
//
// A first-occurrence record has its uid assigned by the driver at write time,
// which happens after the dispatcher (and therefore after Send queued the
// member), so those members carry only a hash. One GetOne per DISTINCT hash
// recovers the uid the UI links on; a hash whose record has not been persisted
// yet is retried (see deliveryBackfillWindow) instead of being given up on.
//
// Best effort throughout: no host, no driver, an expired ctx or a read that
// keeps missing simply leaves the uid empty and the row keeps the hash. The
// whole call is bounded by deliveryBackfillWindow PLUS the read budget: the
// window caps the time spent sleeping between rounds, maxBackfillReads caps the
// driver round-trips, and a cancelled ctx stops the loop at the next hash.
func BackfillMemberUIDs(ctx context.Context, host Host, members []DeliveryMember) {
	if host == nil {
		return
	}
	drv := host.DB()
	if drv == nil {
		return
	}
	pending := make(map[string]struct{}, len(members))
	for i := range members {
		if members[i].UID == "" && members[i].Hash != "" {
			pending[members[i].Hash] = struct{}{}
		}
	}
	if len(pending) == 0 {
		return
	}

	resolved := make(map[string]string, len(pending))
	deadline := time.Now().Add(deliveryBackfillWindow)
	reads := 0
	backoff := deliveryBackfillRetryInterval

rounds:
	for {
		for hash := range pending {
			if ctx.Err() != nil {
				// Checked here and not only in the timer select below: a ctx
				// that was ALREADY cancelled on entry never reaches a sleep,
				// so the first round would otherwise fire the whole read
				// budget at the driver knowing every call is doomed.
				break rounds
			}
			if reads >= maxBackfillReads {
				// Read budget spent. Whatever is still pending keeps an empty
				// uid; the row carries the hash and the UI links on that.
				break rounds
			}
			reads++
			doc, err := drv.GetOne(ctx, recordCollection, db.Document{"hash": hash})
			if err == nil {
				uid, _ := doc["uid"].(string)
				resolved[hash] = uid
				delete(pending, hash)
				continue
			}
			if !errors.Is(err, db.ErrNotFound) {
				// A driver error is not something another round will fix.
				delete(pending, hash)
			}
		}
		if len(pending) == 0 {
			break
		}
		// Geometric backoff (20ms → 40 → 80 …) clamped to what is left of the
		// window, so the last round still starts inside it and the total time
		// spent waiting stays inside deliveryBackfillWindow.
		wait := backoff
		if left := time.Until(deadline); wait > left {
			wait = left
		}
		if wait <= 0 {
			break
		}
		backoff *= 2
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			// Out of budget: keep whatever was resolved, drop the rest.
			t.Stop()
			break rounds
		case <-t.C:
		}
	}

	for i := range members {
		m := &members[i]
		if m.UID != "" || m.Hash == "" {
			continue
		}
		m.UID = resolved[m.Hash]
	}
}

// BumpNotificationCounters stamps `hits` and `last_sent` on every DISTINCT
// notification appearing in members — once per notification, whatever the
// number of actions or alerts involved (D10). Call it only for a SUCCESSFUL
// delivery: failed and misconfigured sends never move the counters.
//
// Both the unbatched dispatcher and every batch flush call this, which is the
// point of it living here: before, only the dispatcher's own rows bumped the
// counters, so a batching action never advanced `hits` at all.
//
// It is a read-modify-write (like the snooze plugin's bumpHits), not an
// asyncwriter increment: the process-wide writer is built WithUpsert(true)
// (internal/core/boot.go), so an increment keyed on the uid of a notification
// deleted between dispatch and flush would INSERT a ghost notification document
// holding nothing but {uid, hits}. A conditional SetFields simply matches no
// row instead. The counters are advisory ("Sent 142× · last Today 14:32"), so
// losing one to a concurrent bump is acceptable; inventing a notification is
// not.
//
// Both fields go out in ONE SetFields so the Mongo watcher sees a single
// {hits, last_sent} update and drops it as counter-only instead of triggering a
// notification-cache reload per delivery.
//
// The tenant is re-stamped from the members when ctx carries none, for the same
// reason RecordDelivery does it: the flush goroutine has no request context and
// the notification collection is tenant-scoped.
func BumpNotificationCounters(ctx context.Context, host Host, members []DeliveryMember) {
	if host == nil || len(members) == 0 {
		return
	}
	drv := host.DB()
	if drv == nil {
		return
	}
	if tenant, ok := auth.TenantFrom(ctx); !ok || tenant == "" {
		if t := MembersTenant(members); t != "" {
			ctx = auth.WithTenant(ctx, t)
		}
	}
	now := time.Now().Unix()
	seen := make(map[string]struct{}, len(members))
	for _, m := range members {
		if m.NotificationUID == "" {
			continue
		}
		if _, dup := seen[m.NotificationUID]; dup {
			continue
		}
		seen[m.NotificationUID] = struct{}{}
		bumpNotificationCounter(ctx, host, drv, m.NotificationUID, now)
	}
}

// bumpNotificationCounter applies one notification's counter update. A vanished
// notification is not an error: nothing matches and nothing is created.
func bumpNotificationCounter(ctx context.Context, host Host, drv db.Driver, notificationUID string, now int64) {
	doc, err := drv.GetOne(ctx, notificationCollection, db.Document{"uid": notificationUID})
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			if lg := host.Logger(); lg != nil {
				lg.Warn("notification: counter read failed", "uid", notificationUID, "err", err)
			}
		}
		return
	}
	hits, _ := CounterValue(doc["hits"])
	if _, err := drv.SetFields(ctx, notificationCollection, db.Document{
		"hits":      hits + 1,
		"last_sent": now,
	}, condition.Equals("uid", notificationUID)); err != nil {
		if lg := host.Logger(); lg != nil {
			lg.Warn("notification: counter write failed", "uid", notificationUID, "err", err)
		}
	}
}

// CounterValue coerces a stored counter to int64. Which concrete Go type it
// comes back as depends on the backend (float64 from the JSON columns,
// int32/int64 from BSON), so all of them are accepted.
//
// Exported because every caller that reads a counter back out of the driver
// needs the same coercion — the notification plugin's tests used to carry a
// byte-identical private copy (`toInt64`).
func CounterValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float32:
		return int64(n), true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}
