package servicenow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
		Hash:     "abc123",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "critical",
		Message:  "disk full on /var",
	}
}

func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "servicenow"})
	require.NoError(t, err)
	sp, ok := p.(*Plugin)
	require.True(t, ok)
	// Override the HTTP client builder so httptest servers are used without
	// proxy/TLS configuration.
	sp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return sp
}

func baseMeta(instanceURL string) map[string]any {
	return map[string]any{
		"instance_url": instanceURL,
		"username":     "admin",
		"password":     "s3cret",
	}
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "servicenow"))
}

// ---------------------------------------------------------------------------
// Interface contract
// ---------------------------------------------------------------------------

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "servicenow"})
	require.NoError(t, err)
	require.Equal(t, "servicenow", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// ---------------------------------------------------------------------------
// Create path
// ---------------------------------------------------------------------------

func TestSendCreate_BasicAuthAndPayload(t *testing.T) {
	type captured struct {
		method      string
		path        string
		authHeader  string
		accept      string
		contentType string
		body        map[string]any
	}
	var mu sync.Mutex
	var got captured

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		got.method = r.Method
		got.path = r.URL.Path
		got.authHeader = r.Header.Get("Authorization")
		got.accept = r.Header.Get("Accept")
		got.contentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":{"sys_id":"SYS001"}}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/api/now/table/incident", got.path)
	// Basic auth: "admin:s3cret" → base64 = "YWRtaW46czNjcmV0"
	require.Equal(t, "Basic YWRtaW46czNjcmV0", got.authHeader)
	require.Equal(t, "application/json", got.accept)
	require.Equal(t, "application/json", got.contentType)
	// Payload fields
	require.Equal(t, rec.Message, got.body["short_description"])
	require.NotEmpty(t, got.body["description"])
	require.Equal(t, "1", got.body["urgency"]) // critical → 1
	require.Equal(t, "1", got.body["impact"])  // critical → 1
	require.Equal(t, rec.Hash, got.body["correlation_id"])
}

func TestSendCreate_CorrelationFallsBackToUID(t *testing.T) {
	var mu sync.Mutex
	var gotCorrID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		gotCorrID, _ = body["correlation_id"].(string)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":{"sys_id":"SYS001"}}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Hash = "" // no hash → fall back to UID
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, rec.UID, gotCorrID)
}

func TestSendCreate_ExplicitUrgencyImpact(t *testing.T) {
	var mu sync.Mutex
	var gotUrgency, gotImpact string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		gotUrgency, _ = body["urgency"].(string)
		gotImpact, _ = body["impact"].(string)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":{"sys_id":"SYS001"}}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["urgency"] = "3"
	meta["impact"] = "2"
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "3", gotUrgency)
	require.Equal(t, "2", gotImpact)
}

func TestSendCreate_CustomTable(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":{"sys_id":"SYS001"}}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["table"] = "em_event"
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "/api/now/table/em_event", gotPath)
}

// ---------------------------------------------------------------------------
// Resolve path
// ---------------------------------------------------------------------------

// TestSendResolve verifies that when rec.State == "close", the plugin:
//  1. GETs the table with sysparm_query=correlation_id={id} to find the sys_id
//  2. PATCHes {table}/{sys_id} with state=6, close_code, close_notes
func TestSendResolve(t *testing.T) {
	type reqCapture struct {
		method string
		path   string
		query  string
		body   map[string]any
	}
	var mu sync.Mutex
	var reqs []reqCapture

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		reqs = append(reqs, reqCapture{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			body:   body,
		})
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			// Respond to the lookup with one matching record.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":[{"sys_id":"SYS999"}]}`))
		} else {
			// PATCH response.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":{"sys_id":"SYS999"}}`))
		}
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, reqs, 2, "expected GET lookup then PATCH")

	// First request: GET lookup.
	get := reqs[0]
	require.Equal(t, http.MethodGet, get.method)
	require.Equal(t, "/api/now/table/incident", get.path)
	require.Contains(t, get.query, "correlation_id="+rec.Hash)
	require.Contains(t, get.query, "sysparm_limit=1")

	// Second request: PATCH resolve.
	patch := reqs[1]
	require.Equal(t, http.MethodPatch, patch.method)
	require.Equal(t, "/api/now/table/incident/SYS999", patch.path)
	require.Equal(t, "6", patch.body["state"])
	require.Equal(t, "Resolved", patch.body["close_code"])
	require.NotEmpty(t, patch.body["close_notes"])
}

func TestSendResolve_NoMatchIsNoOp(t *testing.T) {
	// When the GET finds no records, PATCH must not be called.
	var mu sync.Mutex
	var patchCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPatch {
			patchCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":[]}`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.NoError(t, err) // no-op, not an error

	mu.Lock()
	defer mu.Unlock()
	require.False(t, patchCalled, "PATCH must not be called when no record matches")
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestSendCreate_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"Access denied"}}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
}

func TestSendCreate_MissingInstanceURL(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"username": "admin",
			"password": "s3cret",
		},
	})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "instance_url")
}

func TestSendCreate_MissingUsername(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"instance_url": "https://dev.service-now.com",
			"password":     "s3cret",
		},
	})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "username")
}

// ---------------------------------------------------------------------------
// Severity → urgency/impact mapping
// ---------------------------------------------------------------------------

func TestSeverityMapping(t *testing.T) {
	cases := []struct {
		severity string
		want     string
	}{
		{"emergency", "1"},
		{"critical", "1"},
		{"error", "2"},
		{"err", "2"},
		{"warning", "2"},
		{"warn", "2"},
		{"notice", "3"},
		{"info", "3"},
		{"debug", "3"},
		{"unknown", "3"},
	}
	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			require.Equal(t, tc.want, severityToLevel(tc.severity))
		})
	}
}
