package housekeeper

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeLifecycle is a stand-in for the lifecycleTimeouts contract the job reads
// the configured ack/escalate windows from.
type fakeLifecycle struct {
	ack      time.Duration
	escalate time.Duration
}

func (f fakeLifecycle) AckTimeout(context.Context) time.Duration    { return f.ack }
func (f fakeLifecycle) EscalateAfter(context.Context) time.Duration { return f.escalate }

// escalateFakeDriver is an in-memory db.Driver tailored to the EscalateTimeoutJob
// tests. It stores records and comments per-tenant (keyed by the tenant in the
// query ctx) so the per-tenant scoping can be asserted. Search evaluates the
// supplied condition with the real condition.Match so the job's deadline
// predicates are exercised exactly as production would.
type escalateFakeDriver struct {
	mu       sync.Mutex
	tenants  []db.Document            // rows returned for the "tenant" collection
	records  map[string][]db.Document // tenantID → records
	comments map[string][]db.Document // tenantID → comments
	incs     []incCall
}

type incCall struct {
	tenant     string
	collection string
	field      string
	delta      int64
}

func newEscalateFakeDriver() *escalateFakeDriver {
	return &escalateFakeDriver{
		tenants:  []db.Document{{"id": "default", "status": "active"}},
		records:  map[string][]db.Document{},
		comments: map[string][]db.Document{},
	}
}

func tenantOf(ctx context.Context) string {
	id, _ := auth.TenantFrom(ctx)
	return id
}

func (d *escalateFakeDriver) Search(ctx context.Context, collection string, cond condition.Cond, _ db.Page) ([]db.Document, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if collection == "tenant" {
		out := append([]db.Document(nil), d.tenants...)
		return out, len(out), nil
	}
	if collection == "record" {
		tid := tenantOf(ctx)
		var out []db.Document
		for _, r := range d.records[tid] {
			if condition.Match(r, cond) {
				out = append(out, r)
			}
		}
		return out, len(out), nil
	}
	return nil, 0, nil
}

func (d *escalateFakeDriver) UpdateOne(ctx context.Context, collection, uid string, patch db.Document, _ bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if collection != "record" {
		return nil
	}
	tid := tenantOf(ctx)
	for _, r := range d.records[tid] {
		if u, _ := r["uid"].(string); u == uid {
			for k, v := range patch {
				r[k] = v
			}
		}
	}
	return nil
}

func (d *escalateFakeDriver) Write(ctx context.Context, collection string, docs []db.Document, _ db.WriteOptions) (db.WriteResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if collection == "comment" {
		tid := tenantOf(ctx)
		d.comments[tid] = append(d.comments[tid], docs...)
	}
	return db.WriteResult{}, nil
}

func (d *escalateFakeDriver) IncMany(ctx context.Context, collection, field string, cond condition.Cond, delta int64) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	tid := tenantOf(ctx)
	matched := 0
	for _, r := range d.records[tid] {
		if condition.Match(r, cond) {
			cur, _ := r[field].(int64)
			r[field] = cur + delta
			matched++
		}
	}
	d.incs = append(d.incs, incCall{tenant: tid, collection: collection, field: field, delta: delta})
	return matched, nil
}

// seedRecord registers a record under the given tenant.
func (d *escalateFakeDriver) seedRecord(tenant string, doc db.Document) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records[tenant] = append(d.records[tenant], doc)
}

func (d *escalateFakeDriver) record(tenant, uid string) db.Document {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.records[tenant] {
		if u, _ := r["uid"].(string); u == uid {
			return r
		}
	}
	return nil
}

func (d *escalateFakeDriver) commentCount(tenant string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.comments[tenant])
}

// --- remaining db.Driver methods are no-op passthroughs ---

