package heartbeat

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ---- test doubles ---------------------------------------------------------

// memDB is a tiny in-memory db.Driver covering the methods the heartbeat
// plugin actually exercises (Search, SetFields). Everything else returns a
// zero value. Documents are matched on a single `name = <value>` equality,
// which is all the plugin's queries use.
type memDB struct {
	mu   sync.Mutex
	docs []db.Document
}

func (m *memDB) add(doc db.Document) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs = append(m.docs, doc)
}

func (m *memDB) get(name string) db.Document {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.docs {
		if d["name"] == name {
			return d
		}
	}
	return nil
}

func (m *memDB) Search(_ context.Context, _ string, cond condition.Cond, _ db.Page) ([]db.Document, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]db.Document, 0, len(m.docs))
	for _, d := range m.docs {
		if condMatches(cond, d) {
			cp := db.Document{}
			for k, v := range d {
				cp[k] = v
			}
			out = append(out, cp)
		}
	}
	return out, len(out), nil
}

func (m *memDB) SetFields(_ context.Context, _ string, fields db.Document, cond condition.Cond) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	matched := 0
	for _, d := range m.docs {
		if condMatches(cond, d) {
			for k, v := range fields {
				d[k] = v
			}
			matched++
		}
	}
	return matched, nil
}

func (m *memDB) UnsetFields(_ context.Context, _ string, fields []string, cond condition.Cond) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	matched := 0
	for _, d := range m.docs {
		if !condMatches(cond, d) {
			continue
		}
		changed := false
		for _, k := range fields {
			if _, ok := d[k]; ok {
				delete(d, k)
				changed = true
			}
		}
		if changed {
			matched++
		}
	}
	return matched, nil
}

// condMatches handles the only condition shapes the plugin issues: an empty
// (AlwaysTrue) condition and a single `name = <value>` equality.
func condMatches(cond condition.Cond, doc db.Document) bool {
	if cond.IsZero() {
		return true
	}
	if cond.Op == condition.OpEq {
		return doc[cond.Field] == cond.Value
	}
	return false
}

// --- unused db.Driver methods (return zero values) ---
func (m *memDB) GetOne(context.Context, string, db.Document) (db.Document, error) {
	return nil, nil
}
func (m *memDB) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, nil
}
func (m *memDB) Write(context.Context, string, []db.Document, db.WriteOptions) (db.WriteResult, error) {
	return db.WriteResult{}, nil
}
func (m *memDB) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, nil
}
func (m *memDB) UpdateOne(context.Context, string, string, db.Document, bool) error { return nil }
func (m *memDB) Delete(context.Context, string, condition.Cond, bool) (int, error)  { return 0, nil }
func (m *memDB) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error {
	return nil
}
func (m *memDB) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, nil
}
func (m *memDB) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (m *memDB) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (m *memDB) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (m *memDB) CreateIndex(context.Context, string, []string) error { return nil }
func (m *memDB) ListCollections(context.Context) ([]string, error)   { return nil, nil }
func (m *memDB) Drop(context.Context, string) error                  { return nil }
func (m *memDB) Backup(context.Context, string, []string) error      { return nil }
func (m *memDB) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (m *memDB) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (m *memDB) CleanupOrphans(context.Context, string) (int, error) { return 0, nil }
func (m *memDB) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (m *memDB) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (m *memDB) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (m *memDB) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (m *memDB) RenumberField(context.Context, string, string) error { return nil }
func (m *memDB) Watcher() syncer.Bus                                 { return nil }
func (m *memDB) Close() error                                        { return nil }

// fakeHost is a plugins.Host that also satisfies the plugin's recordProcessor
// runtime assertion. It carries a memDB and captures every injected record.
type fakeHost struct {
	driver  *memDB
	mu      sync.Mutex
	records []snoozetypes.Record
}

func (h *fakeHost) DB() db.Driver                { return h.driver }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("heartbeat-test") }
func (h *fakeHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *fakeHost) Config() *config.Config       { return config.Default() }
func (h *fakeHost) Plugin(string) plugins.Plugin { return nil }

func (h *fakeHost) ProcessRecord(_ context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return rec, plugins.ActionContinue, nil
}

