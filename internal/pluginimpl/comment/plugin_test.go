package comment

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

type testHost struct{ drv *sqlite.Driver }

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return &testHost{drv: drv}
}

func (h *testHost) DB() db.Driver                { return h.drv }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return slog.Default() }
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("comment-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
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