func (d *escalateFakeDriver) GetOne(context.Context, string, db.Document) (db.Document, error) {
	return nil, nil
}
func (d *escalateFakeDriver) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, nil
}
func (d *escalateFakeDriver) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, nil
}
func (d *escalateFakeDriver) Delete(context.Context, string, condition.Cond, bool) (int, error) {
	return 0, nil
}
func (d *escalateFakeDriver) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error {
	return nil
}
func (d *escalateFakeDriver) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, nil
}
func (d *escalateFakeDriver) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
	return 0, nil
}
func (d *escalateFakeDriver) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *escalateFakeDriver) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *escalateFakeDriver) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *escalateFakeDriver) CreateIndex(context.Context, string, []string) error { return nil }
func (d *escalateFakeDriver) ListCollections(context.Context) ([]string, error)   { return nil, nil }
func (d *escalateFakeDriver) Drop(context.Context, string) error                  { return nil }
func (d *escalateFakeDriver) Backup(context.Context, string, []string) error      { return nil }
func (d *escalateFakeDriver) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (d *escalateFakeDriver) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (d *escalateFakeDriver) CleanupOrphans(context.Context, string) (int, error) { return 0, nil }
func (d *escalateFakeDriver) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (d *escalateFakeDriver) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (d *escalateFakeDriver) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (d *escalateFakeDriver) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (d *escalateFakeDriver) Watcher() syncer.Bus { return nil }
func (d *escalateFakeDriver) Close() error        { return nil }

// runJob executes the IntervalJob's Run once under a platform-less background
// context (ForEachTenant lifts to platform scope internally).
func runJob(t *testing.T, ij IntervalJob) {
	t.Helper()
	require.NoError(t, ij.Job.Run(context.Background()))
}

// TestEscalateTimeoutJob_RevertsExpiredAck: a record in state "ack" whose
// ack_until is in the past reverts to "open", ack_until clears to 0, and an
// auto open-comment is written.
func TestEscalateTimeoutJob_RevertsExpiredAck(t *testing.T) {
	clk := newFakeClock(time.Unix(1_000_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{
		"uid": "r-ack", "state": "ack", "ack_until": now - 10,
	})

	notified := 0
	notify := func(context.Context, snoozetypes.Record) error { notified++; return nil }

	ij := EscalateTimeoutJob(drv, clk, fakeLifecycle{ack: time.Hour, escalate: 0}, notify)
	runJob(t, ij)

	rec := drv.record("default", "r-ack")
	require.Equal(t, "open", rec["state"])
	require.Equal(t, int64(0), rec["ack_until"])
	require.Equal(t, 1, drv.commentCount("default"), "expected one auto comment")
	require.Equal(t, 0, notified, "reverting an ack must not re-notify")
}

// TestEscalateTimeoutJob_EscalatesOverdueOpen: with escalate_after>0, an open
// record overdue past escalate_at flips to "esc", clears escalate_at, writes an
// auto esc-comment, and invokes the notify callback exactly once with the
// escalated record.
func TestEscalateTimeoutJob_EscalatesOverdueOpen(t *testing.T) {
	clk := newFakeClock(time.Unix(2_000_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{
		"uid": "r-open", "state": "open", "escalate_at": now - 5,
	})

	var notifiedRecs []snoozetypes.Record
	notify := func(_ context.Context, rec snoozetypes.Record) error {
		notifiedRecs = append(notifiedRecs, rec)
		return nil
	}

	ij := EscalateTimeoutJob(drv, clk, fakeLifecycle{ack: time.Hour, escalate: time.Hour}, notify)
	runJob(t, ij)

	rec := drv.record("default", "r-open")
	require.Equal(t, "esc", rec["state"])
	require.Equal(t, int64(0), rec["escalate_at"], "escalation is one-shot")
	require.Equal(t, 1, drv.commentCount("default"))
	require.Len(t, notifiedRecs, 1, "notify must fire exactly once for the escalated record")
	require.Equal(t, "r-open", notifiedRecs[0].UID)
	require.Equal(t, "esc", notifiedRecs[0].State)
}

// TestEscalateTimeoutJob_StampsEscalationContext locks in the sweep half of the
// escalation contract. The counter must land BOTH on the stored row and on the
// in-memory record handed to notify — the row is what the next occurrence reads,
// the in-memory copy is what the notifiers about to fire branch on. If they
// disagree, a notifier either duplicates a ticket or comments on one it never
// created.
func TestEscalateTimeoutJob_StampsEscalationContext(t *testing.T) {
	clk := newFakeClock(time.Unix(2_100_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{
		"uid": "r-first", "state": "open", "escalate_at": now - 5,
	})
	// A record already escalated twice: the sweep must increment, not reset.
	drv.seedRecord("default", db.Document{
		"uid": "r-again", "state": "open", "escalate_at": now - 5,
		"escalation_count": int64(2),
	})

	var notified []snoozetypes.Record
	notify := func(_ context.Context, rec snoozetypes.Record) error {
		notified = append(notified, rec)
		return nil
	}

	ij := EscalateTimeoutJob(drv, clk, fakeLifecycle{ack: time.Hour, escalate: time.Hour}, notify)
	runJob(t, ij)

	first := drv.record("default", "r-first")
	require.Equal(t, 1, first["escalation_count"])
	require.Equal(t, now, first["escalated_at"])
	require.Equal(t, "timeout", first["escalation_reason"])

	again := drv.record("default", "r-again")
	require.Equal(t, 3, again["escalation_count"], "an already-escalated record must increment")

	byUID := map[string]snoozetypes.Record{}
	for _, r := range notified {
		byUID[r.UID] = r
	}
	require.Len(t, byUID, 2)
	require.Equal(t, 1, byUID["r-first"].EscalationCount,
		"the in-memory record must agree with the stored row")
	require.Equal(t, 3, byUID["r-again"].EscalationCount)
	require.Equal(t, "timeout", byUID["r-again"].EscalationReason)
	require.Equal(t, now, byUID["r-again"].EscalatedAt)
}

// docInt64 must tolerate whichever numeric shape a driver decoded the counter
// into, or the sweep silently restarts the count from 1 on some backends.
func TestDocInt64NumericShapes(t *testing.T) {
	for name, v := range map[string]any{
		"int":     7,
		"int32":   int32(7),
		"int64":   int64(7),
		"float64": float64(7),
		"float32": float32(7),
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, int64(7), docInt64(db.Document{"escalation_count": v}, "escalation_count"))
		})
	}
	require.Equal(t, int64(0), docInt64(db.Document{}, "escalation_count"))
	require.Equal(t, int64(0), docInt64(db.Document{"escalation_count": "nope"}, "escalation_count"))
}

