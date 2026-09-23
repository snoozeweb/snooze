package comment

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

type testHost struct {
	drv *sqlite.Driver
	cfg *config.Config
	// notif is returned for Plugin("notification") so the manual-escalation
	// re-notify path can be observed.
	notif plugins.Plugin
}

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return &testHost{drv: drv}
}

func (h *testHost) DB() db.Driver        { return h.drv }
func (h *testHost) Bus() plugins.Bus     { return nil }
func (h *testHost) Logger() *slog.Logger { return slog.Default() }
func (h *testHost) Tracer() trace.Tracer { return otel.Tracer("comment-test") }
func (h *testHost) Metrics() *telemetry.Registry {
	return telemetry.NewRegistry(nil)
}
func (h *testHost) Config() *config.Config {
	if h.cfg != nil {
		return h.cfg
	}
	return config.Default()
}
func (h *testHost) Plugin(name string) plugins.Plugin {
	if name == "notification" {
		return h.notif
	}
	return nil
}

// recordingProcessor stands in for the notification plugin and records every
// record the comment plugin re-dispatches.
type recordingProcessor struct {
	mu   sync.Mutex
	recs []snoozetypes.Record
}

func (r *recordingProcessor) Name() string { return "notification" }
func (r *recordingProcessor) Metadata() plugins.Metadata {
	return plugins.Metadata{Name: "notification"}
}
func (r *recordingProcessor) PostInit(context.Context, plugins.Host) error { return nil }
func (r *recordingProcessor) Reload(context.Context) error                 { return nil }

func (r *recordingProcessor) Process(_ context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	r.mu.Lock()
	r.recs = append(r.recs, rec)
	r.mu.Unlock()
	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

func (r *recordingProcessor) Records() []snoozetypes.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]snoozetypes.Record, len(r.recs))
	copy(out, r.recs)
	return out
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "comment"))
}

func TestPostInitRoundtrip(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "comment"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, "comment", p.Name())
}

func TestValidate(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(nil))
	require.NoError(t, p.Validate(map[string]any{"record_uid": "r1", "message": "hi"}))
	require.Error(t, p.Validate(map[string]any{"record_uid": "r1", "message": ""}))
	require.Error(t, p.Validate(map[string]any{"record_uid": "", "message": "hi"}))
}

func TestTransformWriteStampsPrincipal(t *testing.T) {
	p := &Plugin{}
	ctx := auth.WithClaims(context.Background(),
		snoozetypes.Claims{Subject: "alice", Method: "local"})

	// A client-supplied `user` must be overridden by the authenticated subject.
	doc := map[string]any{"record_uid": "r1", "type": "ack", "message": "ok", "user": "spoofed"}
	require.NoError(t, p.TransformWrite(ctx, doc))
	require.Equal(t, "alice", doc["user"])
	require.Equal(t, "local", doc["method"])
}

func TestTransformWritePreservesSuppliedMethod(t *testing.T) {
	// Chat-ops bridges (teams/jira/mcp) post as a service account but set
	// `method` to the originating channel. The authenticated subject still
	// overwrites `user`, but the supplied channel must survive.
	p := &Plugin{}
	ctx := auth.WithClaims(context.Background(),
		snoozetypes.Claims{Subject: "snooze-bot", Method: "local"})

	doc := map[string]any{"record_uid": "r1", "type": "ack", "name": "alice via Teams", "method": "teams"}
	require.NoError(t, p.TransformWrite(ctx, doc))
	require.Equal(t, "snooze-bot", doc["user"]) // user is server-authoritative
	require.Equal(t, "teams", doc["method"])    // caller-supplied channel preserved
	require.Equal(t, "alice via Teams", doc["name"])
}

func TestTransformWriteNoClaimsIsNoop(t *testing.T) {
	p := &Plugin{}
	doc := map[string]any{"record_uid": "r1", "message": "hi"}
	require.NoError(t, p.TransformWrite(context.Background(), doc))
	_, hasUser := doc["user"]
	require.False(t, hasUser)
	_, hasMethod := doc["method"]
	require.False(t, hasMethod)
}

