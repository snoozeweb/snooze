package pagerduty

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/condition"
	coreconfig "github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/internal/telemetry"
)

// ---------------------------------------------------------------------------
// Step 1 — event-type mapper
// ---------------------------------------------------------------------------

func TestMapEventType_Acknowledge(t *testing.T) {
	require.Equal(t, "ack", mapEventType("incident.acknowledge"))
}

func TestMapEventType_Resolve(t *testing.T) {
	require.Equal(t, "close", mapEventType("incident.resolve"))
}

func TestMapEventType_Trigger(t *testing.T) {
	require.Equal(t, "", mapEventType("incident.trigger"))
}

func TestMapEventType_Unacknowledge(t *testing.T) {
	require.Equal(t, "", mapEventType("incident.unacknowledge"))
}

func TestMapEventType_Escalate(t *testing.T) {
	require.Equal(t, "", mapEventType("incident.escalate"))
}

func TestMapEventType_Assign(t *testing.T) {
	require.Equal(t, "ignored", mapEventType("incident.assign"))
}

func TestMapEventType_Unknown(t *testing.T) {
	require.Equal(t, "ignored", mapEventType("incident.delegate"))
}

// ---------------------------------------------------------------------------
// HTTP-level HandleWebhook tests (Steps 3, 4, 6)
// ---------------------------------------------------------------------------

// stubDB is a tiny in-memory db.Driver covering only the methods the inbound
// webhook receiver exercises (GetOne, SetFields, UnsetFields, UpdateOne). Every
// other method panics so an accidental call is caught loudly. Documents are
// matched on a single `field = value` equality, which is all the receiver
// issues (hash / uid lookups and patches). setErr forces SetFields/UnsetFields
// to fail, modeling a backend write error.
type stubDB struct {
	mu     sync.Mutex
	docs   []db.Document
	setErr error
}

func (s *stubDB) GetOne(_ context.Context, _ string, match db.Document) (db.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.docs {
		ok := true
		for k, v := range match {
			if d[k] != v {
				ok = false
				break
			}
		}
		if ok {
			cp := db.Document{}
			for k, v := range d {
				cp[k] = v
			}
			return cp, nil
		}
	}
	return nil, db.ErrNotFound
}

