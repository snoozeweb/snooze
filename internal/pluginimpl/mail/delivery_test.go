package mail

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	dbsqlite "github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// requireQueued asserts Send accepted the record into a batch bucket rather
// than delivering it: the outcome is reported later, by the flush.
func requireQueued(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, plugins.ErrBatched)
}

// --- delivery-log host ------------------------------------------------------

// logHost is a plugins.Host backed by a real SQLite driver, so the batch
// flush's delivery-row write and uid backfill read run against the same code
// path (and the same tenant fail-closed rules) production uses. nullHost, the
// other host in this package, has no DB at all.
type logHost struct {
	drv db.Driver
	cfg *config.Config
	// writer is nil unless a test opted into stats via withStats; RecordStat
	// short-circuits on a nil writer, so the default host stays stat-free.
	writer *asyncwriter.Writer
}

func (h *logHost) DB() db.Driver                    { return h.drv }
func (h *logHost) Bus() plugins.Bus                 { return nil }
func (h *logHost) Logger() *slog.Logger             { return slog.Default() }
func (h *logHost) Tracer() trace.Tracer             { return otel.Tracer("mail-test") }
func (h *logHost) Metrics() *telemetry.Registry     { return telemetry.NewRegistry(nil) }
func (h *logHost) Config() *config.Config           { return h.cfg }
func (h *logHost) Plugin(string) plugins.Plugin     { return nil }
func (h *logHost) AsyncWriter() *asyncwriter.Writer { return h.writer }

// incCaptureDriver records the counter increments plugins.RecordStat emits, so
// a flush's action_success / action_error accounting can be asserted.
type incCaptureDriver struct {
	db.Driver
	mu  sync.Mutex
	ops []capturedInc
}

type capturedInc struct {
	metric string
	name   string
	delta  int64
}

func (d *incCaptureDriver) BulkIncrement(ctx context.Context, collection string, ops []db.IncrementOp, upsert bool) error {
	d.mu.Lock()
	for _, op := range ops {
		metric, _ := op.Search["metric"].(string)
		name, _ := op.Search["key"].(string)
		for _, delta := range op.Deltas {
			d.ops = append(d.ops, capturedInc{metric: metric, name: name, delta: delta})
		}
	}
	d.mu.Unlock()
	return d.Driver.BulkIncrement(ctx, collection, ops, upsert)
}

func (d *incCaptureDriver) captured() []capturedInc {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]capturedInc, len(d.ops))
	copy(out, d.ops)
	return out
}

// withStats turns on metrics for h and routes RecordStat through a capturing
// driver. Call before PostInit.
func withStats(h *logHost) *incCaptureDriver {
	inc := &incCaptureDriver{Driver: h.drv}
	h.drv = inc
	h.cfg.General.MetricsEnabled = true
	h.writer = asyncwriter.New(inc, time.Hour, asyncwriter.NewMockClock(time.Unix(0, 0)),
		asyncwriter.WithUpsert(true))
	return inc
}

// statsFor flushes the async writer and returns the increments recorded for
// metric.
func statsFor(t *testing.T, h *logHost, inc *incCaptureDriver, metric string) []capturedInc {
	t.Helper()
	require.NoError(t, h.writer.Flush(context.Background()))
	var out []capturedInc
	for _, op := range inc.captured() {
		if op.metric == metric {
			out = append(out, op)
		}
	}
	return out
}