func TestTransformWriteEmptySubjectIsNoop(t *testing.T) {
	// Claims present but with an empty subject (malformed token) must not
	// stamp an empty user — that would let the comment masquerade as a real
	// user action in the dashboard feed.
	p := &Plugin{}
	ctx := auth.WithClaims(context.Background(), snoozetypes.Claims{Subject: "", Method: "local"})
	doc := map[string]any{"record_uid": "r1", "message": "hi"}
	require.NoError(t, p.TransformWrite(ctx, doc))
	_, hasUser := doc["user"]
	require.False(t, hasUser)
	_, hasMethod := doc["method"]
	require.False(t, hasMethod)
}

// guardCtx returns a tenant-scoped context: the `record` collection is
// tenant-scoped and fail-closed, so both seeding and the GuardWrite GetOne must
// carry a tenant.
func guardCtx() context.Context {
	return auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
}

// seedRecord writes a record with the given state and returns its assigned uid.
func seedRecord(t *testing.T, host *testHost, state string) string {
	t.Helper()
	res, err := host.DB().Write(guardCtx(), "record",
		[]db.Document{{"state": state}}, db.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	return res.Added[0]
}

func TestGuardWrite_BlocksDoubleAck(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	doc := map[string]any{"record_uid": uid, "type": "ack", "message": "ack again"}
	err := p.GuardWrite(guardCtx(), "", doc, false)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidTransition))
}

func TestGuardWrite_BlocksAckOfClosed(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "close")
	doc := map[string]any{"record_uid": uid, "type": "ack", "message": "ack a closed alert"}
	err := p.GuardWrite(guardCtx(), "", doc, false)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidTransition))
}

func TestGuardWrite_AllowsAckOfFresh(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "")
	doc := map[string]any{"record_uid": uid, "type": "ack", "message": "first ack"}
	require.NoError(t, p.GuardWrite(guardCtx(), "", doc, false))
}

func TestGuardWrite_NonTransitionCommentSkipsGuard(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	// Record is acked; a "note" is not a state-changing action, so it must
	// pass the guard regardless of the record's current state.
	uid := seedRecord(t, host, "ack")
	doc := map[string]any{"record_uid": uid, "type": "note", "message": "just a note"}
	require.NoError(t, p.GuardWrite(guardCtx(), "", doc, false))
}

func TestGuardWrite_MissingRecordUIDPassesThrough(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	// No record_uid key — the guard is a no-op for orphan comments.
	doc := map[string]any{"type": "ack", "message": "orphan ack"}
	require.NoError(t, p.GuardWrite(guardCtx(), "", doc, false))
}

// recordDoc fetches the persisted record so AfterCreate side effects can be
// asserted.
func recordDoc(t *testing.T, host *testHost, uid string) db.Document {
	t.Helper()
	rec, err := host.DB().GetOne(guardCtx(), "record", db.Document{"uid": uid})
	require.NoError(t, err)
	return rec
}

// asInt64 normalizes the loosely-typed numeric a backend may return.
func asInt64(t *testing.T, v any) int64 {
	t.Helper()
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	default:
		t.Fatalf("not numeric: %T %v", v, v)
		return 0
	}
}

// TestAfterCreate_AckStampsAckUntil verifies that posting an ack comment stamps
// ack_until = now + ack_timeout on the linked record and pauses escalation
// (escalate_at = 0). The clock and timeout are both injected/configured so the
// deadline math is deterministic.
func TestAfterCreate_AckStampsAckUntil(t *testing.T) {
	host := newTestHost(t)
	host.cfg = config.Default()
	host.cfg.Housekeeper.AckTimeout = schema.Duration(2 * time.Hour)

	now := time.Unix(1_000_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "")
	doc := map[string]any{"record_uid": uid, "type": "ack", "message": "ack it"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "ack", rec["state"])
	require.Equal(t, now.Add(2*time.Hour).Unix(), asInt64(t, rec["ack_until"]),
		"ack_until must be now + ack_timeout")
	// Escalation is paused while acked: escalate_at cleared to 0.
	if v, ok := rec["escalate_at"]; ok {
		require.Equal(t, int64(0), asInt64(t, v))
	}
}

// TestAfterCreate_OpenClearsAckUntil verifies that posting an open comment
// clears ack_until back to 0 (the ack is lifted). With escalate_after disabled
// (default 0) escalate_at stays 0.
func TestAfterCreate_OpenClearsAckUntil(t *testing.T) {
	host := newTestHost(t)

	now := time.Unix(2_000_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	// Start acked so the open transition is legal and ack_until is non-zero.
	uid := seedRecord(t, host, "ack")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{"ack_until": int64(99999)}, false))

	doc := map[string]any{"record_uid": uid, "type": "open", "message": "reopen"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "open", rec["state"])
	require.Equal(t, int64(0), asInt64(t, rec["ack_until"]), "open must clear ack_until")
	// escalate_after defaults to 0 (disabled) → escalate_at not armed.
	if v, ok := rec["escalate_at"]; ok {
		require.Equal(t, int64(0), asInt64(t, v))
	}
}

