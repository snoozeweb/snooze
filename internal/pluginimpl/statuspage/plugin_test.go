package statuspage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "nagios",
		Severity: "critical",
		Message:  "connection refused",
	}
}

func closeRecord() snoozetypes.Record {
	r := sampleRecord()
	r.State = "close"
	return r
}

// newPluginForTest builds a Plugin with newClient overridden to a simple
// plain-HTTP client so that httptest servers are reachable without TLS.
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "statuspage"})
	require.NoError(t, err)
	sp, ok := p.(*Plugin)
	require.True(t, ok)
	sp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return sp
}

// baseMeta returns the minimal payload.Meta needed for happy-path calls.
// api_base is left out so individual tests can inject their httptest URL.
func baseMeta(srv *httptest.Server) map[string]any {
	return map[string]any{
		"api_key":  "test-key",
		"page_id":  "page123",
		"api_base": srv.URL,
	}
}

// ---------------------------------------------------------------------------
// Registration & contract
// ---------------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "statuspage"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)

	p, err := factory(plugins.Metadata{Name: "statuspage"})
	require.NoError(t, err)
	require.Equal(t, "statuspage", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// ---------------------------------------------------------------------------
// Create incident
// ---------------------------------------------------------------------------

// TestCreateIncident verifies that a firing record causes:
//   - POST /v1/pages/{page_id}/incidents
//   - Authorization: OAuth <api_key> header
//   - JSON body fields: incident.name, incident.status, incident.body
func TestCreateIncident(t *testing.T) {
	var (
		mu                          sync.Mutex
		gotMethod, gotPath, gotAuth string
		gotBody                     []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"inc-001"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv),
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/v1/pages/page123/incidents", gotPath)
	require.Equal(t, "OAuth test-key", gotAuth)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	inc, ok := payload["incident"].(map[string]any)
	require.True(t, ok, "body must have an 'incident' object")

	// Default name template: "{{ .Severity }}: {{ .Host }}"
	require.Equal(t, "critical: db-1.example.com", inc["name"])
	// Default status: "investigating"
	require.Equal(t, "investigating", inc["status"])
	// Default body template: "{{ .Message }}"
	require.Equal(t, "connection refused", inc["body"])
}

// TestCreateIncidentWithComponent verifies that when component_id is set
// the incident body includes component_ids and components maps.
func TestCreateIncidentWithComponent(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"inc-002"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv)
	meta["component_id"] = "comp-xyz"
	meta["initial_status"] = "identified"

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	inc := payload["incident"].(map[string]any)

	require.Equal(t, "identified", inc["status"])
	cids, ok := inc["component_ids"].([]any)
	require.True(t, ok, "component_ids must be a JSON array")
	require.Contains(t, cids, "comp-xyz")

	comps, ok := inc["components"].(map[string]any)
	require.True(t, ok, "components must be a JSON object")
	require.Equal(t, "identified", comps["comp-xyz"])
}

// TestCreateIncidentWithImpact verifies that impact_override is included
// in the incident body when the impact field is set.
func TestCreateIncidentWithImpact(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"inc-003"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv)
	meta["impact"] = "critical"

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	inc := payload["incident"].(map[string]any)
	require.Equal(t, "critical", inc["impact_override"])
}

// TestCreateIncidentCustomTemplates verifies that name and body fields are
// rendered as Go text/templates over the record.
func TestCreateIncidentCustomTemplates(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"inc-004"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv)
	meta["name"] = "ALERT: {{ .Host }} ({{ .Severity }})"
	meta["body"] = "Source={{ .Source }} Msg={{ .Message }}"

	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	inc := payload["incident"].(map[string]any)
	require.Equal(t, "ALERT: db-1.example.com (critical)", inc["name"])
	require.Equal(t, "Source=nagios Msg=connection refused", inc["body"])
}

// ---------------------------------------------------------------------------
// Resolve (close) path
// ---------------------------------------------------------------------------

// TestResolveIncident verifies the close path:
//  1. GET /v1/pages/{page_id}/incidents/unresolved
//  2. find the most-recent incident whose name matches the rendered template
//  3. PATCH /v1/pages/{page_id}/incidents/{id} with status=resolved
func TestResolveIncident(t *testing.T) {
	const matchingName = "critical: db-1.example.com"

	var (
		mu        sync.Mutex
		calls     []string // collected "METHOD PATH"
		patchBody []byte
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path)

		switch r.Method {
		case http.MethodGet:
			// Return two incidents; the second one matches the name.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := []map[string]any{
				{"id": "old-inc", "name": "other incident"},
				{"id": "inc-abc", "name": matchingName},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case http.MethodPatch:
			patchBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"inc-abc","status":"resolved"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), closeRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv),
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{
		"GET /v1/pages/page123/incidents/unresolved",
		"PATCH /v1/pages/page123/incidents/inc-abc",
	}, calls)

	var pp map[string]any
	require.NoError(t, json.Unmarshal(patchBody, &pp))
	inc := pp["incident"].(map[string]any)
	require.Equal(t, "resolved", inc["status"])
}

// TestResolveNoMatchIsNoop verifies that if no unresolved incident matches
// the name, the plugin succeeds (no-op) without making a PATCH call.
func TestResolveNoMatchIsNoop(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "inc-999", "name": "completely unrelated"},
		})
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), closeRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv),
	})
	require.NoError(t, err) // no-op is not an error

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"GET /v1/pages/page123/incidents/unresolved"}, calls,
		"PATCH must not be called when no incident matches")
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

// TestNon2xxCreateReturnsError verifies that a non-201 response from the
// create endpoint is surfaced as an error.
func TestNon2xxCreateReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
}

// TestNon2xxResolveReturnsError verifies that a non-2xx response during the
// resolve GET is surfaced as an error.
func TestNon2xxResolveGetReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), closeRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
}

// TestMissingAPIKeyReturnsError verifies that omitting api_key is caught
// before any HTTP call is made.
func TestMissingAPIKeyReturnsError(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"page_id": "page123",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
}

// TestMissingPageIDReturnsError verifies that omitting page_id is caught
// before any HTTP call is made.
func TestMissingPageIDReturnsError(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"api_key": "test-key",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "page_id")
}
