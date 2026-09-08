package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// capturingDB records the context and documents of every Write so the tenant
// stamping contract can be asserted without a real driver.
type capturingDB struct {
	*memDB
	writes []capturedWrite
	err    error
}

type capturedWrite struct {
	ctx  context.Context //nolint:containedctx // the assertion target
	col  string
	docs []db.Document
	opts db.WriteOptions
}

func newCapturingDB() *capturingDB { return &capturingDB{memDB: newMemDB()} }

func (c *capturingDB) Write(ctx context.Context, col string, docs []db.Document, opts db.WriteOptions) (db.WriteResult, error) {
	c.writes = append(c.writes, capturedWrite{ctx: ctx, col: col, docs: docs, opts: opts})
	if c.err != nil {
		return db.WriteResult{}, c.err
	}
	return c.memDB.Write(ctx, col, docs, opts)
}

// rsHost is a nullHost that also satisfies RuntimeSettingsHost, mirroring the
// real core host.
type rsHost struct {
	*nullHost
	rs *config.RuntimeSettings
}

func (h *rsHost) RuntimeSettings() *config.RuntimeSettings { return h.rs }

func testMember(uid, hash, notif, notifUID string) DeliveryMember {
	return DeliveryMember{
		UID:             uid,
		Hash:            hash,
		Host:            "db-01",
		Severity:        "critical",
		Message:         "disk 98%",
		State:           "open",
		Notification:    notif,
		NotificationUID: notifUID,
		Tenant:          snoozetypes.DefaultTenant,
		QueuedAt:        time.Unix(1757339998, 0),
	}
}

func TestMemberFromRecord(t *testing.T) {
	ctx := auth.WithTenant(context.Background(), "acme")
	rec := snoozetypes.Record{
		UID:              "a1",
		Hash:             "h1",
		Host:             "db-01",
		Severity:         "critical",
		Message:          "disk 98%",
		State:            "open",
		EscalationCount:  2,
		EscalationReason: "timeout",
	}
	before := time.Now()
	m := MemberFromRecord(ctx, rec, "page-oncall", "n-1")

	require.Equal(t, "a1", m.UID)
	require.Equal(t, "h1", m.Hash)
	require.Equal(t, "db-01", m.Host)
	require.Equal(t, "critical", m.Severity)
	require.Equal(t, "disk 98%", m.Message)
	require.Equal(t, "open", m.State)
	require.Equal(t, "page-oncall", m.Notification)
	require.Equal(t, "n-1", m.NotificationUID)
	require.Equal(t, "acme", m.Tenant)
	require.Equal(t, 2, m.Escalation.Count)
	require.Equal(t, "timeout", m.Escalation.Reason)
	require.False(t, m.QueuedAt.Before(before))
}

// TestMemberFromRecordTruncatesMessage pins the 512-rune cap: a pathological
// alert body must not be copied wholesale into every batch row.
func TestMemberFromRecordTruncatesMessage(t *testing.T) {
	long := strings.Repeat("é", 600)
	m := MemberFromRecord(context.Background(), snoozetypes.Record{Message: long}, "n", "")

	got := []rune(m.Message)
	require.Len(t, got, deliveryMessageMaxRunes, "the cap is a total, ellipsis included")
	require.Equal(t, '…', got[len(got)-1])
	require.Equal(t, strings.Repeat("é", deliveryMessageMaxRunes-1), string(got[:deliveryMessageMaxRunes-1]))

	// Exactly at the cap: untouched, no ellipsis.
	exact := strings.Repeat("x", deliveryMessageMaxRunes)
	require.Equal(t, exact, MemberFromRecord(context.Background(), snoozetypes.Record{Message: exact}, "n", "").Message)

	// Naked tenant-less context leaves Tenant empty rather than guessing.
	require.Empty(t, m.Tenant)
}