// TestAfterCreate_ShelveStampsShelveUntil verifies that posting a shelve comment
// transitions the record to "shelved" AND stamps shelve_until = now +
// shelve_timeout. The clock and timeout are both injected/configured so the
// deadline math is deterministic.
func TestAfterCreate_ShelveStampsShelveUntil(t *testing.T) {
	host := newTestHost(t)
	host.cfg = config.Default()
	host.cfg.Housekeeper.ShelveTimeout = schema.Duration(3 * time.Hour)

	now := time.Unix(1_500_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "open")
	doc := map[string]any{"record_uid": uid, "type": "shelve", "message": "noisy"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "shelved", rec["state"], "shelve must transition state to shelved")
	require.Equal(t, now.Add(3*time.Hour).Unix(), asInt64(t, rec["shelve_until"]),
		"shelve_until must be now + shelve_timeout")
}

// TestAfterCreate_ShelveHonoursDuration verifies that an operator-chosen
// `duration` (seconds, as the shelve dialog posts it) wins over the configured
// housekeeping.shelve_timeout: shelve_until must be now + duration. The value
// arrives as a float64 because that is what encoding/json decodes a JSON number
// into on the real create path.
func TestAfterCreate_ShelveHonoursDuration(t *testing.T) {
	host := newTestHost(t)
	host.cfg = config.Default()
	host.cfg.Housekeeper.ShelveTimeout = schema.Duration(3 * time.Hour)

	now := time.Unix(1_600_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "open")
	doc := map[string]any{
		"record_uid": uid, "type": "shelve", "message": "noisy",
		"duration": float64(86400),
	}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "shelved", rec["state"])
	require.Equal(t, now.Add(24*time.Hour).Unix(), asInt64(t, rec["shelve_until"]),
		"shelve_until must be now + the chosen duration, not the configured timeout")
}

// TestAfterCreate_ShelveZeroDurationFallsBack verifies the fallback: a shelve
// comment carrying duration==0 (the "use the server default" sentinel the web
// client sends when no explicit window was picked) is stamped from the
// configured housekeeping.shelve_timeout, exactly as a comment with no
// duration field at all.
func TestAfterCreate_ShelveZeroDurationFallsBack(t *testing.T) {
	host := newTestHost(t)
	host.cfg = config.Default()
	host.cfg.Housekeeper.ShelveTimeout = schema.Duration(3 * time.Hour)

	now := time.Unix(1_700_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "open")
	doc := map[string]any{
		"record_uid": uid, "type": "shelve", "duration": float64(0),
	}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, now.Add(3*time.Hour).Unix(), asInt64(t, rec["shelve_until"]),
		"duration==0 must fall back to the configured shelve_timeout")
}

// TestValidate_Duration covers the duration guard: a negative window would
// stamp shelve_until in the past (the sweep would revert the alert on its next
// tick), and a non-numeric one would be silently read as zero. Both are
// rejected; zero, positive and absent all pass, and no upper bound is enforced
// — a very long shelve is a legitimate operator choice.
func TestValidate_Duration(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(map[string]any{"record_uid": "r1", "type": "shelve"}))
	require.NoError(t, p.Validate(map[string]any{"record_uid": "r1", "duration": float64(0)}))
	require.NoError(t, p.Validate(map[string]any{"record_uid": "r1", "duration": float64(86400)}))
	require.NoError(t, p.Validate(map[string]any{
		"record_uid": "r1", "duration": float64(365 * 24 * 3600),
	}), "no upper bound: a year-long shelve is allowed")
	require.Error(t, p.Validate(map[string]any{"record_uid": "r1", "duration": float64(-1)}))
	require.Error(t, p.Validate(map[string]any{"record_uid": "r1", "duration": "4h"}))
}