// seedNotification writes the notification entry the batch members point at so
// the flush's counter bump has a row to advance, and returns its
// server-assigned uid (the drivers treat a client-supplied uid as an update
// target, so it cannot be chosen up front).
func seedNotification(t *testing.T, h *logHost, tenant string) string {
	t.Helper()
	res, err := h.drv.Write(tctx(tenant), "notification",
		[]db.Document{{"name": "page-oncall"}}, db.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	return res.Added[0]
}

// batchPayloadFor is batchPayload attributed to a specific notification uid,
// for the tests that assert on that notification's counters.
func batchPayloadFor(meta map[string]any, notifUID string) plugins.NotificationPayload {
	p := batchPayload(meta)
	p.NotificationUID = notifUID
	return p
}

// notificationCounters reads back a notification's hits / last_sent.
func notificationCounters(t *testing.T, h *logHost, tenant, uid string) (int64, int64) {
	t.Helper()
	doc, err := h.drv.GetOne(tctx(tenant), "notification", db.Document{"uid": uid})
	require.NoError(t, err)
	var hits, last int64
	if v, ok := doc["hits"]; ok {
		hits = numOf(t, v)
	}
	if v, ok := doc["last_sent"]; ok {
		last = numOf(t, v)
	}
	return hits, last
}

func newLogHost(t *testing.T) *logHost {
	t.Helper()
	drv, err := dbsqlite.New(context.Background(), dbsqlite.Config{
		Path: filepath.Join(t.TempDir(), "snooze.db"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return &logHost{drv: drv, cfg: config.Default()}
}

// tctx scopes a context to a tenant. Every collection the flush touches is
// tenant-scoped, so a naked context would be rejected by the driver.
func tctx(tenant string) context.Context {
	return auth.WithTenant(context.Background(), tenant)
}

func deliveryRows(t *testing.T, h *logHost, tenant string) []db.Document {
	t.Helper()
	docs, _, err := h.drv.Search(tctx(tenant), plugins.NotificationLogCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	return docs
}

// waitForRows polls until the tenant has exactly n rows (the flush is async).
func waitForRows(t *testing.T, h *logHost, tenant string, n int) []db.Document {
	t.Helper()
	var docs []db.Document
	require.Eventually(t, func() bool {
		docs = deliveryRows(t, h, tenant)
		return len(docs) == n
	}, 3*time.Second, 10*time.Millisecond, "expected %d delivery row(s) for %s", n, tenant)
	return docs
}

func alertsOf(t *testing.T, row db.Document) []map[string]any {
	t.Helper()
	raw, ok := row["alerts"].([]any)
	require.True(t, ok, "alerts should be a list, got %T", row["alerts"])
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		m, ok := e.(map[string]any)
		require.True(t, ok, "alert entry should be an object, got %T", e)
		out = append(out, m)
	}
	return out
}

func stringsOf(t *testing.T, v any) []string {
	t.Helper()
	raw, ok := v.([]any)
	require.True(t, ok, "expected a list, got %T", v)
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		s, ok := e.(string)
		require.True(t, ok, "expected a string element, got %T", e)
		out = append(out, s)
	}
	return out
}

func numOf(t *testing.T, v any) int64 {
	t.Helper()
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	default:
		t.Fatalf("expected a number, got %T", v)
		return 0
	}
}

// batchPayload builds the payload the dispatcher would hand a batched mail
// action, including the two attribution fields the delivery row keys on.
func batchPayload(meta map[string]any) plugins.NotificationPayload {
	m := make(map[string]any, len(meta)+1)
	for k, v := range meta {
		m[k] = v
	}
	m["notification_name"] = "page-oncall"
	return plugins.NotificationPayload{Meta: m, NotificationUID: "notif-1"}
}

func alertRecord(host, message, hash string) snoozetypes.Record {
	return snoozetypes.Record{
		Host:     host,
		Message:  message,
		Hash:     hash,
		Severity: "critical",
		State:    "open",
	}
}

// --- tests ------------------------------------------------------------------

func TestBatchMail_DeliveryRowOnSizeFlush(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))

	meta := batchMailMeta(smtpHost, port, 2, 60, "mail-oncall")
	ctx := tctx("acme")
	requireQueued(t, p.Send(ctx, alertRecord("db-01", "disk 98%", "h1"), batchPayload(meta)))
	requireQueued(t, p.Send(ctx, alertRecord("db-02", "disk 99%", "h2"), batchPayload(meta)))

	rows := waitForRows(t, h, "acme", 1)
	row := rows[0]
	require.Equal(t, plugins.DeliveryStatusSuccess, row["status"])
	require.Equal(t, true, row["batch"])
	require.Equal(t, plugins.BatchReasonSize, row["batch_reason"])
	require.Equal(t, "mail-oncall", row["action"])
	require.Equal(t, "mail", row["notifier"])
	require.EqualValues(t, 2, numOf(t, row["alert_count"]))
	require.Equal(t, []string{"page-oncall"}, stringsOf(t, row["notification_names"]))
	require.Equal(t, []string{"notif-1"}, stringsOf(t, row["notification_uids"]))
	require.Equal(t, []string{"h1", "h2"}, stringsOf(t, row["alert_hashes"]))
	require.NotZero(t, numOf(t, row["queued_epoch"]))

	alerts := alertsOf(t, row)
	require.Len(t, alerts, 2)
	require.Equal(t, "db-01", alerts[0]["host"])
	require.Equal(t, "disk 98%", alerts[0]["message"])
	require.Equal(t, "db-02", alerts[1]["host"])
}