func TestDeliveryRowDocUnbatched(t *testing.T) {
	row := DeliveryRow{
		CompletedAt: time.Unix(1757340000, 0),
		Duration:    412 * time.Millisecond,
		Status:      DeliveryStatusSuccess,
		Action:      "mail-oncall",
		Notifier:    "mail",
		Members:     []DeliveryMember{testMember("a1", "h1", "page-oncall", "n-1")},
	}
	doc := row.Doc()

	require.Equal(t, int64(1757340000), doc["date_epoch"])
	require.Equal(t, int64(1757339998), doc["queued_epoch"])
	require.Equal(t, int64(412), doc["duration_ms"])
	require.Equal(t, "success", doc["status"])
	require.Equal(t, "mail-oncall", doc["action"])
	require.Equal(t, "mail", doc["notifier"])
	require.Equal(t, false, doc["batch"])
	require.Equal(t, 1, doc["alert_count"])
	require.Equal(t, []any{"n-1"}, doc["notification_uids"])
	require.Equal(t, []any{"page-oncall"}, doc["notification_names"])
	require.Equal(t, []any{"a1"}, doc["alert_uids"])
	require.Equal(t, []any{"h1"}, doc["alert_hashes"])
	require.Equal(t, 0, doc["escalation_count"])

	alerts, ok := doc["alerts"].([]any)
	require.True(t, ok)
	require.Len(t, alerts, 1)
	require.Equal(t, map[string]any{
		"uid": "a1", "hash": "h1", "host": "db-01", "severity": "critical",
		"message": "disk 98%", "state": "open", "notification": "page-oncall",
	}, alerts[0])

	// Empty strings are elided rather than written as "".
	require.NotContains(t, doc, "error")
	require.NotContains(t, doc, "batch_reason")
	require.NotContains(t, doc, "escalation_reason")
	require.NotContains(t, doc, "ref")
}

// TestDeliveryRowDocBatch covers the flush row: distinct flat arrays across
// members, the earliest queue time, and the max escalation.
func TestDeliveryRowDocBatch(t *testing.T) {
	m1 := testMember("a1", "h1", "page-oncall", "n-1")
	m1.QueuedAt = time.Unix(2000, 0)
	m1.Escalation = Escalation{Count: 1, Reason: "timeout"}

	m2 := testMember("a2", "h2", "page-oncall", "n-1") // same notification
	m2.QueuedAt = time.Unix(1000, 0)                   // earliest
	m2.Escalation = Escalation{Count: 3, Reason: "manual"}

	m3 := testMember("", "h3", "wake-sre", "n-2") // uid unresolved
	m3.QueuedAt = time.Unix(3000, 0)
	m3.Escalation = Escalation{Count: 3, Reason: "watchlist"} // ties: first wins

	m4 := testMember("a4", "h1", "", "") // duplicate hash, no notification
	m4.QueuedAt = time.Unix(4000, 0)

	row := DeliveryRow{
		CompletedAt: time.Unix(9000, 0),
		QueuedAt:    time.Unix(8000, 0), // later than the members: ignored
		Duration:    2 * time.Second,
		Status:      DeliveryStatusError,
		Error:       "dial tcp: refused",
		Action:      "webhook-ops",
		Notifier:    "webhook",
		Batch:       true,
		BatchReason: BatchReasonSize,
		Members:     []DeliveryMember{m1, m2, m3, m4},
		Ref:         map[string]any{"issue_key": "OPS-123"},
	}
	doc := row.Doc()

	require.Equal(t, int64(1000), doc["queued_epoch"], "earliest member wins")
	require.Equal(t, true, doc["batch"])
	require.Equal(t, "size", doc["batch_reason"])
	require.Equal(t, "dial tcp: refused", doc["error"])
	require.Equal(t, 4, doc["alert_count"])
	require.Equal(t, []any{"n-1", "n-2"}, doc["notification_uids"], "distinct, first-seen order")
	require.Equal(t, []any{"page-oncall", "wake-sre"}, doc["notification_names"])
	require.Equal(t, []any{"a1", "a2", "a4"}, doc["alert_uids"], "the unresolved uid is skipped, not blank")
	require.Equal(t, []any{"h1", "h2", "h3"}, doc["alert_hashes"], "hashes are de-duplicated")
	require.Equal(t, 3, doc["escalation_count"], "max over members")
	require.Equal(t, "manual", doc["escalation_reason"], "reason of the first member at the max")
	require.Equal(t, map[string]any{"issue_key": "OPS-123"}, doc["ref"])

	alerts, ok := doc["alerts"].([]any)
	require.True(t, ok)
	require.Len(t, alerts, 4, "alerts[] keeps every member, even the uid-less one")
	require.NotContains(t, alerts[2], "uid")
	require.NotContains(t, alerts[3], "notification")
}