func (s *stubDB) SetFields(_ context.Context, _ string, fields db.Document, cond condition.Cond) (int, error) {
	if s.setErr != nil {
		return 0, s.setErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	matched := 0
	for _, d := range s.docs {
		if condEqMatches(cond, d) {
			for k, v := range fields {
				d[k] = v
			}
			matched++
		}
	}
	return matched, nil
}

func (s *stubDB) UnsetFields(_ context.Context, _ string, fields []string, cond condition.Cond) (int, error) {
	if s.setErr != nil {
		return 0, s.setErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	matched := 0
	for _, d := range s.docs {
		if !condEqMatches(cond, d) {
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

func (s *stubDB) UpdateOne(_ context.Context, _, uid string, patch db.Document, _ bool) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.docs {
		if d["uid"] == uid {
			for k, v := range patch {
				d[k] = v
			}
			return nil
		}
	}
	return db.ErrNotFound
}

// get returns the live document with the given uid (not a copy) for assertions.
func (s *stubDB) get(uid string) db.Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.docs {
		if d["uid"] == uid {
			return d
		}
	}
	return nil
}

// condEqMatches handles the only condition shape the receiver issues: a single
// `field = value` equality.
func condEqMatches(cond condition.Cond, doc db.Document) bool {
	if cond.IsZero() {
		return true
	}
	if cond.Op == condition.OpEq {
		return doc[cond.Field] == cond.Value
	}
	return false
}

// Unused Driver methods panic — the receiver must never reach them.
func (s *stubDB) Search(context.Context, string, condition.Cond, db.Page) ([]db.Document, int, error) {
	panic("stubDB.Search: not implemented")
}
func (s *stubDB) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	panic("stubDB.Convert: not implemented")
}
func (s *stubDB) Write(context.Context, string, []db.Document, db.WriteOptions) (db.WriteResult, error) {
	panic("stubDB.Write: not implemented")
}
func (s *stubDB) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	panic("stubDB.ReplaceOne: not implemented")
}
func (s *stubDB) Delete(context.Context, string, condition.Cond, bool) (int, error) {
	panic("stubDB.Delete: not implemented")
}
func (s *stubDB) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error {
	panic("stubDB.BulkIncrement: not implemented")
}
func (s *stubDB) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	panic("stubDB.IncMany: not implemented")
}
func (s *stubDB) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	panic("stubDB.AppendList: not implemented")
}
func (s *stubDB) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	panic("stubDB.PrependList: not implemented")
}
func (s *stubDB) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	panic("stubDB.RemoveList: not implemented")
}
func (s *stubDB) CreateIndex(context.Context, string, []string) error {
	panic("stubDB.CreateIndex: not implemented")
}
func (s *stubDB) ListCollections(context.Context) ([]string, error) {
	panic("stubDB.ListCollections: not implemented")
}
func (s *stubDB) Drop(context.Context, string) error { panic("stubDB.Drop: not implemented") }
func (s *stubDB) Backup(context.Context, string, []string) error {
	panic("stubDB.Backup: not implemented")
}
func (s *stubDB) CleanupTimeout(context.Context, string) (int, error) {
	panic("stubDB.CleanupTimeout: not implemented")
}
func (s *stubDB) CleanupComments(context.Context) (int, error) {
	panic("stubDB.CleanupComments: not implemented")
}
func (s *stubDB) CleanupOrphans(context.Context, string) (int, error) {
	panic("stubDB.CleanupOrphans: not implemented")
}
func (s *stubDB) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	panic("stubDB.CleanupAuditLogs: not implemented")
}
func (s *stubDB) CleanupSnooze(context.Context) (int, error) {
	panic("stubDB.CleanupSnooze: not implemented")
}
func (s *stubDB) CountBy(context.Context, string, condition.Cond, string) (map[string]int, error) {
	panic("stubDB.CountBy: not implemented")
}
func (s *stubDB) CleanupNotification(context.Context) (int, error) {
	panic("stubDB.CleanupNotification: not implemented")
}
func (s *stubDB) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	panic("stubDB.ComputeStats: not implemented")
}
func (s *stubDB) Watcher() syncer.Bus { panic("stubDB.Watcher: not implemented") }
func (s *stubDB) Close() error        { panic("stubDB.Close: not implemented") }

// stubHost is a minimal plugins.Host exposing the stubDB.
type stubHost struct{ driver *stubDB }

func (h *stubHost) DB() db.Driver                { return h.driver }
func (h *stubHost) Bus() plugins.Bus             { return nil }
func (h *stubHost) Logger() *slog.Logger         { return slog.Default() }
func (h *stubHost) Tracer() trace.Tracer         { return otel.Tracer("pagerduty-webhook-test") }
func (h *stubHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *stubHost) Config() *coreconfig.Config   { return coreconfig.Default() }
func (h *stubHost) Plugin(string) plugins.Plugin { return nil }

// newWebhookPlugin builds a Plugin wired to host for the webhook tests.
func newWebhookPlugin(t *testing.T, host plugins.Host) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "pagerduty"})
	require.NoError(t, err)
	pp := p.(*Plugin)
	require.NoError(t, pp.PostInit(context.Background(), host))
	return pp
}

// pdMessage builds a single-message envelope JSON body for the given event type
// and incident_key.
func pdMessage(eventType, incidentKey string) []byte {
	env := pdWebhookEnvelope{Messages: []pdWebhookMessage{{Type: eventType}}}
	env.Messages[0].Data.Incident.IncidentKey = incidentKey
	b, _ := json.Marshal(env)
	return b
}

// postWebhook posts body to the receiver and returns the recorder.
func postWebhook(t *testing.T, p *Plugin, method string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/webhook/pagerduty", bytes.NewReader(body))
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

// recordDoc seeds a record document with the given hash and uid.
func recordDoc(hash, uid string) db.Document {
	return db.Document{"hash": hash, "uid": uid, "tenant_id": "default", "state": ""}
}

func TestHandleWebhookContract(t *testing.T) {
	var _ plugins.WebhookReceiver = (*Plugin)(nil)
	p := newWebhookPlugin(t, &stubHost{driver: &stubDB{}})
	require.Equal(t, "/pagerduty", p.WebhookPath())
}

func TestHandleWebhook_Acknowledge(t *testing.T) {
	host := &stubHost{driver: &stubDB{docs: []db.Document{recordDoc("abc123", "rec-1")}}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, pdMessage("incident.acknowledge", "abc123"))
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.Equal(t, float64(1), resp["updated"])

	require.Equal(t, "ack", host.driver.get("rec-1")["state"], "ack must set state=ack on the record")
}

func TestHandleWebhook_Resolve(t *testing.T) {
	host := &stubHost{driver: &stubDB{docs: []db.Document{recordDoc("abc123", "rec-1")}}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, pdMessage("incident.resolve", "abc123"))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "close", host.driver.get("rec-1")["state"], "resolve must set state=close")
}