func TestBatchMail_DeliveryRowOnTimerFlush(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))

	meta := batchMailMeta(smtpHost, port, 100, 1, "mail-oncall") // size unreachable
	requireQueued(t, p.Send(tctx("acme"), alertRecord("db-01", "disk 98%", "h1"), batchPayload(meta)))

	rows := waitForRows(t, h, "acme", 1)
	require.Equal(t, plugins.BatchReasonTimer, rows[0]["batch_reason"])
	require.Equal(t, true, rows[0]["batch"])
	require.EqualValues(t, 1, numOf(t, rows[0]["alert_count"]))
}

func TestMailSendReturnsErrBatchedOnlyWhenBatching(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	require.NoError(t, p.PostInit(context.Background(), nullHost{}))

	batched := batchMailMeta(smtpHost, port, 5, 60, "mail-oncall")
	requireQueued(t, p.Send(tctx("acme"), alertRecord("db-01", "boom", "h1"), batchPayload(batched)))

	immediate := batchMailMeta(smtpHost, port, 5, 60, "mail-oncall")
	delete(immediate, "batch")
	require.NoError(t, p.Send(tctx("acme"), alertRecord("db-01", "boom", "h1"), batchPayload(immediate)),
		"an unbatched send reports its own outcome")
}

func TestBatchMail_BucketsAreTenantScoped(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))

	// Same action name, same SMTP target, two tenants: a shared bucket would
	// put acme's alerts inside globex's email.
	meta := batchMailMeta(smtpHost, port, 2, 60, "mail-oncall")
	requireQueued(t, p.Send(tctx("acme"), alertRecord("acme-01", "acme boom", "a1"), batchPayload(meta)))
	requireQueued(t, p.Send(tctx("globex"), alertRecord("globex-01", "globex boom", "g1"), batchPayload(meta)))

	time.Sleep(50 * time.Millisecond)
	require.Empty(t, deliveryRows(t, h, "acme"), "one alert per tenant must not reach either bucket's threshold")
	require.Empty(t, deliveryRows(t, h, "globex"))

	requireQueued(t, p.Send(tctx("acme"), alertRecord("acme-02", "acme boom 2", "a2"), batchPayload(meta)))
	requireQueued(t, p.Send(tctx("globex"), alertRecord("globex-02", "globex boom 2", "g2"), batchPayload(meta)))

	acme := waitForRows(t, h, "acme", 1)
	require.Equal(t, []string{"a1", "a2"}, stringsOf(t, acme[0]["alert_hashes"]))
	globex := waitForRows(t, h, "globex", 1)
	require.Equal(t, []string{"g1", "g2"}, stringsOf(t, globex[0]["alert_hashes"]))
}

func TestBatchMail_DeliveryRowOnFailedFlush(t *testing.T) {
	srv := newStubSMTP(t, "RCPT") // the server refuses every recipient
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))

	meta := batchMailMeta(smtpHost, port, 2, 60, "mail-oncall")
	ctx := tctx("acme")
	requireQueued(t, p.Send(ctx, alertRecord("db-01", "boom", "h1"), batchPayload(meta)))
	requireQueued(t, p.Send(ctx, alertRecord("db-02", "boom", "h2"), batchPayload(meta)))

	rows := waitForRows(t, h, "acme", 1)
	require.Equal(t, plugins.DeliveryStatusError, rows[0]["status"])
	errText, _ := rows[0]["error"].(string)
	require.True(t, strings.Contains(errText, "550") || strings.Contains(errText, "rcpt refused"),
		"the row should carry the SMTP rejection, got %q", errText)
	require.EqualValues(t, 2, numOf(t, rows[0]["alert_count"]))
}