// TestDeliveryRowDocArraysAreAnySlices guards the DSL: the in-memory condition
// evaluator only flattens []any, so a []string here would make
// `notification_uids CONTAINS "x"` silently false off the SQL path.
func TestDeliveryRowDocArraysAreAnySlices(t *testing.T) {
	doc := DeliveryRow{Members: []DeliveryMember{testMember("a1", "h1", "n", "n-1")}}.Doc()
	for _, key := range []string{"notification_uids", "notification_names", "alert_uids", "alert_hashes", "alerts"} {
		_, ok := doc[key].([]any)
		require.Truef(t, ok, "%s must be []any, got %T", key, doc[key])
	}
}

func TestDeliveryRowDocDefaultsCompletedAt(t *testing.T) {
	doc := DeliveryRow{Members: []DeliveryMember{testMember("a1", "h1", "n", "n-1")}}.Doc()
	epoch, ok := doc["date_epoch"].(int64)
	require.True(t, ok)
	require.InDelta(t, time.Now().Unix(), epoch, 5)
}

func TestDeliveryLogEnabled(t *testing.T) {
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	require.True(t, DeliveryLogEnabled(ctx, nil), "no host: fail open")

	host := newNullHost(newMemDB())
	require.True(t, DeliveryLogEnabled(ctx, host), "config default is on")

	host.cfg.Notification.DeliveryLog = false
	require.False(t, DeliveryLogEnabled(ctx, host))

	// A host exposing runtime settings reads those instead of the file config.
	base := config.Default()
	base.Notification.DeliveryLog = false
	live := &rsHost{nullHost: newNullHost(newMemDB()), rs: config.NewRuntimeSettings(newMemDB(), base, time.Minute)}
	require.False(t, DeliveryLogEnabled(ctx, live), "runtime settings win over the file config")

	// A nil RuntimeSettings pointer falls back to the file config.
	nilRS := &rsHost{nullHost: newNullHost(newMemDB())}
	nilRS.cfg.Notification.DeliveryLog = false
	require.False(t, DeliveryLogEnabled(ctx, nilRS))
	nilRS.cfg.Notification.DeliveryLog = true
	require.True(t, DeliveryLogEnabled(ctx, nilRS))
}

func TestRecordDeliveryWritesRow(t *testing.T) {
	drv := newCapturingDB()
	host := newNullHost(drv)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	RecordDelivery(ctx, host, DeliveryRow{
		Status:  DeliveryStatusSuccess,
		Action:  "mail-oncall",
		Members: []DeliveryMember{testMember("a1", "h1", "page-oncall", "n-1")},
	})

	require.Len(t, drv.writes, 1)
	w := drv.writes[0]
	require.Equal(t, NotificationLogCollection, w.col)
	require.False(t, w.opts.UpdateTime, "history rows are immutable; no updated_at churn")
	require.Len(t, w.docs, 1)
	require.Equal(t, "mail-oncall", w.docs[0]["action"])
}

func TestRecordDeliveryGating(t *testing.T) {
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	row := DeliveryRow{Status: DeliveryStatusSuccess, Members: []DeliveryMember{testMember("a1", "h1", "n", "n-1")}}

	t.Run("disabled", func(t *testing.T) {
		drv := newCapturingDB()
		host := newNullHost(drv)
		host.cfg.Notification.DeliveryLog = false
		RecordDelivery(ctx, host, row)
		require.Empty(t, drv.writes)
	})

	t.Run("no members", func(t *testing.T) {
		drv := newCapturingDB()
		RecordDelivery(ctx, newNullHost(drv), DeliveryRow{Status: DeliveryStatusSuccess})
		require.Empty(t, drv.writes)
	})

	t.Run("nil host", func(t *testing.T) {
		require.NotPanics(t, func() { RecordDelivery(ctx, nil, row) })
	})

	t.Run("nil driver", func(t *testing.T) {
		require.NotPanics(t, func() { RecordDelivery(ctx, newNullHost(nil), row) })
	})

	t.Run("write error is swallowed", func(t *testing.T) {
		drv := newCapturingDB()
		drv.err = errors.New("disk full")
		require.NotPanics(t, func() { RecordDelivery(ctx, newNullHost(drv), row) })
		require.Len(t, drv.writes, 1)
	})
}