// TestAfterCreate_OpenClearsShelveUntil verifies that posting an open comment on
// a shelved record clears shelve_until back to 0 (the timed shelve is lifted).
func TestAfterCreate_OpenClearsShelveUntil(t *testing.T) {
	host := newTestHost(t)

	now := time.Unix(2_500_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	// Start shelved with a non-zero shelve_until so the open transition's clear
	// is observable.
	uid := seedRecord(t, host, "shelved")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{"shelve_until": int64(99999)}, false))

	doc := map[string]any{"record_uid": uid, "type": "open", "message": "reopen"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "open", rec["state"])
	require.Equal(t, int64(0), asInt64(t, rec["shelve_until"]), "open must clear shelve_until")
}

// seedAckedBy stamps acked_by onto an existing record so the clear/preserve
// paths can be exercised from a record that was already acknowledged.
func seedAckedBy(t *testing.T, host *testHost, uid, who string) {
	t.Helper()
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{"acked_by": who}, false))
	rec := recordDoc(t, host, uid)
	require.Equal(t, who, rec["acked_by"], "seed precondition")
}

// TestAfterCreate_AckedByStampedOnAck verifies that an ack comment carrying a
// resolved user stamps acked_by = user onto the linked record (denormalised
// convenience for the alert-list "Acked by" column).
func TestAfterCreate_AckedByStampedOnAck(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "")
	doc := map[string]any{"record_uid": uid, "type": "ack", "user": "alice", "message": "ack it"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "ack", rec["state"])
	require.Equal(t, "alice", rec["acked_by"], "ack must stamp acked_by = user")
}

// TestAfterCreate_AckedByClearedOnOpen verifies that re-opening an acknowledged
// alert truly removes the acked_by key (UnsetFields, not an empty string) so
// EXISTS/omitempty semantics hold.
func TestAfterCreate_AckedByClearedOnOpen(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	seedAckedBy(t, host, uid, "alice")

	doc := map[string]any{"record_uid": uid, "type": "open", "message": "reopen"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "open", rec["state"])
	_, has := rec["acked_by"]
	require.False(t, has, "open must remove the acked_by key entirely")
}

// TestAfterCreate_AckedByClearedOnClose verifies the close path also removes
// the acked_by key.
func TestAfterCreate_AckedByClearedOnClose(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	seedAckedBy(t, host, uid, "alice")

	doc := map[string]any{"record_uid": uid, "type": "close", "message": "resolved"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "close", rec["state"])
	_, has := rec["acked_by"]
	require.False(t, has, "close must remove the acked_by key entirely")
}

// TestAfterCreate_AckedByPreservedOnEsc verifies that re-escalating keeps the
// last acknowledger for accountability (Alerta clears on open but is silent on
// esc; Snooze preserves it).
func TestAfterCreate_AckedByPreservedOnEsc(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	seedAckedBy(t, host, uid, "alice")

	doc := map[string]any{"record_uid": uid, "type": "esc", "message": "fired again"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "esc", rec["state"])
	require.Equal(t, "alice", rec["acked_by"], "esc must preserve acked_by")
}

// TestAfterCreate_AckedByFreeCommentNoChange verifies a free-form comment
// (no transition type) leaves acked_by untouched.
func TestAfterCreate_AckedByFreeCommentNoChange(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	seedAckedBy(t, host, uid, "alice")

	doc := map[string]any{"record_uid": uid, "type": "", "user": "bob", "message": "just a note"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "alice", rec["acked_by"], "free comment must not touch acked_by")
}

// TestAfterCreate_AckedByEmptyUserAck verifies the auto-comment path (ack with
// no resolved user, e.g. from the aggregate-rule processor that bypasses
// TransformWrite) does not stamp an empty acked_by — omitempty/EXISTS stays
// clean and the column renders "—".
func TestAfterCreate_AckedByEmptyUserAck(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "")
	doc := map[string]any{"record_uid": uid, "type": "ack", "user": "", "message": "auto ack"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "ack", rec["state"])
	_, has := rec["acked_by"]
	require.False(t, has, "empty-user ack must not stamp acked_by")
}