func TestBatchMail_DeliveryRowWhenNoRecipients(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))

	// A batched action can be saved with recipients that render empty; the
	// flush drops the message, which the history must show as a failure.
	meta := batchMailMeta(smtpHost, port, 2, 60, "mail-oncall")
	ctx := tctx("acme")
	requireQueued(t, p.Send(ctx, alertRecord("db-01", "boom", "h1"), batchPayload(meta)))
	// Empty the bucket's recipients behind the flush's back — the bucket
	// captured the config of the first queued record.
	p.bMu.Lock()
	for _, b := range p.buckets {
		b.cfg.to = ""
	}
	p.bMu.Unlock()
	requireQueued(t, p.Send(ctx, alertRecord("db-02", "boom", "h2"), batchPayload(meta)))

	rows := waitForRows(t, h, "acme", 1)
	require.Equal(t, plugins.DeliveryStatusError, rows[0]["status"])
	require.Contains(t, rows[0]["error"], "no recipients")
}

func TestBatchMail_BackfillsMemberUID(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))

	// The record lands after the dispatcher ran, so the queued member carries
	// only a hash. The flush must recover the uid the driver minted at write
	// time — which is exactly why the member had none to begin with.
	res, err := h.drv.Write(tctx("acme"), "record",
		[]db.Document{{"hash": "h1", "host": "db-01"}},
		db.WriteOptions{Primary: []string{"hash"}, UpdateTime: true})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	uid := res.Added[0]

	meta := batchMailMeta(smtpHost, port, 2, 60, "mail-oncall")
	ctx := tctx("acme")
	requireQueued(t, p.Send(ctx, alertRecord("db-01", "boom", "h1"), batchPayload(meta)))
	requireQueued(t, p.Send(ctx, alertRecord("db-02", "boom", "h-unknown"), batchPayload(meta)))

	rows := waitForRows(t, h, "acme", 1)
	require.Equal(t, []string{uid}, stringsOf(t, rows[0]["alert_uids"]),
		"the unresolvable hash must not contribute an empty uid")
	alerts := alertsOf(t, rows[0])
	require.Equal(t, uid, alerts[0]["uid"])
	require.Nil(t, alerts[1]["uid"], "an unresolved member keeps its hash only")
}

// --- flush accounting: stats, counters, test sends, shutdown ----------------

// TestBatchMail_RecordsOneStatPerMember pins the regression the ErrBatched
// contract introduced: before it, Send recorded action_success at queue time,
// once per alert. Reporting the outcome at flush must keep that cardinality.
func TestBatchMail_RecordsOneStatPerMember(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	inc := withStats(h)
	require.NoError(t, p.PostInit(context.Background(), h))

	meta := batchMailMeta(smtpHost, port, 3, 60, "mail-oncall")
	ctx := tctx("acme")
	requireQueued(t, p.Send(ctx, alertRecord("db-01", "boom", "h1"), batchPayload(meta)))
	requireQueued(t, p.Send(ctx, alertRecord("db-02", "boom", "h2"), batchPayload(meta)))
	requireQueued(t, p.Send(ctx, alertRecord("db-03", "boom", "h3"), batchPayload(meta)))
	waitForRows(t, h, "acme", 1)

	ok := statsFor(t, h, inc, "action_success")
	require.Len(t, ok, 1)
	require.Equal(t, "mail-oncall", ok[0].name)
	require.EqualValues(t, 3, ok[0].delta, "one per member, not one per flush")
	require.Empty(t, statsFor(t, h, inc, "action_error"))
}

func TestBatchMail_RecordsActionErrorOnFailure(t *testing.T) {
	srv := newStubSMTP(t, "RCPT")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	inc := withStats(h)
	require.NoError(t, p.PostInit(context.Background(), h))

	meta := batchMailMeta(smtpHost, port, 2, 60, "mail-oncall")
	ctx := tctx("acme")
	requireQueued(t, p.Send(ctx, alertRecord("db-01", "boom", "h1"), batchPayload(meta)))
	requireQueued(t, p.Send(ctx, alertRecord("db-02", "boom", "h2"), batchPayload(meta)))
	waitForRows(t, h, "acme", 1)

	bad := statsFor(t, h, inc, "action_error")
	require.Len(t, bad, 1)
	require.EqualValues(t, 2, bad[0].delta)
	require.Empty(t, statsFor(t, h, inc, "action_success"))
}