// TestRecordDeliveryStampsTenantFromMember is the batch-flush contract: the
// timer goroutine has no request context, so the tenant has to come off the
// member or the driver fails closed on the scoped collection.
func TestRecordDeliveryStampsTenantFromMember(t *testing.T) {
	drv := newCapturingDB()
	host := newNullHost(drv)

	member := testMember("a1", "h1", "page-oncall", "n-1")
	member.Tenant = "acme"
	RecordDelivery(context.Background(), host, DeliveryRow{
		Status:  DeliveryStatusSuccess,
		Batch:   true,
		Members: []DeliveryMember{member},
	})

	require.Len(t, drv.writes, 1)
	got, ok := auth.TenantFrom(drv.writes[0].ctx)
	require.True(t, ok)
	require.Equal(t, "acme", got)
}

// TestRecordDeliveryKeepsCtxTenant checks the dispatcher path: a context that
// already carries a tenant is never re-stamped from the member.
func TestRecordDeliveryKeepsCtxTenant(t *testing.T) {
	drv := newCapturingDB()
	host := newNullHost(drv)

	member := testMember("a1", "h1", "page-oncall", "n-1")
	member.Tenant = "other"
	RecordDelivery(auth.WithTenant(context.Background(), "acme"), host, DeliveryRow{
		Status:  DeliveryStatusSuccess,
		Members: []DeliveryMember{member},
	})

	require.Len(t, drv.writes, 1)
	got, _ := auth.TenantFrom(drv.writes[0].ctx)
	require.Equal(t, "acme", got)
}

func TestErrBatchedIsSentinel(t *testing.T) {
	require.True(t, errors.Is(ErrBatched, ErrBatched))
	require.ErrorIs(t, errors.Join(ErrBatched, errors.New("x")), ErrBatched)
}

// ---------------------------------------------------------------------------
// Shared batch/counter primitives (MembersTenant / BackfillMemberUIDs /
// BumpNotificationCounters) — the three notifiers and the dispatcher all go
// through these, so their behaviour is pinned here once.
// ---------------------------------------------------------------------------

// recordDB is a driver stub for the uid backfill and the counter bump. It
// answers GetOne from two tables keyed the way the real drivers are, counts the
// reads, and can be told to miss a hash a number of times before the record
// "lands" — the size-flush-races-writeRecord case.
type recordDB struct {
	db.Driver

	mu sync.Mutex
	// recordsByHash maps an alert hash to its stored uid.
	recordsByHash map[string]string
	// missesLeft[hash] delays a hash's first successful read.
	missesLeft map[string]int
	// notifications maps a notification uid to its stored document.
	notifications map[string]db.Document
	// setFields records every counter write, in order.
	setFields []db.Document

	getOnes int
	setErr  error
}

func newRecordDB() *recordDB {
	return &recordDB{
		recordsByHash: map[string]string{},
		missesLeft:    map[string]int{},
		notifications: map[string]db.Document{},
	}
}

func (d *recordDB) GetOne(_ context.Context, col string, match db.Document) (db.Document, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOnes++
	switch col {
	case recordCollection:
		hash, _ := match["hash"].(string)
		if d.missesLeft[hash] > 0 {
			d.missesLeft[hash]--
			return nil, db.ErrNotFound
		}
		uid, ok := d.recordsByHash[hash]
		if !ok {
			return nil, db.ErrNotFound
		}
		return db.Document{"uid": uid, "hash": hash}, nil
	case notificationCollection:
		uid, _ := match["uid"].(string)
		doc, ok := d.notifications[uid]
		if !ok {
			return nil, db.ErrNotFound
		}
		return doc, nil
	}
	return nil, db.ErrNotFound
}