// TestAfterCreate_EscStampsEscalationContext locks in the manual-escalation
// producer: an operator escalating from the UI or a chat command must leave the
// record carrying a count, a reason and the acting login, so the notifiers the
// re-dispatch reaches can update the ticket / thread they already created.
func TestAfterCreate_EscStampsEscalationContext(t *testing.T) {
	host := newTestHost(t)

	now := time.Unix(3_000_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	doc := map[string]any{"record_uid": uid, "type": "esc", "message": "still broken", "user": "alice"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "esc", rec["state"])
	require.Equal(t, int64(1), asInt64(t, rec["escalation_count"]))
	require.Equal(t, now.Unix(), asInt64(t, rec["escalated_at"]))
	require.Equal(t, "manual", rec["escalation_reason"])
	require.Equal(t, "alice", rec["escalation_actor"])
}

// A second manual escalation must increment rather than reset, so a notifier
// can render "escalation #2" and an operator can see the alert is repeating.
func TestAfterCreate_EscIncrementsExistingCount(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{clock: func() time.Time { return time.Unix(3_000_100, 0).UTC() }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{"escalation_count": 4}, false))

	doc := map[string]any{"record_uid": uid, "type": "esc", "message": "again"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	require.Equal(t, int64(5), asInt64(t, recordDoc(t, host, uid)["escalation_count"]))
}

// Close is terminal: the next occurrence of this alert is a new incident, so it
// must reach the notifiers as a first delivery rather than commenting on the
// ticket that was just resolved.
func TestAfterCreate_CloseResetsEscalationContext(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{clock: func() time.Time { return time.Unix(3_000_200, 0).UTC() }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "esc")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid, db.Document{
		"escalation_count":  3,
		"escalated_at":      int64(123),
		"escalation_reason": "timeout",
		"escalation_actor":  "bob",
	}, false))

	doc := map[string]any{"record_uid": uid, "type": "close", "message": "fixed"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, int64(0), asInt64(t, rec["escalation_count"]))
	require.Equal(t, int64(0), asInt64(t, rec["escalated_at"]))
	require.Empty(t, rec["escalation_reason"])
	require.Empty(t, rec["escalation_actor"])
}

// TestAfterCreate_EscReNotifies is the point of the manual-escalation work:
// before this, "escalate" from the UI or a Teams command changed a state field
// and reached no output plugin at all.
func TestAfterCreate_EscReNotifies(t *testing.T) {
	host := newTestHost(t)
	notif := &recordingProcessor{}
	host.notif = notif

	now := time.Unix(3_000_300, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{"host": "myhost01", "message": "disk full"}, false))

	doc := map[string]any{"record_uid": uid, "type": "esc", "message": "page again", "user": "alice"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	recs := notif.Records()
	require.Len(t, recs, 1, "an esc comment must re-fire the dispatcher exactly once")
	// The dispatcher must see the post-transition record, escalation stamp and
	// all — that is what makes the notifier comment instead of creating.
	require.Equal(t, "esc", recs[0].State)
	require.Equal(t, uid, recs[0].UID)
	require.Equal(t, "myhost01", recs[0].Host)
	require.Equal(t, 1, recs[0].EscalationCount)
	require.Equal(t, "manual", recs[0].EscalationReason)
	require.Equal(t, "alice", recs[0].EscalationActor)
}

// Every other transition must leave notification volume exactly as it was.
func TestAfterCreate_NonEscDoesNotReNotify(t *testing.T) {
	for _, ctype := range []string{"ack", "open", "close", "shelve", "unshelve", "comment"} {
		t.Run(ctype, func(t *testing.T) {
			host := newTestHost(t)
			notif := &recordingProcessor{}
			host.notif = notif

			p := &Plugin{clock: func() time.Time { return time.Unix(3_000_400, 0).UTC() }}
			require.NoError(t, p.PostInit(guardCtx(), host))

			// Start from a state each transition is legal from.
			from := "open"
			if ctype == "open" || ctype == "unshelve" {
				from = "ack"
			}
			uid := seedRecord(t, host, from)
			doc := map[string]any{"record_uid": uid, "type": ctype, "message": "x"}
			require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

			require.Empty(t, notif.Records(), "%s must not re-notify", ctype)
		})
	}
}

// A notifier blowing up must not fail the operator's comment write, which has
// already landed and is what their action was about.
func TestAfterCreate_EscSurvivesNotifierError(t *testing.T) {
	host := newTestHost(t)
	host.notif = &failingProcessor{}

	p := &Plugin{clock: func() time.Time { return time.Unix(3_000_500, 0).UTC() }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	doc := map[string]any{"record_uid": uid, "type": "esc", "message": "x"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}),
		"a notifier failure must not fail the comment write")
	require.Equal(t, "esc", recordDoc(t, host, uid)["state"])
}

// A host with no notification plugin registered (optional-plugin filtering,
// tests) must be a silent no-op rather than a nil dereference.
func TestAfterCreate_EscWithoutNotificationPlugin(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{clock: func() time.Time { return time.Unix(3_000_600, 0).UTC() }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	doc := map[string]any{"record_uid": uid, "type": "esc", "message": "x"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))
	require.Equal(t, "esc", recordDoc(t, host, uid)["state"])
}