func (h *fakeHost) seen() []snoozetypes.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]snoozetypes.Record, len(h.records))
	copy(out, h.records)
	return out
}

// ---- helpers --------------------------------------------------------------

func newPlugin(t *testing.T, host plugins.Host) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "heartbeat"})
	require.NoError(t, err)
	hp := p.(*Plugin)
	require.NoError(t, hp.PostInit(context.Background(), host))
	return hp
}

func newHost() *fakeHost { return &fakeHost{driver: &memDB{}} }

// ---- contract & registration ----------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "heartbeat"))
}

func TestEmbeddedMetadataParses(t *testing.T) {
	meta, err := plugins.ParseMetadata(metaYAML)
	require.NoError(t, err)
	require.Equal(t, "heartbeat", meta.Name)

	// CRUD is authenticated: route_defaults.authentication is true.
	require.True(t, meta.AuthenticationRequired(""), "CRUD must require a Bearer token")

	// The ping endpoint is public via a per-path override.
	// WebhookPath() == "/heartbeat" — that key must resolve to false.
	require.False(t, meta.AuthenticationRequired("/heartbeat"), "ping endpoint must be public")
}

func TestPluginContract(t *testing.T) {
	var (
		_ plugins.DataModel        = (*Plugin)(nil)
		_ plugins.LifecycleHook    = (*Plugin)(nil)
		_ plugins.WebhookReceiver  = (*Plugin)(nil)
		_ plugins.WriteTransformer = (*Plugin)(nil)
	)
	p := newPlugin(t, newHost())
	require.Equal(t, "heartbeat", p.Name())
	require.Equal(t, "/heartbeat", p.WebhookPath())
	require.Equal(t, "heartbeat", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))
}

// ---- schema / validate -----------------------------------------------------

func TestValidateAcceptsGoodDoc(t *testing.T) {
	p := newPlugin(t, newHost())
	require.NoError(t, p.Validate(map[string]any{
		"name":     "nightly-backup",
		"interval": float64(3600),
		"grace":    float64(300),
		"severity": "critical",
		"enabled":  true,
	}))
}

func TestValidateRejectsBadDocs(t *testing.T) {
	p := newPlugin(t, newHost())

	// Missing interval on a doc that has name → full doc missing required field.
	require.Error(t, p.Validate(map[string]any{"name": "x", "enabled": true}))

	// Non-positive interval.
	require.Error(t, p.Validate(map[string]any{"name": "x", "interval": float64(0)}))

	// Wrong type for interval.
	require.Error(t, p.Validate(map[string]any{"name": "x", "interval": "soon"}))

	// Empty name on a full-ish doc.
	require.Error(t, p.Validate(map[string]any{"name": "  ", "interval": float64(10)}))

	// Wrong type for enabled.
	require.Error(t, p.Validate(map[string]any{"name": "x", "interval": float64(10), "enabled": "yes-please"}))
}

func TestValidateAllowsPartialPatch(t *testing.T) {
	p := newPlugin(t, newHost())
	// A PATCH that only flips enabled must not require name/interval.
	require.NoError(t, p.Validate(map[string]any{"enabled": false}))
	// A PATCH bumping the interval alone is fine.
	require.NoError(t, p.Validate(map[string]any{"interval": float64(120)}))
}

func TestSchemaShape(t *testing.T) {
	p := newPlugin(t, newHost())
	schema, ok := p.Schema().(map[string]any)
	require.True(t, ok)
	require.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, props, "name")
	require.Contains(t, props, "interval")
	require.Contains(t, props, "token", "token must appear in schema")
}

// TestValidateAcceptsDocWithoutToken confirms that Validate does not require
// an operator-supplied token — the server generates it in TransformWrite.
func TestValidateAcceptsDocWithoutToken(t *testing.T) {
	p := newPlugin(t, newHost())
	require.NoError(t, p.Validate(map[string]any{
		"name":     "nightly-backup",
		"interval": float64(3600),
	}), "token must not be required by Validate")
}

// ---- TransformWrite / token generation ------------------------------------

