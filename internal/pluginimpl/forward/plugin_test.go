package forward

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// --- test scaffolding ---

// tctx returns a context scoped to the reserved default tenant. The forward
// collection is tenant-scoped, so every DB/plugin call that touches it must
// carry a tenant (mirrors real single-tenant behaviour).
func tctx() context.Context {
	return auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
}

type testHost struct {
	driver db.Driver
	writer *asyncwriter.Writer
	cfg    *config.Config
	logger *slog.Logger
	tracer trace.Tracer
	metr   *telemetry.Registry
	plugs  map[string]plugins.Plugin
}

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	cfg := config.Default()
	cfg.Syncer.Hostname = "self-node"
	return &testHost{
		driver: drv,
		cfg:    cfg,
		logger: slog.Default(),
		tracer: otel.Tracer("forward-test"),
		metr:   telemetry.NewRegistry(nil),
		plugs:  map[string]plugins.Plugin{},
	}
}

func (h *testHost) DB() db.Driver                { return h.driver }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return h.logger }
func (h *testHost) Tracer() trace.Tracer         { return h.tracer }
func (h *testHost) Metrics() *telemetry.Registry { return h.metr }
func (h *testHost) Config() *config.Config       { return h.cfg }
func (h *testHost) Plugin(name string) plugins.Plugin {
	return h.plugs[name]
}
func (h *testHost) AsyncWriter() *asyncwriter.Writer { return h.writer }

// peerRecorder is a goroutine-safe collector for relay POSTs. The relay fires
// on a detached goroutine, so capture through a mutex keeps -race clean.
type peerRecorder struct {
	mu        sync.Mutex
	hits      int
	bodies    [][]byte
	loopHdrs  []string
	apikey    []string
	relayedAt []string
}

func (p *peerRecorder) add(body []byte, loop, apikey, relayedAt string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hits++
	p.bodies = append(p.bodies, body)
	p.loopHdrs = append(p.loopHdrs, loop)
	p.apikey = append(p.apikey, apikey)
	p.relayedAt = append(p.relayedAt, relayedAt)
}

func (p *peerRecorder) relayedAtHdrs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.relayedAt)
}

func (p *peerRecorder) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits
}

func (p *peerRecorder) snapshot() ([][]byte, []string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := slices.Clone(p.bodies)
	l := slices.Clone(p.loopHdrs)
	k := slices.Clone(p.apikey)
	return b, l, k
}

func newPeer(t *testing.T, status int) (*httptest.Server, *peerRecorder) {
	t.Helper()
	rec := &peerRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.add(body, r.Header.Get("X-Snooze-Loop"), r.Header.Get("X-API-Key"), r.Header.Get("X-Snooze-Relayed-At"))
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// writeDestination persists a forward destination definition.
func writeDestination(t *testing.T, host *testHost, dest db.Document) {
	t.Helper()
	_, err := host.driver.Write(tctx(), collectionName,
		[]db.Document{dest}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: true})
	require.NoError(t, err)
}

func freshPlugin(t *testing.T, host *testHost) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "forward"}, Now: func() time.Time { return time.Unix(1_700_000_000, 0) }}
	require.NoError(t, p.PostInit(tctx(), host))
	return p
}

// --- tests ---

func TestProcess_RelaysMatchingDestination(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	peer, rec := newPeer(t, http.StatusOK)
	writeDestination(t, host, db.Document{
		"name":     "peer1",
		"enabled":  true,
		"endpoint": peer.URL + "/api/v1/alerts",
		"auth":     map[string]any{"type": "apikey", "api_key": "k", "header": "X-API-Key"},
	})
	p := freshPlugin(t, host)

	in := snoozetypes.Record{Host: "h1", Message: "boom", Severity: "critical"}
	res, err := p.Process(tctx(), in)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	require.Eventually(t, func() bool { return rec.count() == 1 }, 2*time.Second, 10*time.Millisecond)
	bodies, loops, keys := rec.snapshot()
	require.Len(t, bodies, 1)
	require.Equal(t, "self-node", loops[0], "loop header carries this server's id")
	require.Equal(t, "k", keys[0], "destination apikey auth applied")

	var got snoozetypes.Record
	require.NoError(t, json.Unmarshal(bodies[0], &got))
	require.Equal(t, "h1", got.Host)
	require.Equal(t, "boom", got.Message)

	// The injected clock (frozen at 1_700_000_000) drives the relay timestamp,
	// proving the relay path never reaches time.Now() directly.
	require.Equal(t, "2023-11-14T22:13:20Z", rec.relayedAtHdrs()[0])
}