type failingProcessor struct{}

func (f *failingProcessor) Name() string                                 { return "notification" }
func (f *failingProcessor) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: "notification"} }
func (f *failingProcessor) PostInit(context.Context, plugins.Host) error { return nil }
func (f *failingProcessor) Reload(context.Context) error                 { return nil }
func (f *failingProcessor) Process(context.Context, snoozetypes.Record) (plugins.Result, error) {
	return plugins.Result{}, errors.New("boom")
}

// --- ownership ---

// ownerCtx is guardCtx carrying the JWT claims of an authenticated operator,
// which is where owner_method comes from.
func ownerCtx(subject, method string) context.Context {
	return auth.WithClaims(guardCtx(), snoozetypes.Claims{Subject: subject, Method: method})
}

// seedOwned stamps an owner onto an existing record.
func seedOwned(t *testing.T, host *testHost, uid, owner, method string) {
	t.Helper()
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid, db.Document{
		"owner": owner, "owner_method": method, "owner_since": int64(10),
	}, false))
}

// seedUser writes a user document into the default tenant.
func seedUser(t *testing.T, host *testHost, name, method string, enabled bool) {
	t.Helper()
	_, err := host.DB().Write(guardCtx(), "user",
		[]db.Document{{"name": name, "method": method, "enabled": enabled}}, db.WriteOptions{})
	require.NoError(t, err)
}

// An ack takes ownership for the caller. The method comes from the JWT claims,
// not the comment's `method`, which a chat-ops bridge overrides with the
// channel name.
func TestAfterCreate_AckTakesOwnership(t *testing.T) {
	host := newTestHost(t)
	now := time.Unix(4_000_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{"previous_owner": "bob", "previous_owner_method": "local"}, false))

	ctx := ownerCtx("alice", "ldap")
	doc := map[string]any{"record_uid": uid, "type": "ack", "message": "mine", "method": "teams"}
	require.NoError(t, p.TransformWrite(ctx, doc))
	require.NoError(t, p.AfterCreate(ctx, []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "alice", rec["owner"])
	require.Equal(t, "ldap", rec["owner_method"], "owner_method is the caller's auth method, not the channel")
	require.Equal(t, now.Unix(), asInt64(t, rec["owner_since"]))
	require.Equal(t, "", rec["previous_owner"], "taking ownership resets the ghost")
	require.Equal(t, "", rec["previous_owner_method"])
	require.Equal(t, "alice", rec["acked_by"], "acked_by is unchanged (D7)")
}

