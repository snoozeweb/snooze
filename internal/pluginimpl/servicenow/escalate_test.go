package servicenow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// snowRecorder is a ServiceNow stand-in that records every request. Tests
// assert on how many incident CREATES happened — the behaviour this work fixes
// is an escalation POSTing a brand-new incident.
type snowRecorder struct {
	mu   sync.Mutex
	reqs []snowReq

	// lookupState is the state the scripted lookup reports ("" = no incident
	// found, "2" = in progress, "6" = resolved).
	lookupState string
	// lookupFound controls whether the query returns a row at all.
	lookupFound bool
	// lookupStatus overrides the lookup response status (0 = 200).
	lookupStatus int
}

type snowReq struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

func newSNOWRecorder(t *testing.T) (*snowRecorder, *httptest.Server) {
	t.Helper()
	r := &snowRecorder{lookupFound: true, lookupState: "2"}
	srv := httptest.NewServer(http.HandlerFunc(r.handle))
	t.Cleanup(srv.Close)
	return r, srv
}

func (s *snowRecorder) handle(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, snowReq{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: body})
	status, found, state := s.lookupStatus, s.lookupFound, s.lookupState
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if !found {
			_, _ = w.Write([]byte(`{"result":[]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]any{{"sys_id": "SYS001", "state": state}},
		})
	case http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":{"sys_id":"SYS002"}}`))
	default:
		_, _ = w.Write([]byte(`{"result":{}}`))
	}
}

func (s *snowRecorder) calls() []snowReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]snowReq, len(s.reqs))
	copy(out, s.reqs)
	return out
}

func (s *snowRecorder) countOf(method string) int {
	n := 0
	for _, r := range s.calls() {
		if r.Method == method {
			n++
		}
	}
	return n
}

func (s *snowRecorder) firstOf(t *testing.T, method string) snowReq {
	t.Helper()
	for _, r := range s.calls() {
		if r.Method == method {
			return r
		}
	}
	t.Fatalf("no %s request was made; calls=%v", method, s.calls())
	return snowReq{}
}

func escalationPayload(instanceURL string, esc plugins.Escalation) plugins.NotificationPayload {
	return plugins.NotificationPayload{Meta: baseMeta(instanceURL), Escalation: esc}
}

// TestEscalationUpdatesInsteadOfCreating is the load-bearing test: an operator
// with one problem must end up with one incident, not one per escalation.
func TestEscalationUpdatesInsteadOfCreating(t *testing.T) {
	snow, srv := newSNOWRecorder(t)
	p := newPluginForTest(t)

	err := p.Send(context.Background(), sampleRecord(),
		escalationPayload(srv.URL, plugins.Escalation{Count: 1, Reason: "timeout"}))
	require.NoError(t, err)

	require.Zero(t, snow.countOf(http.MethodPost), "a re-escalation must not POST a new incident")
	require.Equal(t, 1, snow.countOf(http.MethodPatch))

	patch := snow.firstOf(t, http.MethodPatch)
	require.Equal(t, "/api/now/table/incident/SYS001", patch.Path)
	notes, _ := patch.Body["work_notes"].(string)
	require.Contains(t, notes, "Re-escalated by Snooze (#1)")
	require.Contains(t, notes, "reason: timeout")
	require.Contains(t, notes, "db-1.example.com")

	// The lookup must key on the alert's stable correlation id.
	lookup := snow.firstOf(t, http.MethodGet)
	require.Contains(t, lookup.Query, "correlation_id=abc123")
}

// A first delivery still creates, unchanged.
func TestFirstDeliveryStillCreates(t *testing.T) {
	snow, srv := newSNOWRecorder(t)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		escalationPayload(srv.URL, plugins.Escalation{})))

	require.Equal(t, 1, snow.countOf(http.MethodPost))
	require.Zero(t, snow.countOf(http.MethodGet), "a first delivery must not pay for a lookup")
}

// The alert escalated but ServiceNow has no incident for it (deleted, or the
// first delivery never landed) — a create is then the right answer.
func TestEscalationWithNoIncidentCreates(t *testing.T) {
	snow, srv := newSNOWRecorder(t)
	snow.lookupFound = false
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		escalationPayload(srv.URL, plugins.Escalation{Count: 1})))

	require.Equal(t, 1, snow.countOf(http.MethodPost))
	require.Zero(t, snow.countOf(http.MethodPatch))
}

