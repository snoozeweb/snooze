package comment

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
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
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

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