func TestAfterCreate_CloseTakesOwnership(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{clock: func() time.Time { return time.Unix(4_000_100, 0).UTC() }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "esc")
	seedOwned(t, host, uid, "bob", "local")

	ctx := ownerCtx("alice", "local")
	doc := map[string]any{"record_uid": uid, "type": "close", "message": "fixed"}
	require.NoError(t, p.TransformWrite(ctx, doc))
	require.NoError(t, p.AfterCreate(ctx, []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "alice", rec["owner"], "whoever closes the alert owns it")
	require.Equal(t, "", rec["previous_owner"])
}

// A user-less ack (an auto-comment bypassing TransformWrite) takes nothing,
// exactly like acked_by.
func TestAfterCreate_UserlessAckTakesNothing(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "")
	doc := map[string]any{"record_uid": uid, "type": "ack", "message": "auto"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	_, has := recordDoc(t, host, uid)["owner"]
	require.False(t, has)
}

func TestAfterCreate_OpenAndEscClearOwnership(t *testing.T) {
	for _, ctype := range []string{"open", "esc"} {
		t.Run(ctype, func(t *testing.T) {
			host := newTestHost(t)
			p := &Plugin{clock: func() time.Time { return time.Unix(4_000_200, 0).UTC() }}
			require.NoError(t, p.PostInit(guardCtx(), host))

			uid := seedRecord(t, host, "ack")
			seedOwned(t, host, uid, "alice", "ldap")

			ctx := ownerCtx("bob", "local")
			doc := map[string]any{"record_uid": uid, "type": ctype, "message": "x"}
			require.NoError(t, p.TransformWrite(ctx, doc))
			require.NoError(t, p.AfterCreate(ctx, []map[string]any{doc}))

			rec := recordDoc(t, host, uid)
			require.Equal(t, "", rec["owner"], "cleared with an explicit empty, never unset")
			require.Equal(t, "", rec["owner_method"])
			require.Equal(t, int64(0), asInt64(t, rec["owner_since"]))
			require.Equal(t, "alice", rec["previous_owner"])
			require.Equal(t, "ldap", rec["previous_owner_method"])
		})
	}
}

// Free comments and the shelve pair leave ownership alone.
func TestAfterCreate_OwnershipUnchangedByOtherTypes(t *testing.T) {
	for _, ctype := range []string{"comment", "shelve", "unshelve"} {
		t.Run(ctype, func(t *testing.T) {
			host := newTestHost(t)
			p := &Plugin{}
			require.NoError(t, p.PostInit(guardCtx(), host))

			uid := seedRecord(t, host, "ack")
			seedOwned(t, host, uid, "alice", "ldap")

			ctx := ownerCtx("bob", "local")
			doc := map[string]any{"record_uid": uid, "type": ctype, "message": "x"}
			require.NoError(t, p.TransformWrite(ctx, doc))
			require.NoError(t, p.AfterCreate(ctx, []map[string]any{doc}))

			require.Equal(t, "alice", recordDoc(t, host, uid)["owner"])
		})
	}
}

func TestValidate_AssignRequiresAssignee(t *testing.T) {
	p := &Plugin{}
	require.Error(t, p.Validate(map[string]any{"record_uid": "r1", "type": "assign"}))
	require.Error(t, p.Validate(map[string]any{"record_uid": "r1", "type": "assign", "assignee": ""}))
	require.NoError(t, p.Validate(map[string]any{"record_uid": "r1", "type": "assign", "assignee": "bob"}))
	require.NoError(t, p.Validate(map[string]any{"record_uid": "r1", "type": "release"}))
}

func TestGuardWrite_Assign(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))
	seedUser(t, host, "bob", "local", true)
	seedUser(t, host, "carol", "local", false)
	seedUser(t, host, "dave", "local", true)
	seedUser(t, host, "dave", "ldap", true)

	open := seedRecord(t, host, "open")
	closed := seedRecord(t, host, "close")

	t.Run("fills the method of a unique login", func(t *testing.T) {
		doc := map[string]any{"record_uid": open, "type": "assign", "assignee": "bob"}
		require.NoError(t, p.GuardWrite(guardCtx(), "", doc, false))
		require.Equal(t, "local", doc["assignee_method"])
	})
	t.Run("explicit method disambiguates", func(t *testing.T) {
		doc := map[string]any{"record_uid": open, "type": "assign", "assignee": "dave", "assignee_method": "ldap"}
		require.NoError(t, p.GuardWrite(guardCtx(), "", doc, false))
		require.Equal(t, "ldap", doc["assignee_method"])
	})
	for name, doc := range map[string]map[string]any{
		"closed record":  {"record_uid": closed, "type": "assign", "assignee": "bob"},
		"unknown user":   {"record_uid": open, "type": "assign", "assignee": "nobody"},
		"disabled user":  {"record_uid": open, "type": "assign", "assignee": "carol"},
		"wrong method":   {"record_uid": open, "type": "assign", "assignee": "bob", "assignee_method": "oidc"},
		"ambiguous user": {"record_uid": open, "type": "assign", "assignee": "dave"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, p.GuardWrite(guardCtx(), "", doc, false))
		})
	}
	t.Run("closed record is an invalid transition", func(t *testing.T) {
		doc := map[string]any{"record_uid": closed, "type": "assign", "assignee": "bob"}
		require.True(t, errors.Is(p.GuardWrite(guardCtx(), "", doc, false), ErrInvalidTransition))
	})
}

// A user in another tenant is not a valid assignee.
func TestGuardWrite_AssignIsTenantScoped(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))
	_, err := host.DB().Write(auth.WithTenant(context.Background(), "other"), "user",
		[]db.Document{{"name": "eve", "method": "local", "enabled": true}}, db.WriteOptions{})
	require.NoError(t, err)

	uid := seedRecord(t, host, "open")
	doc := map[string]any{"record_uid": uid, "type": "assign", "assignee": "eve"}
	require.Error(t, p.GuardWrite(guardCtx(), "", doc, false))
}