func (d *recordDB) SetFields(_ context.Context, _ string, fields db.Document, _ condition.Cond) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.setErr != nil {
		return 0, d.setErr
	}
	d.setFields = append(d.setFields, fields)
	return 1, nil
}

func (d *recordDB) counterWrites() []db.Document {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]db.Document, len(d.setFields))
	copy(out, d.setFields)
	return out
}

func (d *recordDB) reads() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getOnes
}

func TestMembersTenant(t *testing.T) {
	require.Empty(t, MembersTenant(nil))
	require.Empty(t, MembersTenant([]DeliveryMember{{}, {}}))
	require.Equal(t, "acme", MembersTenant([]DeliveryMember{{}, {Tenant: "acme"}, {Tenant: "globex"}}),
		"buckets are keyed by tenant, so the first non-empty is the bucket's tenant")
}

func TestBackfillMemberUIDs(t *testing.T) {
	drv := newRecordDB()
	drv.recordsByHash["h1"] = "a1"
	drv.recordsByHash["h2"] = "a2"
	host := newNullHost(drv)

	members := []DeliveryMember{
		{UID: "already", Hash: "h1"}, // untouched
		{Hash: "h1"},
		{Hash: "h1"}, // same hash: one read for both
		{Hash: "h2"},
		{Hash: "h-unknown"}, // never lands
		{},                  // nothing to key on
	}
	BackfillMemberUIDs(context.Background(), host, members)

	require.Equal(t, "already", members[0].UID)
	require.Equal(t, "a1", members[1].UID)
	require.Equal(t, "a1", members[2].UID)
	require.Equal(t, "a2", members[3].UID)
	require.Empty(t, members[4].UID, "an unresolvable hash leaves the uid empty, not blank-filled")
	require.Empty(t, members[5].UID)
}

// TestBackfillMemberUIDsRetriesUntilTheRecordLands is the size-flush race: the
// flush runs on the dispatcher's own goroutine, so the record it is looking for
// may still be mid-write. A single GetOne would degrade that member to a
// hash-only link.
func TestBackfillMemberUIDsRetriesUntilTheRecordLands(t *testing.T) {
	drv := newRecordDB()
	drv.recordsByHash["h1"] = "a1"
	drv.missesLeft["h1"] = 3 // three ErrNotFound, then it lands
	host := newNullHost(drv)

	members := []DeliveryMember{{Hash: "h1"}}
	BackfillMemberUIDs(context.Background(), host, members)
	require.Equal(t, "a1", members[0].UID)
	require.GreaterOrEqual(t, drv.reads(), 4)
}

// TestBackfillMemberUIDsHonoursTheContext keeps the retry bounded: a cancelled
// context stops the loop instead of spinning for the whole window.
func TestBackfillMemberUIDsHonoursTheContext(t *testing.T) {
	drv := newRecordDB() // every hash misses forever
	host := newNullHost(drv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	members := []DeliveryMember{{Hash: "h-never"}}
	done := make(chan struct{})
	go func() {
		BackfillMemberUIDs(ctx, host, members)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("BackfillMemberUIDs ignored its context")
	}
	require.Empty(t, members[0].UID)
}

// backfillCountingDriver answers every record read with db.ErrNotFound and
// counts the calls. It is the "records never land" worst case the read budget
// exists for.
type backfillCountingDriver struct {
	db.Driver

	mu    sync.Mutex
	reads int
}

func (d *backfillCountingDriver) GetOne(_ context.Context, _ string, _ db.Document) (db.Document, error) {
	d.mu.Lock()
	d.reads++
	d.mu.Unlock()
	return nil, db.ErrNotFound
}

func (d *backfillCountingDriver) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reads
}

