package chataction

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/pluginimpl/comment"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// testHost is a minimal plugins.Host backed by a real SQLite driver and a real
// comment plugin, so Apply exercises the genuine GuardWrite + AfterCreate seam.
type testHost struct {
	drv     *sqlite.Driver
	cfg     *config.Config
	comment plugins.Plugin
}

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	h := &testHost{drv: drv}

	// Build and wire the real comment plugin so the create seam fires.
	cp, err := comment.New(plugins.Metadata{Name: "comment"})
	require.NoError(t, err)
	require.NoError(t, cp.PostInit(context.Background(), h))
	h.comment = cp
	return h
}

func (h *testHost) DB() db.Driver        { return h.drv }
func (h *testHost) Bus() plugins.Bus     { return nil }
func (h *testHost) Logger() *slog.Logger { return slog.Default() }
func (h *testHost) Tracer() trace.Tracer { return otel.Tracer("chataction-test") }
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
	if name == "comment" {
		return h.comment
	}
	return nil
}

// tenantCtx returns a tenant-scoped context: the `record` and `comment`
// collections are tenant-scoped and fail-closed, so seeding and Apply's own
// lookups/writes must carry a tenant.
func tenantCtx() context.Context {
	return auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
}

// seedRecord writes a record with the given state and returns its assigned uid.
func seedRecord(t *testing.T, host *testHost, state string) string {
	t.Helper()
	res, err := host.DB().Write(tenantCtx(), "record",
		[]db.Document{{"state": state}}, db.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	return res.Added[0]
}

func recordDoc(t *testing.T, host *testHost, uid string) db.Document {
	t.Helper()
	rec, err := host.DB().GetOne(tenantCtx(), "record", db.Document{"uid": uid})
	require.NoError(t, err)
	return rec
}

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

// TestApply_ValidAck: a fresh record (state "") acked through Apply returns the
// new state "ack", writes exactly one attributed comment of type "ack", and the
// record's state + comment_count update through the comment plugin's AfterCreate.
func TestApply_ValidAck(t *testing.T) {
	host := newTestHost(t)
	uid := seedRecord(t, host, "")

	newState, err := Apply(tenantCtx(), host, uid, "ack", "alice", "slack")
	require.NoError(t, err)
	require.Equal(t, "ack", newState)

	// Record transitioned and its comment_count bumped (AfterCreate fired).
	rec := recordDoc(t, host, uid)
	require.Equal(t, "ack", rec["state"], "record state must be applied via the comment seam")
	require.Equal(t, int64(1), asInt64(t, rec["comment_count"]), "comment_count must bump")
	require.Equal(t, "alice", rec["acked_by"], "acked_by must be denormalised from the actor")

	// Exactly one comment exists, attributed to the actor + channel.
	comments, _, err := host.DB().Search(tenantCtx(), "comment", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, comments, 1)
	require.Equal(t, "ack", comments[0]["type"])
	require.Equal(t, uid, comments[0]["record_uid"])
	require.Equal(t, "alice", comments[0]["user"], "user attribution is the actor")
	require.Equal(t, "slack", comments[0]["method"], "method attribution is the channel")
}

// TestApply_RejectsDoubleAck: a record already in state "ack" cannot be acked
// again — Apply returns an ErrInvalidTransition-wrapped error, writes no comment,
// and leaves the record state unchanged.
func TestApply_RejectsDoubleAck(t *testing.T) {
	host := newTestHost(t)
	uid := seedRecord(t, host, "ack")

	_, err := Apply(tenantCtx(), host, uid, "ack", "bob", "telegram")
	require.Error(t, err)
	require.True(t, errors.Is(err, comment.ErrInvalidTransition),
		"double-ack must wrap comment.ErrInvalidTransition")

	rec := recordDoc(t, host, uid)
	require.Equal(t, "ack", rec["state"], "illegal transition must leave state unchanged")

	comments, _, err := host.DB().Search(tenantCtx(), "comment", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Empty(t, comments, "no comment may be written on a rejected transition")
}

// TestApply_UnknownRecord: a uid that resolves to no record errors cleanly
// (no panic), and writes no comment.
func TestApply_UnknownRecord(t *testing.T) {
	host := newTestHost(t)

	_, err := Apply(tenantCtx(), host, "does-not-exist", "ack", "carol", "slack")
	require.Error(t, err)

	comments, _, err := host.DB().Search(tenantCtx(), "comment", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Empty(t, comments)
}