func TestGuardWrite_Release(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{}
	require.NoError(t, p.PostInit(guardCtx(), host))

	unowned := seedRecord(t, host, "ack")
	owned := seedRecord(t, host, "ack")
	seedOwned(t, host, owned, "alice", "local")

	require.Error(t, p.GuardWrite(guardCtx(), "",
		map[string]any{"record_uid": unowned, "type": "release"}, false),
		"nothing to release on an unowned record")
	require.NoError(t, p.GuardWrite(guardCtx(), "",
		map[string]any{"record_uid": owned, "type": "release"}, false))
}

func TestAfterCreate_AssignTakesForAssignee(t *testing.T) {
	host := newTestHost(t)
	notif := &recordingProcessor{}
	host.notif = notif
	now := time.Unix(4_000_300, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	seedAckedBy(t, host, uid, "alice")
	seedOwned(t, host, uid, "alice", "local")

	ctx := ownerCtx("alice", "local")
	doc := map[string]any{"record_uid": uid, "type": "assign", "assignee": "bob", "assignee_method": "ldap"}
	require.NoError(t, p.TransformWrite(ctx, doc))
	require.NoError(t, p.AfterCreate(ctx, []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "ack", rec["state"], "assign is not a state transition")
	require.Equal(t, "bob", rec["owner"])
	require.Equal(t, "ldap", rec["owner_method"])
	require.Equal(t, now.Unix(), asInt64(t, rec["owner_since"]))
	require.Equal(t, "", rec["previous_owner"])
	require.Equal(t, "alice", rec["acked_by"], "acked_by is unchanged (D7)")
	require.Equal(t, int64(1), asInt64(t, rec["comment_count"]))
	require.Empty(t, notif.Records(), "assign does not notify")
}

// Releasing an acknowledged record returns it to open on the same clock as a
// manual open: ack expiry lifted, escalation re-armed, acked_by removed.
func TestAfterCreate_ReleaseOfAckedReopens(t *testing.T) {
	host := newTestHost(t)
	host.cfg = config.Default()
	host.cfg.Housekeeper.EscalateAfter = schema.Duration(30 * time.Minute)
	notif := &recordingProcessor{}
	host.notif = notif
	now := time.Unix(4_000_400, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{"ack_until": int64(99999)}, false))
	seedAckedBy(t, host, uid, "alice")
	seedOwned(t, host, uid, "alice", "local")

	ctx := ownerCtx("alice", "local")
	doc := map[string]any{"record_uid": uid, "type": "release", "message": "not mine"}
	require.NoError(t, p.TransformWrite(ctx, doc))
	require.NoError(t, p.AfterCreate(ctx, []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "open", rec["state"])
	require.Equal(t, int64(0), asInt64(t, rec["ack_until"]))
	require.Equal(t, now.Add(30*time.Minute).Unix(), asInt64(t, rec["escalate_at"]))
	require.Equal(t, "", rec["owner"])
	require.Equal(t, "alice", rec["previous_owner"])
	require.Equal(t, "local", rec["previous_owner_method"])
	_, has := rec["acked_by"]
	require.False(t, has, "releasing an ack unsets acked_by like a manual open")
	require.Empty(t, notif.Records(), "release does not notify")
}

func TestAfterCreate_ReleaseOfEscKeepsState(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{clock: func() time.Time { return time.Unix(4_000_500, 0).UTC() }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "esc")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{"escalate_at": int64(12345)}, false))
	seedAckedBy(t, host, uid, "alice")
	seedOwned(t, host, uid, "bob", "local")

	doc := map[string]any{"record_uid": uid, "type": "release"}
	require.NoError(t, p.AfterCreate(ownerCtx("bob", "local"), []map[string]any{doc}))

	rec := recordDoc(t, host, uid)
	require.Equal(t, "esc", rec["state"])
	require.Equal(t, int64(12345), asInt64(t, rec["escalate_at"]), "no state change, no timer change")
	require.Equal(t, "alice", rec["acked_by"])
	require.Equal(t, "", rec["owner"])
	require.Equal(t, "bob", rec["previous_owner"])
}

// The ownership types are not state transitions: ValidateTransition ignores
// them and they stay out of the validity table.
func TestOwnershipActionsAreNotTransitions(t *testing.T) {
	for _, a := range []string{"assign", "release"} {
		require.False(t, stateChangingActions[a])
		require.True(t, ownershipActions[a])
		for state := range allowedTransitions {
			require.NoError(t, ValidateTransition(state, a))
		}
	}
}