func TestHandleWebhook_Unacknowledge(t *testing.T) {
	host := &stubHost{driver: &stubDB{docs: []db.Document{
		{"hash": "abc123", "uid": "rec-1", "tenant_id": "default", "state": "ack"},
	}}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, pdMessage("incident.unacknowledge", "abc123"))
	require.Equal(t, http.StatusOK, w.Code)

	doc := host.driver.get("rec-1")
	// Re-open: the final document must have state == "" OR no state field at
	// all (Risk #4) — never an error.
	if v, has := doc["state"]; has {
		require.Equal(t, "", v, "re-open must leave state empty if present")
	}
}

func TestHandleWebhook_NotFound(t *testing.T) {
	// No records seeded → both hash and uid lookups miss.
	host := &stubHost{driver: &stubDB{}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, pdMessage("incident.acknowledge", "nope"))
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleWebhook_AssignIgnored(t *testing.T) {
	// A seeded record proves no write happens even though one would match.
	host := &stubHost{driver: &stubDB{docs: []db.Document{recordDoc("abc123", "rec-1")}}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, pdMessage("incident.assign", "abc123"))
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, float64(0), resp["updated"], "assign is a no-op")
	require.Equal(t, "", host.driver.get("rec-1")["state"], "assign must not write any state")
}

func TestHandleWebhook_BadJSON(t *testing.T) {
	host := &stubHost{driver: &stubDB{}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, []byte(`{not-json`))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleWebhook_EmptyMessages(t *testing.T) {
	host := &stubHost{driver: &stubDB{}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, []byte(`{"messages":[]}`))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleWebhook_UIDFallback proves the hash-miss → uid lookup path.
func TestHandleWebhook_UIDFallback(t *testing.T) {
	// Record has no matching hash; incident_key equals its uid.
	host := &stubHost{driver: &stubDB{docs: []db.Document{
		{"hash": "", "uid": "rec-9", "tenant_id": "default", "state": ""},
	}}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, pdMessage("incident.acknowledge", "rec-9"))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ack", host.driver.get("rec-9")["state"], "uid-fallback lookup must patch the record")
}

func TestHandleWebhook_MultiMessage(t *testing.T) {
	host := &stubHost{driver: &stubDB{docs: []db.Document{
		recordDoc("hash-a", "rec-a"),
		recordDoc("hash-b", "rec-b"),
	}}}
	p := newWebhookPlugin(t, host)

	env := pdWebhookEnvelope{Messages: []pdWebhookMessage{
		{Type: "incident.acknowledge"},
		{Type: "incident.resolve"},
	}}
	env.Messages[0].Data.Incident.IncidentKey = "hash-a"
	env.Messages[1].Data.Incident.IncidentKey = "hash-b"
	body, _ := json.Marshal(env)

	w := postWebhook(t, p, http.MethodPost, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, float64(2), resp["updated"], "both messages must patch their records")
	require.Equal(t, "ack", host.driver.get("rec-a")["state"])
	require.Equal(t, "close", host.driver.get("rec-b")["state"])
}

func TestHandleWebhook_DBError(t *testing.T) {
	host := &stubHost{driver: &stubDB{
		docs:   []db.Document{recordDoc("abc123", "rec-1")},
		setErr: errBoom,
	}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodPost, pdMessage("incident.acknowledge", "abc123"))
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleWebhook_MethodNotAllowed(t *testing.T) {
	host := &stubHost{driver: &stubDB{}}
	p := newWebhookPlugin(t, host)

	w := postWebhook(t, p, http.MethodGet, nil)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// errBoom is a sentinel write error used by TestHandleWebhook_DBError.
var errBoom = errTest("boom")

type errTest string

func (e errTest) Error() string { return string(e) }
