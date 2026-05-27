package opsgenie

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
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
		Hash:     "abc123hash",
	}
}

// newPluginForTest builds a Plugin with newClient overridden to a plain
// http.Client (no TLS, honours timeout). The httptest server uses plain HTTP
// so the default TLS-aware transport is not needed.
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "opsgenie"})
	require.NoError(t, err)
	op := p.(*Plugin)
	op.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return op
}

// baseMeta returns the minimum valid Meta map for the given httptest server URL.
func baseMeta(apiBase string) map[string]any {
	return map[string]any{
		"api_key":  "test-genie-key",
		"api_base": apiBase,
	}
}

// ---------------------------------------------------------------------------
// Registration & contract
// ---------------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "opsgenie"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)

	p, err := factory(plugins.Metadata{Name: "opsgenie"})
	require.NoError(t, err)
	require.Equal(t, "opsgenie", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// ---------------------------------------------------------------------------
// Create alert
// ---------------------------------------------------------------------------

// captured holds the fields the test server observed from a single request.
type captured struct {
	mu     sync.Mutex
	method string
	path   string
	query  string
	auth   string
	body   []byte
}

func (c *captured) set(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.method = r.Method
	c.path = r.URL.Path
	c.query = r.URL.RawQuery
	c.auth = r.Header.Get("Authorization")
	c.body, _ = io.ReadAll(r.Body)
}

func (c *captured) get() (method, path, query, auth string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.path, c.query, c.auth, c.body
}

func TestSendCreateAlert(t *testing.T) {
	var capt captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.set(r)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.NoError(t, err)

	method, path, _, auth, body := capt.get()
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "/v2/alerts", path)
	require.Equal(t, "GenieKey test-genie-key", auth)

	var req createRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.Equal(t, rec.Hash, req.Alias, "alias should be rec.Hash")
	require.Equal(t, rec.Message, req.Message)
	require.Equal(t, "P3", req.Priority, "warning → P3")
	require.Equal(t, "Snooze", req.Source)
}

func TestSendCreateAliasFromUID(t *testing.T) {
	// When Hash is empty, alias should fall back to UID.
	var capt captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.set(r)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Hash = "" // force fallback to UID
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.NoError(t, err)

	_, _, _, _, body := capt.get()
	var req createRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.Equal(t, rec.UID, req.Alias, "alias should be rec.UID when Hash is empty")
}

func TestSendPriorityMapping(t *testing.T) {
	cases := []struct {
		severity string
		want     string
	}{
		{"emergency", "P1"},
		{"critical", "P1"},
		{"error", "P2"},
		{"err", "P2"},
		{"warning", "P3"},
		{"notice", "P4"},
		{"info", "P5"},
		{"debug", "P5"},
		{"", "P5"},
	}
	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			var capt captured
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capt.set(r)
				w.WriteHeader(http.StatusAccepted)
			}))
			t.Cleanup(srv.Close)

			p := newPluginForTest(t)
			rec := sampleRecord()
			rec.Severity = tc.severity
			require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
				Meta: baseMeta(srv.URL),
			}))
			_, _, _, _, body := capt.get()
			var req createRequest
			require.NoError(t, json.Unmarshal(body, &req))
			require.Equal(t, tc.want, req.Priority)
		})
	}
}

func TestSendPriorityOverride(t *testing.T) {
	// When priority is set to a fixed value (not "auto"), the severity mapping
	// must be bypassed.
	var capt captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.set(r)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["priority"] = "P1"
	rec := sampleRecord()
	rec.Severity = "info" // would map to P5 under "auto"
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}))

	_, _, _, _, body := capt.get()
	var req createRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.Equal(t, "P1", req.Priority)
}

func TestSendTags(t *testing.T) {
	var capt captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.set(r)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["tags"] = "prod, snooze, db"
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	_, _, _, _, body := capt.get()
	var req createRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.Equal(t, []string{"prod", "snooze", "db"}, req.Tags)
}

// ---------------------------------------------------------------------------
// Close alert
// ---------------------------------------------------------------------------

func TestSendCloseAlert(t *testing.T) {
	var capt captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.set(r)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.NoError(t, err)

	method, path, query, auth, _ := capt.get()
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "/v2/alerts/"+rec.Hash+"/close", path)
	require.Equal(t, "identifierType=alias", query)
	require.Equal(t, "GenieKey test-genie-key", auth)
}

func TestSendCloseAliasFromUID(t *testing.T) {
	var capt captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.set(r)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Hash = ""
	rec.State = "close"
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	}))

	_, path, _, _, _ := capt.get()
	require.Equal(t, "/v2/alerts/"+rec.UID+"/close", path)
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

func TestSendNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Invalid API key"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "Invalid API key")
}

func TestSendMissingAPIKey(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
}

func TestSendNilMeta(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
}

// ---------------------------------------------------------------------------
// Region selector
// ---------------------------------------------------------------------------

func TestRegionEU(t *testing.T) {
	cfg, err := configFromMeta(map[string]any{
		"api_key": "key",
		"region":  "eu",
	})
	require.NoError(t, err)
	require.Equal(t, apiBaseEU, cfg.APIBase)
}

func TestRegionUS(t *testing.T) {
	cfg, err := configFromMeta(map[string]any{
		"api_key": "key",
		"region":  "us",
	})
	require.NoError(t, err)
	require.Equal(t, apiBaseUS, cfg.APIBase)
}

func TestRegionDefaultsToUS(t *testing.T) {
	cfg, err := configFromMeta(map[string]any{
		"api_key": "key",
	})
	require.NoError(t, err)
	require.Equal(t, apiBaseUS, cfg.APIBase)
}

func TestAPIBaseOverridesRegion(t *testing.T) {
	custom := "https://my-opsgenie-proxy.internal"
	cfg, err := configFromMeta(map[string]any{
		"api_key":  "key",
		"region":   "eu",
		"api_base": custom,
	})
	require.NoError(t, err)
	require.Equal(t, custom, cfg.APIBase)
}