// TestBatchMail_BumpsNotificationCounters: `hits` / `last_sent` were only ever
// bumped from the dispatcher's own rows, so a batching action never advanced
// them at all.
func TestBatchMail_BumpsNotificationCounters(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))
	notifUID := seedNotification(t, h, "acme")

	before := time.Now().Unix()
	meta := batchMailMeta(smtpHost, port, 2, 60, "mail-oncall")
	ctx := tctx("acme")
	requireQueued(t, p.Send(ctx, alertRecord("db-01", "boom", "h1"), batchPayloadFor(meta, notifUID)))
	requireQueued(t, p.Send(ctx, alertRecord("db-02", "boom", "h2"), batchPayloadFor(meta, notifUID)))
	waitForRows(t, h, "acme", 1)

	hits, last := notificationCounters(t, h, "acme", notifUID)
	require.EqualValues(t, 1, hits, "one flush is one hit, whatever the member count")
	require.GreaterOrEqual(t, last, before)
}

func TestBatchMail_LeavesCountersAloneOnFailure(t *testing.T) {
	srv := newStubSMTP(t, "RCPT")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))
	notifUID := seedNotification(t, h, "acme")

	meta := batchMailMeta(smtpHost, port, 2, 60, "mail-oncall")
	ctx := tctx("acme")
	requireQueued(t, p.Send(ctx, alertRecord("db-01", "boom", "h1"), batchPayloadFor(meta, notifUID)))
	requireQueued(t, p.Send(ctx, alertRecord("db-02", "boom", "h2"), batchPayloadFor(meta, notifUID)))
	waitForRows(t, h, "acme", 1)

	hits, last := notificationCounters(t, h, "acme", notifUID)
	require.Zero(t, hits)
	require.Zero(t, last)
}

// TestMailTestSendBypassesTheBucket is the phantom-alert fix: POST
// /action/test on a batching action must deliver immediately, never join a
// live bucket, and leave no trace in the history.
func TestMailTestSendBypassesTheBucket(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	require.NoError(t, p.PostInit(context.Background(), h))
	notifUID := seedNotification(t, h, "acme")

	payload := batchPayloadFor(batchMailMeta(smtpHost, port, 5, 60, "mail-oncall"), notifUID)
	payload.Test = true

	require.NoError(t, p.Send(tctx("acme"), alertRecord("test-host", "probe", "h-test"), payload),
		"a test send reports its real outcome, never ErrBatched")

	p.bMu.Lock()
	buckets := len(p.buckets)
	p.bMu.Unlock()
	require.Zero(t, buckets, "a synthetic alert must never enter a live tenant bucket")

	time.Sleep(100 * time.Millisecond)
	require.Empty(t, deliveryRows(t, h, "acme"), "a probe is not a delivery")
	hits, _ := notificationCounters(t, h, "acme", notifUID)
	require.Zero(t, hits)
}

// blockingLogDB wedges the delivery-row write so a drain cannot complete.
type blockingLogDB struct {
	db.Driver
	release chan struct{}
}

func (d *blockingLogDB) Write(ctx context.Context, col string, docs []db.Document, opts db.WriteOptions) (db.WriteResult, error) {
	if col == plugins.NotificationLogCollection {
		<-d.release
		return db.WriteResult{}, context.Canceled
	}
	return d.Driver.Write(ctx, col, docs, opts)
}

// TestMailStopHonoursItsDeadline: Stop's ctx is the parent of every drain, so a
// wedged driver costs the shutdown its deadline, not the process.
func TestMailStopHonoursItsDeadline(t *testing.T) {
	srv := newStubSMTP(t, "")
	smtpHost, port := srv.hostPort()
	p := &Plugin{}
	h := newLogHost(t)
	blocking := &blockingLogDB{Driver: h.drv, release: make(chan struct{})}
	t.Cleanup(func() { close(blocking.release) })
	h.drv = blocking
	require.NoError(t, p.PostInit(context.Background(), h))

	meta := batchMailMeta(smtpHost, port, 100, 60, "mail-oncall") // never flushes on its own
	requireQueued(t, p.Send(tctx("acme"), alertRecord("db-01", "boom", "h1"), batchPayload(meta)))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- p.Stop(ctx) }()

	select {
	case err := <-done:
		require.NoError(t, err, "a slow drain is not a shutdown failure")
		require.Less(t, time.Since(start), 3*time.Second)
	case <-time.After(3 * time.Second):
		t.Fatal("Stop hung on a wedged delivery write")
	}
}
