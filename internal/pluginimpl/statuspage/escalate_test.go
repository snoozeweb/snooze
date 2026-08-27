package statuspage

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
)

type spReq struct {
	Method string
	Path   string
	Body   map[string]any
}

// spRecorder is a Statuspage stand-in that records every call and serves a
// scripted list of unresolved incidents.
type spRecorder struct {
	mu   sync.Mutex
	reqs []spReq

	// unresolved is what GET /incidents/unresolved returns.
	unresolved []map[string]any
	// listStatus overrides the list response status (0 = 200).
	listStatus int
}

func newSPRecorder(t *testing.T, name string) (*spRecorder, *httptest.Server) {
	t.Helper()
	r := &spRecorder{
		unresolved: []map[string]any{
			{"id": "inc-1", "name": name, "status": "investigating"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(r.handle))
	t.Cleanup(srv.Close)
	return r, srv
}

func (s *spRecorder) handle(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, spReq{Method: r.Method, Path: r.URL.Path, Body: body})
	list, status := s.unresolved, s.listStatus
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/unresolved"):
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(list)
	case r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"inc-new"}`))
	default:
		_, _ = w.Write([]byte(`{"id":"inc-1"}`))
	}
}

func (s *spRecorder) calls() []spReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]spReq, len(s.reqs))
	copy(out, s.reqs)
	return out
}

func (s *spRecorder) countOf(method string) int {
	n := 0
	for _, c := range s.calls() {
		if c.Method == method {
			n++
		}
	}
	return n
}

func (s *spRecorder) firstOf(t *testing.T, method string) spReq {
	t.Helper()
	for _, c := range s.calls() {
		if c.Method == method {
			return c
		}
	}
	t.Fatalf("no %s request; calls=%v", method, s.calls())
	return spReq{}
}

// The default name template, rendered for sampleRecord(), is what the lookup
// matches on. Derived here rather than hardcoded so the test survives a
// template change.
func renderedName(t *testing.T) string {
	t.Helper()
	name, err := renderTemplate("name", defaultNameTmpl, sampleRecord())
	require.NoError(t, err)
	return name
}

// TestEscalationUpdatesInsteadOfCreating: a public status page is the worst
// place for a duplicate — two open incidents for one outage tells customers the
// wrong story.
func TestEscalationUpdatesInsteadOfCreating(t *testing.T) {
	sp, srv := newSPRecorder(t, renderedName(t))
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       baseMeta(srv),
		Escalation: plugins.Escalation{Count: 2, Reason: "timeout"},
	}))

	require.Zero(t, sp.countOf(http.MethodPost), "a re-escalation must not open a second incident")
	patch := sp.firstOf(t, http.MethodPatch)
	require.Equal(t, "/v1/pages/page123/incidents/inc-1", patch.Path)

	incident, _ := patch.Body["incident"].(map[string]any)
	body, _ := incident["body"].(string)
	require.Contains(t, body, "Re-escalated")
	// This page faces CUSTOMERS. The escalation ordinal and the internal reason
	// ("timeout" = a Snooze ack deadline elapsed) must not appear on it.
	require.NotContains(t, body, "#2")
	require.NotContains(t, body, "timeout")
}

// The lifecycle advances one step, and never backwards past a status an
// operator moved it to.
func TestEscalationStatusProgression(t *testing.T) {
	for name, tc := range map[string]struct {
		current string
		want    any
	}{
		"investigating_advances": {current: "investigating", want: "identified"},
		"identified_holds":       {current: "identified", want: nil},
		"monitoring_holds":       {current: "monitoring", want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			sp, srv := newSPRecorder(t, renderedName(t))
			sp.unresolved = []map[string]any{
				{"id": "inc-1", "name": renderedName(t), "status": tc.current},
			}
			p := newPluginForTest(t)
			meta := baseMeta(srv)
			meta["component_id"] = "comp-1"

			require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
				Meta:       meta,
				Escalation: plugins.Escalation{Count: 1},
			}))

			incident, _ := sp.firstOf(t, http.MethodPatch).Body["incident"].(map[string]any)
			if tc.want == nil {
				require.NotContains(t, incident, "status",
					"an operator who advanced the incident must not be dragged back")
			} else {
				require.Equal(t, tc.want, incident["status"])
			}
			// An escalation must never send `components`: those take component
			// statuses (operational / degraded_performance / ...), not incident
			// statuses, so Statuspage would reject the PATCH and the escalation
			// body would be lost with it.
			require.NotContains(t, incident, "components")
		})
	}
}

// Nothing open under this name (resolved in between, or the first delivery never
// landed) → a create is correct.
func TestEscalationWithNoOpenIncidentCreates(t *testing.T) {
	sp, srv := newSPRecorder(t, renderedName(t))
	sp.unresolved = nil
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       baseMeta(srv),
		Escalation: plugins.Escalation{Count: 1},
	}))
	require.Equal(t, 1, sp.countOf(http.MethodPost))
}

// A broken list call must not drop the outage update.
func TestEscalationListFailureFallsBackToCreate(t *testing.T) {
	sp, srv := newSPRecorder(t, renderedName(t))
	sp.listStatus = http.StatusInternalServerError
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       baseMeta(srv),
		Escalation: plugins.Escalation{Count: 1},
	}))
	require.Equal(t, 1, sp.countOf(http.MethodPost))
}

// A first delivery still creates and pays for no lookup.
func TestFirstDeliveryStillCreates(t *testing.T) {
	sp, srv := newSPRecorder(t, renderedName(t))
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: baseMeta(srv)}))

	require.Equal(t, 1, sp.countOf(http.MethodPost))
	require.Zero(t, sp.countOf(http.MethodGet))
}

func TestNextStatus(t *testing.T) {
	require.Equal(t, "identified", nextStatus("investigating"))
	require.Empty(t, nextStatus("identified"))
	require.Empty(t, nextStatus("monitoring"))
	require.Empty(t, nextStatus(""))
}

func TestEscalationBodyOmitsInternalDetail(t *testing.T) {
	require.Equal(t, "Re-escalated: boom",
		escalationBody("boom", plugins.Escalation{Count: 2, Reason: "manual", Actor: "alice"}),
		"a customer-facing page must not carry the ordinal, the reason or the operator")
	require.Equal(t, "Re-escalated", escalationBody("", plugins.Escalation{}))
}