// TestBackfillMemberUIDsCapsTotalReads pins the fan-out ceiling (N5).
// batch_maxsize is operator-configured, so a bucket of never-landing hashes
// used to fire (rounds × bucket size) point reads inside the window. The cap
// is absolute: it is spent inside the FIRST round here, since there are more
// distinct hashes than maxBackfillReads.
func TestBackfillMemberUIDsCapsTotalReads(t *testing.T) {
	drv := &backfillCountingDriver{}
	host := newNullHost(drv)

	members := make([]DeliveryMember, 0, maxBackfillReads+50)
	for i := range maxBackfillReads + 50 {
		members = append(members, DeliveryMember{Hash: fmt.Sprintf("h-%d", i)})
	}

	start := time.Now()
	BackfillMemberUIDs(context.Background(), host, members)
	elapsed := time.Since(start)

	require.Equal(t, maxBackfillReads, drv.count(),
		"the read budget is a hard ceiling, whatever the bucket size")
	require.Less(t, elapsed, deliveryBackfillWindow,
		"the budget is spent in the first round, so there is no sleeping at all")
	for i := range members {
		require.Empty(t, members[i].UID,
			"a member the budget never reached keeps its hash, same as one that never resolves")
	}
}

// TestBackfillMemberUIDsBacksOffGeometrically is the other half of N5: with a
// fixed 20ms retry a single never-landing hash was re-read ~25 times inside the
// 500ms window. The 20→40→80… backoff brings that down to a handful of rounds
// while keeping the same total latency budget.
func TestBackfillMemberUIDsBacksOffGeometrically(t *testing.T) {
	drv := &backfillCountingDriver{}
	host := newNullHost(drv)

	members := []DeliveryMember{{Hash: "h-never"}}
	start := time.Now()
	BackfillMemberUIDs(context.Background(), host, members)
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, drv.count(), 3, "it must still retry, not read once")
	require.LessOrEqual(t, drv.count(), 10,
		"a fixed 20ms retry would be ~25 rounds; the geometric backoff is ~6")
	require.Less(t, elapsed, 3*deliveryBackfillWindow,
		"the window still bounds the whole call")
	require.Empty(t, members[0].UID)
}

// backfillCancellingDriver misses like backfillCountingDriver but cancels the
// backfill's own context on its FIRST read, standing in for a flush whose
// DeliveryWriteTimeout expired (or a shutdown drain) mid-backfill.
type backfillCancellingDriver struct {
	db.Driver

	cancel context.CancelFunc

	mu    sync.Mutex
	reads int
}

func (d *backfillCancellingDriver) GetOne(_ context.Context, _ string, _ db.Document) (db.Document, error) {
	d.mu.Lock()
	d.reads++
	first := d.reads == 1
	d.mu.Unlock()
	if first {
		d.cancel()
	}
	return nil, db.ErrNotFound
}

func (d *backfillCancellingDriver) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reads
}

// TestBackfillMemberUIDsStopsOnCancelledContext pins that ctx cancellation is
// honoured INSIDE a round, not only at the sleep between rounds. The budget is
// spent in the first round when there are more hashes than maxBackfillReads, so
// checking ctx only in the timer select meant a cancelled context still cost
// the driver up to 200 point reads it could never satisfy.
func TestBackfillMemberUIDsStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drv := &backfillCancellingDriver{cancel: cancel}
	host := newNullHost(drv)

	members := make([]DeliveryMember, 0, maxBackfillReads+50)
	for i := range maxBackfillReads + 50 {
		members = append(members, DeliveryMember{Hash: fmt.Sprintf("h-%d", i)})
	}

	BackfillMemberUIDs(ctx, host, members)

	require.LessOrEqual(t, drv.count(), 2,
		"the cancellation must stop the round, not let the whole read budget drain")
	require.GreaterOrEqual(t, drv.count(), 1, "it should have tried once")
	for i := range members {
		require.Empty(t, members[i].UID, "an unresolved member keeps its hash")
	}
}

