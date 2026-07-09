package snoozepeer

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// --- test scaffolding ---
//
// testHost is a minimal stub satisfying the full plugins.Host interface.
// snoozepeer's PostInit never touches the DB (config lives in the action's
// subcontent, not in a collection), so DB() is a nil driver here — unlike a
// Processor plugin's testHost, which would back DB() with a real sqlite
// instance because such a plugin's Reload actually queries a collection.

type testHost struct {
	cfg    *config.Config
	logger *slog.Logger
	tracer trace.Tracer
	metr   *telemetry.Registry
}

func newTestHost(hostname string) *testHost {
	cfg := config.Default()
	cfg.Syncer.Hostname = hostname
	return &testHost{
		cfg:    cfg,
		logger: slog.Default(),
		tracer: otel.Tracer("snoozepeer-test"),
		metr:   telemetry.NewRegistry(nil),
	}
}

func (h *testHost) DB() db.Driver                     { return nil }
func (h *testHost) Bus() plugins.Bus                  { return nil }
func (h *testHost) Logger() *slog.Logger              { return h.logger }
func (h *testHost) Tracer() trace.Tracer              { return h.tracer }
func (h *testHost) Metrics() *telemetry.Registry      { return h.metr }
func (h *testHost) Config() *config.Config            { return h.cfg }
func (h *testHost) Plugin(name string) plugins.Plugin { return nil }

// frozenNow is the deterministic clock injected into every test plugin, so
// the relay path never reaches time.Now() directly (the injected-clock rule).
var frozenNow = time.Unix(1_700_000_000, 0).UTC()

func freshPlugin(t *testing.T, hostname string) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "snoozepeer"}, Now: func() time.Time { return frozenNow }}
	require.NoError(t, p.PostInit(context.Background(), newTestHost(hostname)))
	return p
}

type captured struct {
	hit         bool
	body        []byte
	loop        string
	relayedAt   string
	contentType string
	authHeader  string
}

func newPeer(t *testing.T, status int) (*httptest.Server, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.hit = true
		c.body, _ = io.ReadAll(r.Body)
		c.loop = r.Header.Get("X-Snooze-Loop")
		c.relayedAt = r.Header.Get("X-Snooze-Relayed-At")
		c.contentType = r.Header.Get("Content-Type")
		c.authHeader = r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{Host: "h1", Message: "boom", Severity: "critical"}
}

// --- tests ---

func TestSend_RelaySuccess(t *testing.T) {
	t.Parallel()
	peer, c := newPeer(t, http.StatusOK)
	p := freshPlugin(t, "self-node")

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"endpoint": peer.URL + "/api/v1/alerts"},
	})
	require.NoError(t, err)

	require.True(t, c.hit, "peer must have been called")
	require.Equal(t, "application/json", c.contentType)
	require.Equal(t, "self-node", c.loop, "self appended to empty chain")
	require.NotEmpty(t, c.relayedAt)
	require.Equal(t, "2023-11-14T22:13:20Z", c.relayedAt, "relayed-at uses the injected clock")

	var got snoozetypes.Record
	require.NoError(t, json.Unmarshal(c.body, &got))
	require.Equal(t, "h1", got.Host)
	require.Equal(t, "boom", got.Message)
}

func TestSend_SkipsWhenSelfInChain(t *testing.T) {
	t.Parallel()
	peer, c := newPeer(t, http.StatusOK)
	p := freshPlugin(t, "self-node")

	ctx := auth.WithLoopChain(context.Background(), []string{"self-node"})
	err := p.Send(ctx, sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"endpoint": peer.URL},
	})
	require.NoError(t, err)
	require.False(t, c.hit, "self in chain -> no relay")
}

func TestSend_SkipsWhenPeerInChain(t *testing.T) {
	t.Parallel()
	peer, c := newPeer(t, http.StatusOK)
	p := freshPlugin(t, "self-node")

	ctx := auth.WithLoopChain(context.Background(), []string{"peer-a"})
	err := p.Send(ctx, sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"endpoint": peer.URL, "action_name": "peer-a"},
	})
	require.NoError(t, err)
	require.False(t, c.hit, "this action's peer already in chain -> no relay")
}

func TestSend_MissingEndpointErrors(t *testing.T) {
	t.Parallel()
	p := freshPlugin(t, "self-node")

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: map[string]any{}})
	require.Error(t, err)

	err = p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"endpoint": "   "},
	})
	require.Error(t, err, "blank endpoint after trim is also rejected")
}

func TestSend_BearerAuthApplied(t *testing.T) {
	t.Parallel()
	peer, c := newPeer(t, http.StatusOK)
	p := freshPlugin(t, "self-node")

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"endpoint": peer.URL,
			"auth":     map[string]any{"type": "bearer", "token": "s3cret"},
		},
	})
	require.NoError(t, err)
	require.True(t, c.hit)
	require.Equal(t, "Bearer s3cret", c.authHeader)
}

func TestSend_NonOKPeerResponseErrors(t *testing.T) {
	t.Parallel()
	peer, c := newPeer(t, http.StatusInternalServerError)
	p := freshPlugin(t, "self-node")

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"endpoint": peer.URL},
	})
	require.Error(t, err)
	require.True(t, c.hit, "the request still fires; the 500 is what makes Send fail")
}

func TestMetadata(t *testing.T) {
	t.Parallel()
	p, err := factory(plugins.Metadata{Name: "snoozepeer"})
	require.NoError(t, err)
	require.Equal(t, "snoozepeer", p.Name())
	require.Equal(t, "snoozepeer", p.Metadata().Name)
}

func TestRegistration(t *testing.T) {
	t.Parallel()
	require.True(t, slices.Contains(plugins.Registered(), "snoozepeer"))
}