// TestEscalateTimeoutJob_SkipsWhenEscalateAfterZero: an overdue open record is
// left untouched and no notify fires when escalate_after<=0 (the escalate pass
// is a strict no-op). The expired-ack pass still runs.
func TestEscalateTimeoutJob_SkipsWhenEscalateAfterZero(t *testing.T) {
	clk := newFakeClock(time.Unix(3_000_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{
		"uid": "r-open", "state": "open", "escalate_at": now - 5,
	})

	notified := 0
	notify := func(context.Context, snoozetypes.Record) error { notified++; return nil }

	ij := EscalateTimeoutJob(drv, clk, fakeLifecycle{ack: time.Hour, escalate: 0}, notify)
	runJob(t, ij)

	rec := drv.record("default", "r-open")
	require.Equal(t, "open", rec["state"], "escalate pass must be a no-op when escalate_after<=0")
	require.Equal(t, 0, notified)
	require.Equal(t, 0, drv.commentCount("default"), "no auto comment when escalation disabled")
}

// TestEscalateTimeoutJob_PerTenant: the sweep runs inside each active tenant's
// scope. A record in tenant "default" and another in tenant "acme" must both be
// processed under their own scope.
func TestEscalateTimeoutJob_PerTenant(t *testing.T) {
	clk := newFakeClock(time.Unix(4_000_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.tenants = []db.Document{
		{"id": "default", "status": "active"},
		{"id": "acme", "status": "active"},
		{"id": "ghost", "status": "suspended"}, // must be skipped
	}
	drv.seedRecord("default", db.Document{"uid": "d-ack", "state": "ack", "ack_until": now - 1})
	drv.seedRecord("acme", db.Document{"uid": "a-ack", "state": "ack", "ack_until": now - 1})
	drv.seedRecord("ghost", db.Document{"uid": "g-ack", "state": "ack", "ack_until": now - 1})

	notify := func(context.Context, snoozetypes.Record) error { return nil }
	ij := EscalateTimeoutJob(drv, clk, fakeLifecycle{ack: time.Hour, escalate: 0}, notify)
	runJob(t, ij)

	require.Equal(t, "open", drv.record("default", "d-ack")["state"])
	require.Equal(t, "open", drv.record("acme", "a-ack")["state"])
	// Suspended tenant skipped: its record is never touched.
	require.Equal(t, "ack", drv.record("ghost", "g-ack")["state"])
}

// TestEscalateTimeoutJob_IgnoresUnexpiredAndZeroDeadlines: a still-valid ack
// (ack_until in the future) and a record with ack_until==0 are both untouched.
func TestEscalateTimeoutJob_IgnoresUnexpiredAndZeroDeadlines(t *testing.T) {
	clk := newFakeClock(time.Unix(5_000_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{"uid": "future", "state": "ack", "ack_until": now + 3600})
	drv.seedRecord("default", db.Document{"uid": "zero", "state": "ack", "ack_until": int64(0)})

	notify := func(context.Context, snoozetypes.Record) error { return nil }
	ij := EscalateTimeoutJob(drv, clk, fakeLifecycle{ack: time.Hour, escalate: time.Hour}, notify)
	runJob(t, ij)

	require.Equal(t, "ack", drv.record("default", "future")["state"])
	require.Equal(t, "ack", drv.record("default", "zero")["state"])
}

// TestEscalateTimeoutJob_PreservesUntypedRecordFields is the regression test for
// the worst bug in the escalation work: the sweep projected the stored document
// into a typed Record via a JSON round-trip, and Record.Extra is `json:"-"`, so
// EVERY untyped field was silently dropped on the way to the notifiers.
//
// That included notify_ref_<action> — the handle a notifier uses to find the
// ticket or chat thread it already created. So on the ack-timeout path, which is
// the most common escalation of all, JIRA would not find its issue and would
// open a SECOND ticket: exactly the duplication this feature exists to prevent,
// on the one path that matters most.
func TestEscalateTimeoutJob_PreservesUntypedRecordFields(t *testing.T) {
	clk := newFakeClock(time.Unix(2_200_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{
		"uid": "r-handle", "state": "open", "escalate_at": now - 5,
		"severity": "critical",
		// Stamped by a previous delivery of the notifier.
		"notify_ref_Create ticket": map[string]any{"issue_key": "OPS-1"},
		// Aggregaterule counters notifiers and conditions also read.
		"duplicates":        int64(4),
		"previous_severity": "warning",
		"trend_indication":  "moreSevere",
	})

	var notified []snoozetypes.Record
	notify := func(_ context.Context, rec snoozetypes.Record) error {
		notified = append(notified, rec)
		return nil
	}

	ij := EscalateTimeoutJob(drv, clk, fakeLifecycle{ack: time.Hour, escalate: time.Hour}, notify)
	runJob(t, ij)

	require.Len(t, notified, 1)
	rec := notified[0]
	require.Equal(t, "critical", rec.Severity)
	require.NotNil(t, rec.Extra, "untyped fields must survive the projection")
	// The handle a notifier reads via plugins.NotifyRef. Asserted on the raw
	// field here because internal/plugins cannot be imported from this package
	// (import cycle through telemetry).
	ref, ok := rec.Extra["notify_ref_Create ticket"].(map[string]any)
	require.True(t, ok, "without the handle the notifier opens a duplicate ticket")
	require.Equal(t, "OPS-1", ref["issue_key"])

	// The aggregaterule counters the escalation context is built from.
	require.Equal(t, int64(4), rec.Extra["duplicates"])
	require.Equal(t, "warning", rec.Extra["previous_severity"])
	require.Equal(t, 1, rec.EscalationCount)
}