func TestBumpNotificationCounters(t *testing.T) {
	drv := newRecordDB()
	drv.notifications["n-1"] = db.Document{"uid": "n-1", "hits": float64(41)}
	drv.notifications["n-2"] = db.Document{"uid": "n-2"} // never bumped before
	host := newNullHost(drv)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	before := time.Now().Unix()
	BumpNotificationCounters(ctx, host, []DeliveryMember{
		{NotificationUID: "n-1"},
		{NotificationUID: "n-1"}, // second action of the same notification
		{NotificationUID: "n-2"},
		{}, // no uid: skipped
	})

	writes := drv.counterWrites()
	require.Len(t, writes, 2, "once per DISTINCT notification, not per member")
	require.Equal(t, int64(42), writes[0]["hits"])
	require.Equal(t, int64(1), writes[1]["hits"], "an absent counter starts at zero")
	for _, w := range writes {
		last, ok := w["last_sent"].(int64)
		require.True(t, ok)
		require.GreaterOrEqual(t, last, before)
		require.Len(t, w, 2, "hits and last_sent go out together so the Mongo watcher can drop the update")
	}
}

// TestBumpNotificationCountersNeverCreatesAGhost is the reason this is a
// read-modify-write and not an upserting increment: a notification deleted
// between dispatch and flush must not be resurrected as {uid, hits}.
func TestBumpNotificationCountersNeverCreatesAGhost(t *testing.T) {
	drv := newRecordDB() // no notifications at all
	host := newNullHost(drv)

	BumpNotificationCounters(auth.WithTenant(context.Background(), snoozetypes.DefaultTenant),
		host, []DeliveryMember{{NotificationUID: "deleted"}})

	require.Empty(t, drv.counterWrites(), "a vanished notification is not an error and not a new row")
}

// TestBumpNotificationCountersStampsTenantFromMember mirrors RecordDelivery:
// the batch flush has no request context, and the notification collection is
// tenant-scoped.
func TestBumpNotificationCountersStampsTenantFromMember(t *testing.T) {
	drv := newRecordDB()
	drv.notifications["n-1"] = db.Document{"uid": "n-1"}
	host := newNullHost(drv)

	BumpNotificationCounters(context.Background(), host,
		[]DeliveryMember{{NotificationUID: "n-1", Tenant: "acme"}})
	require.Len(t, drv.counterWrites(), 1)
}

func TestBumpNotificationCountersGuards(t *testing.T) {
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	require.NotPanics(t, func() { BumpNotificationCounters(ctx, nil, []DeliveryMember{{NotificationUID: "n"}}) })
	require.NotPanics(t, func() { BumpNotificationCounters(ctx, newNullHost(nil), []DeliveryMember{{NotificationUID: "n"}}) })
	require.NotPanics(t, func() { BumpNotificationCounters(ctx, newNullHost(newRecordDB()), nil) })

	drv := newRecordDB()
	drv.notifications["n-1"] = db.Document{"uid": "n-1"}
	drv.setErr = errors.New("disk full")
	require.NotPanics(t, func() {
		BumpNotificationCounters(ctx, newNullHost(drv), []DeliveryMember{{NotificationUID: "n-1"}})
	})
}

// TestDeliveryRowDocDedupesMembers pins the alerts[] / alert_count agreement:
// two notifications routing the same alert to one batching action queue it
// twice, and the row must not claim two alerts.
func TestDeliveryRowDocDedupesMembers(t *testing.T) {
	m1 := testMember("a1", "h1", "page-oncall", "n-1")
	dupUID := testMember("a1", "h1", "wake-sre", "n-2") // same alert, other notification
	hashOnly := testMember("", "h9", "page-oncall", "n-1")
	dupHash := testMember("", "h9", "page-oncall", "n-1")

	doc := DeliveryRow{
		Status:  DeliveryStatusSuccess,
		Batch:   true,
		Members: []DeliveryMember{m1, dupUID, hashOnly, dupHash},
	}.Doc()

	require.Equal(t, 2, doc["alert_count"], "two distinct alerts, four queued members")
	alerts, ok := doc["alerts"].([]any)
	require.True(t, ok)
	require.Len(t, alerts, 2)
	require.Equal(t, []any{"a1"}, doc["alert_uids"])
	require.Equal(t, []any{"h1", "h9"}, doc["alert_hashes"])
	// The FIRST occurrence wins, so the notification attribution of the kept
	// snapshot is the one that queued it.
	first, _ := alerts[0].(map[string]any)
	require.Equal(t, "page-oncall", first["notification"])
	// Both notifications are still credited by the flat arrays.
	require.Equal(t, []any{"n-1", "n-2"}, doc["notification_uids"])
}