func TestTransformWriteGeneratesTokenOnCreate(t *testing.T) {
	p := newPlugin(t, newHost())
	doc := map[string]any{
		"name":     "daily-job",
		"interval": float64(86400),
	}
	require.NoError(t, p.TransformWrite(context.Background(), doc))
	tok, ok := doc["token"].(string)
	require.True(t, ok, "token must be a string after TransformWrite")
	require.NotEmpty(t, tok, "token must not be empty")
	// Tokens are base64-RawURL encoded, so they must not contain +, /, or =.
	require.NotContains(t, tok, "+")
	require.NotContains(t, tok, "/")
	require.NotContains(t, tok, "=")
}

func TestTransformWritePreservesExistingToken(t *testing.T) {
	p := newPlugin(t, newHost())
	const original = "already-set-token"
	doc := map[string]any{
		"name":     "daily-job",
		"interval": float64(86400),
		"token":    original,
	}
	require.NoError(t, p.TransformWrite(context.Background(), doc))
	require.Equal(t, original, doc["token"], "existing token must not be overwritten")
}

func TestTransformWriteDoesNotGenerateOnPartialPatch(t *testing.T) {
	p := newPlugin(t, newHost())
	// A PATCH that only updates interval (no `name`) must not invent a token.
	doc := map[string]any{
		"interval": float64(3600),
	}
	require.NoError(t, p.TransformWrite(context.Background(), doc))
	_, hasToken := doc["token"]
	require.False(t, hasToken, "partial patch without 'name' must not receive a generated token")
}

func TestTransformWriteTwoCallsProduceDifferentTokens(t *testing.T) {
	p := newPlugin(t, newHost())
	doc1 := map[string]any{"name": "job-a", "interval": float64(60)}
	doc2 := map[string]any{"name": "job-b", "interval": float64(60)}
	require.NoError(t, p.TransformWrite(context.Background(), doc1))
	require.NoError(t, p.TransformWrite(context.Background(), doc2))
	require.NotEqual(t, doc1["token"], doc2["token"], "each create must get a unique token")
}

// ---- ping endpoint ---------------------------------------------------------