// A broken lookup must not drop an escalating alert: a duplicate ticket beats a
// missed page.
func TestEscalationLookupFailureFallsBackToCreate(t *testing.T) {
	snow, srv := newSNOWRecorder(t)
	snow.lookupStatus = http.StatusInternalServerError
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		escalationPayload(srv.URL, plugins.Escalation{Count: 1})))

	require.Equal(t, 1, snow.countOf(http.MethodPost))
}

// A resolved or closed incident is pulled back to In Progress; one already
// being worked is left where it is.
func TestEscalationReopensResolvedIncident(t *testing.T) {
	for name, tc := range map[string]struct {
		state     string
		wantState any
	}{
		"resolved":    {state: "6", wantState: "2"},
		"closed":      {state: "7", wantState: "2"},
		"in_progress": {state: "2", wantState: nil},
		"new":         {state: "1", wantState: nil},
	} {
		t.Run(name, func(t *testing.T) {
			snow, srv := newSNOWRecorder(t)
			snow.lookupState = tc.state
			p := newPluginForTest(t)

			require.NoError(t, p.Send(context.Background(), sampleRecord(),
				escalationPayload(srv.URL, plugins.Escalation{Count: 1})))

			patch := snow.firstOf(t, http.MethodPatch)
			if tc.wantState == nil {
				require.NotContains(t, patch.Body, "state",
					"an incident someone is working must not have its state reset")
			} else {
				require.Equal(t, tc.wantState, patch.Body["state"])
			}
		})
	}
}

// Urgency/impact rise only when severity rose, and never when the action pins
// them explicitly.
func TestEscalationUrgencyBump(t *testing.T) {
	t.Run("severity_rose_auto", func(t *testing.T) {
		snow, srv := newSNOWRecorder(t)
		p := newPluginForTest(t)

		require.NoError(t, p.Send(context.Background(), sampleRecord(),
			escalationPayload(srv.URL, plugins.Escalation{Count: 1, Trend: "up", PreviousSeverity: "warning"})))

		patch := snow.firstOf(t, http.MethodPatch)
		require.Equal(t, "1", patch.Body["urgency"]) // critical → 1
		require.Equal(t, "1", patch.Body["impact"])
		notes, _ := patch.Body["work_notes"].(string)
		require.Contains(t, notes, "(was warning)")
	})

	t.Run("severity_flat", func(t *testing.T) {
		snow, srv := newSNOWRecorder(t)
		p := newPluginForTest(t)

		require.NoError(t, p.Send(context.Background(), sampleRecord(),
			escalationPayload(srv.URL, plugins.Escalation{Count: 1, Trend: "same"})))

		patch := snow.firstOf(t, http.MethodPatch)
		require.NotContains(t, patch.Body, "urgency")
		require.NotContains(t, patch.Body, "impact")
	})

	t.Run("explicit_urgency_is_respected", func(t *testing.T) {
		snow, srv := newSNOWRecorder(t)
		p := newPluginForTest(t)

		payload := escalationPayload(srv.URL, plugins.Escalation{Count: 1, Trend: "up"})
		payload.Meta["urgency"] = "3"
		payload.Meta["impact"] = "3"
		require.NoError(t, p.Send(context.Background(), sampleRecord(), payload))

		patch := snow.firstOf(t, http.MethodPatch)
		require.NotContains(t, patch.Body, "urgency",
			"an action that pins urgency must not have it overridden by an escalation")
	})
}

// Close still resolves via the same shared lookup.
func TestCloseStillResolves(t *testing.T) {
	snow, srv := newSNOWRecorder(t)
	p := newPluginForTest(t)

	rec := sampleRecord()
	rec.State = "close"
	require.NoError(t, p.Send(context.Background(), rec, escalationPayload(srv.URL, plugins.Escalation{Count: 2})))

	patch := snow.firstOf(t, http.MethodPatch)
	require.Equal(t, "6", patch.Body["state"])
	require.Equal(t, "Resolved", patch.Body["close_code"])
	require.Zero(t, snow.countOf(http.MethodPost))
}

// correlationID falls back to the uid for a record that never went through
// aggregaterule, so the lookup still has a stable key.
func TestCorrelationIDFallsBackToUID(t *testing.T) {
	require.Equal(t, "abc123", correlationID(sampleRecord()))
	rec := sampleRecord()
	rec.Hash = ""
	require.Equal(t, "rec-1", correlationID(rec))
}

func TestBuildEscalationNoteMinimal(t *testing.T) {
	note := buildEscalationNote(snoozetypes.Record{Host: "h", Message: "m", Severity: "s"},
		plugins.Escalation{})
	require.True(t, strings.HasPrefix(note, "Re-escalated by Snooze"))
	require.Contains(t, note, "Host: h")
	require.NotContains(t, note, "reason:")
}