func TestProcess_SkipsWhenSelfInChain(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	peer, rec := newPeer(t, http.StatusOK)
	writeDestination(t, host, db.Document{
		"name": "peer1", "enabled": true, "endpoint": peer.URL,
	})
	p := freshPlugin(t, host)

	ctx := auth.WithLoopChain(tctx(), []string{"other", "self-node"})
	res, err := p.Process(ctx, snoozetypes.Record{Host: "h1"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	// Give any erroneous goroutine a moment to fire, then assert none did.
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 0, rec.count(), "self in chain → no relay")
}

func TestProcess_SkipsDestinationInChain(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	peer, rec := newPeer(t, http.StatusOK)
	writeDestination(t, host, db.Document{
		"name": "peer1", "enabled": true, "endpoint": peer.URL,
	})
	p := freshPlugin(t, host)

	ctx := auth.WithLoopChain(tctx(), []string{"peer1"})
	res, err := p.Process(ctx, snoozetypes.Record{Host: "h1"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 0, rec.count(), "destination already in chain → skip")
}

func TestProcess_SkipsNonMatchingCondition(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	peer, rec := newPeer(t, http.StatusOK)
	writeDestination(t, host, db.Document{
		"name": "peer1", "enabled": true, "endpoint": peer.URL,
		"condition": []any{"=", "severity", "critical"},
	})
	p := freshPlugin(t, host)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "h1", Severity: "warning"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 0, rec.count(), "condition mismatch → no relay")
}

func TestProcess_SkipsDisabled(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	peer, rec := newPeer(t, http.StatusOK)
	writeDestination(t, host, db.Document{
		"name": "peer1", "enabled": false, "endpoint": peer.URL,
	})
	p := freshPlugin(t, host)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "h1"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 0, rec.count(), "disabled destination → no relay")
}

func TestProcess_SkipsNonMatchingEventClass(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	peer, rec := newPeer(t, http.StatusOK)
	writeDestination(t, host, db.Document{
		"name": "peer1", "enabled": true, "endpoint": peer.URL,
		"event_classes": []string{"actions"}, // does not include "*" or "alerts"
	})
	p := freshPlugin(t, host)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "h1"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 0, rec.count(), "event_classes not covering alerts → no relay")
}

func TestProcess_PeerFailureDoesNotBlock(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	peer, rec := newPeer(t, http.StatusInternalServerError)
	writeDestination(t, host, db.Document{
		"name": "peer1", "enabled": true, "endpoint": peer.URL,
	})
	p := freshPlugin(t, host)

	res, err := p.Process(tctx(), snoozetypes.Record{Host: "h1"})
	require.NoError(t, err, "peer failure must not surface as a pipeline error")
	require.Equal(t, plugins.ActionContinue, res.Action)

	// The POST still fires (and 500s); we just don't block on it.
	require.Eventually(t, func() bool { return rec.count() == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestProcess_AppendsSelfToInboundChain(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	peer, rec := newPeer(t, http.StatusOK)
	writeDestination(t, host, db.Document{
		"name": "peer1", "enabled": true, "endpoint": peer.URL,
	})
	p := freshPlugin(t, host)

	ctx := auth.WithLoopChain(tctx(), []string{"hub"})
	res, err := p.Process(ctx, snoozetypes.Record{Host: "h1"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	require.Eventually(t, func() bool { return rec.count() == 1 }, 2*time.Second, 10*time.Millisecond)
	_, loops, _ := rec.snapshot()
	require.Equal(t, "hub,self-node", loops[0], "relayed chain = inbound chain + self")
}

func TestMetadata(t *testing.T) {
	t.Parallel()
	p, err := factory(plugins.Metadata{Name: "forward"})
	require.NoError(t, err)
	require.Equal(t, "forward", p.Name())
	meta := p.Metadata()
	require.Equal(t, "forward", meta.Name)
}

func TestRegistration(t *testing.T) {
	t.Parallel()
	require.True(t, slices.Contains(plugins.Registered(), "forward"))
}

func TestValidate(t *testing.T) {
	t.Parallel()
	p := &Plugin{meta: plugins.Metadata{Name: "forward"}}

	require.NoError(t, p.Validate(map[string]any{"name": "peer1", "endpoint": "https://peer/api/v1/alerts"}))
	require.NoError(t, p.Validate(map[string]any{"name": "peer1", "endpoint": "https://peer", "event_classes": []any{"*"}}))
	require.NoError(t, p.Validate(map[string]any{"name": "peer1", "endpoint": "https://peer", "event_classes": []any{"alerts"}}))
	require.NoError(t, p.Validate(map[string]any{"name": "patch-only"})) // partial PATCH tolerated

	require.Error(t, p.Validate(map[string]any{"name": "x", "endpoint": ""}), "empty endpoint rejected")
	require.Error(t, p.Validate(map[string]any{"name": "x", "endpoint": "https://peer", "event_classes": []any{"bogus"}}),
		"unknown event_class rejected")
}

func TestForward_TenantIsolation(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	ctxA := auth.WithTenant(context.Background(), "acme")
	ctxB := auth.WithTenant(context.Background(), "beta")

	_, err := host.DB().Write(ctxA, collectionName, []db.Document{{
		"name": "acme-peer", "enabled": true, "endpoint": "https://peer",
	}}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: false})
	require.NoError(t, err)

	p := &Plugin{meta: plugins.Metadata{}, Now: time.Now}
	require.NoError(t, p.PostInit(ctxA, host))

	p.mu.RLock()
	acme := p.dests["acme"]
	p.mu.RUnlock()
	require.Len(t, acme, 1)

	require.NoError(t, p.Reload(ctxB))
	p.mu.RLock()
	beta := p.dests["beta"]
	p.mu.RUnlock()
	require.Empty(t, beta)
}