// ping fires a test ping request. Pass token="" to omit the token parameter
// entirely (tests negative / 401 cases).
func ping(t *testing.T, p *Plugin, method, name, token string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/v1/webhook/heartbeat"
	sep := "?"
	if name != "" {
		url += sep + "name=" + name
		sep = "&"
	}
	if token != "" {
		url += sep + "token=" + token
	}
	req := httptest.NewRequest(method, url, nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestPingUpdatesLastSeen(t *testing.T) {
	const tok = "test-token-abc123"
	host := newHost()
	host.driver.add(db.Document{"name": "hb1", "interval": float64(60), "token": tok})

	fixed := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	p := newPlugin(t, host)
	p.now = func() time.Time { return fixed }

	w := ping(t, p, http.MethodPost, "hb1", tok)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.Equal(t, "hb1", resp["name"])

	doc := host.driver.get("hb1")
	require.Equal(t, fixed.Format(time.RFC3339), doc["last_seen"])
}

func TestPingGETAlsoWorks(t *testing.T) {
	const tok = "test-token-get"
	host := newHost()
	host.driver.add(db.Document{"name": "hb1", "interval": float64(60), "token": tok})
	p := newPlugin(t, host)

	w := ping(t, p, http.MethodGet, "hb1", tok)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestPingUnknownHeartbeat404(t *testing.T) {
	host := newHost()
	p := newPlugin(t, host)
	// name does not exist; token is non-empty so we reach the lookup step.
	w := ping(t, p, http.MethodPost, "nope", "some-token")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestPingMissingName400(t *testing.T) {
	host := newHost()
	p := newPlugin(t, host)
	w := ping(t, p, http.MethodPost, "", "")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPingMissingToken401(t *testing.T) {
	host := newHost()
	host.driver.add(db.Document{"name": "hb1", "interval": float64(60), "token": "secret"})
	p := newPlugin(t, host)
	// name is present but token is omitted → 401.
	w := ping(t, p, http.MethodPost, "hb1", "")
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPingWrongToken401(t *testing.T) {
	host := newHost()
	host.driver.add(db.Document{"name": "hb1", "interval": float64(60), "token": "correct-token"})
	p := newPlugin(t, host)
	w := ping(t, p, http.MethodPost, "hb1", "wrong-token")
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPingWrongMethod405(t *testing.T) {
	host := newHost()
	p := newPlugin(t, host)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhook/heartbeat?name=hb1&token=x", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---- scanner ---------------------------------------------------------------

// overdueDoc inserts a heartbeat last seen `silentFor` ago relative to `nowRef`.
func overdueDoc(host *fakeHost, name string, interval, grace int64, lastSeen time.Time) {
	host.driver.add(db.Document{
		"name":      name,
		"interval":  float64(interval),
		"grace":     float64(grace),
		"severity":  "critical",
		"enabled":   true,
		"last_seen": lastSeen.UTC().Format(time.RFC3339),
	})
}

func TestScanFiresOnceForOverdueHeartbeat(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	// last_seen 5 minutes ago, interval 60s, grace 0 → overdue.
	overdueDoc(host, "hb1", 60, 0, now.Add(-5*time.Minute))

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	p.scan(context.Background())
	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "heartbeat", recs[0].Source)
	require.Equal(t, "hb1", recs[0].Host)
	require.Equal(t, "critical", recs[0].Severity)
	require.Contains(t, recs[0].Message, "heartbeat hb1 missed")
	require.Equal(t, "hb1", recs[0].Raw["name"])

	// Second scan with the SAME last_seen must not re-fire.
	p.scan(context.Background())
	require.Len(t, host.seen(), 1)
}

func TestScanDoesNotFireForFreshHeartbeat(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	// last_seen 10s ago, interval 60s → not overdue.
	overdueDoc(host, "hb1", 60, 0, now.Add(-10*time.Second))

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	p.scan(context.Background())
	require.Empty(t, host.seen())
}

func TestScanRespectsGrace(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	// 90s of silence, interval 60s + grace 60s = 120s budget → not yet overdue.
	overdueDoc(host, "hb1", 60, 60, now.Add(-90*time.Second))

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	p.scan(context.Background())
	require.Empty(t, host.seen())
}

func TestScanSkipsDisabled(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	host.driver.add(db.Document{
		"name":      "hb1",
		"interval":  float64(60),
		"enabled":   false,
		"last_seen": now.Add(-1 * time.Hour).Format(time.RFC3339),
	})

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	p.scan(context.Background())
	require.Empty(t, host.seen())
}

func TestFreshPingClearsFiringState(t *testing.T) {
	const tok = "scan-rearm-token"
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	// overdueDoc does not set a token field; add it explicitly.
	host.driver.add(db.Document{
		"name":      "hb1",
		"interval":  float64(60),
		"grace":     float64(0),
		"severity":  "critical",
		"enabled":   true,
		"last_seen": now.Add(-5 * time.Minute).UTC().Format(time.RFC3339),
		"token":     tok,
	})

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	// First scan fires.
	p.scan(context.Background())
	require.Len(t, host.seen(), 1)

	// A fresh ping at `now` updates last_seen and clears the fired mark.
	w := ping(t, p, http.MethodPost, "hb1", tok)
	require.Equal(t, http.StatusOK, w.Code)

	// Advance time well past interval again so the new window is overdue.
	later := now.Add(5 * time.Minute)
	p.now = func() time.Time { return later }
	p.scan(context.Background())

	// A new miss alert must have fired for the new window.
	require.Len(t, host.seen(), 2)
}

func TestScanNoProcessorIsNoOp(t *testing.T) {
	// A host that does NOT satisfy recordProcessor.
	host := &nakedHost{driver: &memDB{}}
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	host.driver.add(db.Document{
		"name":      "hb1",
		"interval":  float64(60),
		"enabled":   true,
		"last_seen": now.Add(-5 * time.Minute).Format(time.RFC3339),
	})

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }
	// Must not panic and must return promptly.
	p.scan(context.Background())
}

// nakedHost is a plugins.Host that does NOT satisfy recordProcessor.
type nakedHost struct{ driver *memDB }

func (h *nakedHost) DB() db.Driver                { return h.driver }
func (h *nakedHost) Bus() plugins.Bus             { return nil }
func (h *nakedHost) Logger() *slog.Logger         { return slog.Default() }
func (h *nakedHost) Tracer() trace.Tracer         { return otel.Tracer("heartbeat-test") }
func (h *nakedHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *nakedHost) Config() *config.Config       { return config.Default() }
func (h *nakedHost) Plugin(string) plugins.Plugin { return nil }

// ---- lifecycle -------------------------------------------------------------

func TestStartStop(t *testing.T) {
	host := newHost()
	p := newPlugin(t, host)
	p.interval = 5 * time.Millisecond

	require.NoError(t, p.Start(context.Background()))
	// Give the ticker a chance to fire a couple of times.
	time.Sleep(25 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		_ = p.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return promptly")
	}

	// Stop again is a no-op and must not block or panic.
	require.NoError(t, p.Stop(context.Background()))
}

func TestStartStopFiresViaTicker(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	overdueDoc(host, "hb1", 60, 0, now.Add(-5*time.Minute))

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }
	p.interval = 5 * time.Millisecond

	require.NoError(t, p.Start(context.Background()))
	require.Eventually(t, func() bool {
		return len(host.seen()) >= 1
	}, time.Second, 5*time.Millisecond)
	require.NoError(t, p.Stop(context.Background()))

	// Even after many ticks, only one alert fired (dedup).
	require.Len(t, host.seen(), 1)
}

// ---- PostInit backfill (Follow-up A) ----------------------------------------

// TestPostInitBackfillsTokenForLegacyDoc verifies that a heartbeat document
// seeded without a token receives a non-empty token after PostInit.
func TestPostInitBackfillsTokenForLegacyDoc(t *testing.T) {
	host := newHost()
	// A legacy document has name + interval but no token field.
	host.driver.add(db.Document{
		"name":     "legacy-hb",
		"interval": float64(60),
		"enabled":  true,
	})

	// newPlugin calls PostInit internally.
	newPlugin(t, host)

	doc := host.driver.get("legacy-hb")
	require.NotNil(t, doc, "doc must still be in the DB")
	tok, _ := doc["token"].(string)
	require.NotEmpty(t, tok, "legacy doc without a token must be backfilled after PostInit")
}

// TestPostInitPreservesExistingToken verifies that a heartbeat document that
// already has a non-empty token is left unchanged by PostInit (idempotent).
func TestPostInitPreservesExistingToken(t *testing.T) {
	const original = "already-set-token-xyz"
	host := newHost()
	host.driver.add(db.Document{
		"name":     "modern-hb",
		"interval": float64(60),
		"token":    original,
		"enabled":  true,
	})

	newPlugin(t, host)

	doc := host.driver.get("modern-hb")
	require.NotNil(t, doc)
	require.Equal(t, original, doc["token"], "existing token must not be overwritten")
}

// TestPostInitNilHostDoesNotPanic verifies that PostInit with a nil host returns
// nil without panicking (handles migration-tool / test scenarios where no host
// is available).
func TestPostInitNilHostDoesNotPanic(t *testing.T) {
	p, err := factory(plugins.Metadata{Name: "heartbeat"})
	require.NoError(t, err)
	hp := p.(*Plugin)
	require.NotPanics(t, func() {
		err := hp.PostInit(context.Background(), nil)
		require.NoError(t, err)
	})
}

// TestPostInitNilDBDoesNotPanic verifies that PostInit returns nil and does not
// panic when the host's DB() returns nil (covers migration tool startup path).
func TestPostInitNilDBDoesNotPanic(t *testing.T) {
	// nilDBHost satisfies plugins.Host but returns nil from DB().
	host := &nilDBHost{}

	p, err := factory(plugins.Metadata{Name: "heartbeat"})
	require.NoError(t, err)
	hp := p.(*Plugin)
	require.NotPanics(t, func() {
		err := hp.PostInit(context.Background(), host)
		require.NoError(t, err)
	})
}

// nilDBHost is a plugins.Host whose DB() returns nil.
type nilDBHost struct{}

func (h *nilDBHost) DB() db.Driver                { return nil }
func (h *nilDBHost) Bus() plugins.Bus             { return nil }
func (h *nilDBHost) Logger() *slog.Logger         { return slog.Default() }
func (h *nilDBHost) Tracer() trace.Tracer         { return otel.Tracer("heartbeat-test") }
func (h *nilDBHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *nilDBHost) Config() *config.Config       { return config.Default() }
func (h *nilDBHost) Plugin(string) plugins.Plugin { return nil }
