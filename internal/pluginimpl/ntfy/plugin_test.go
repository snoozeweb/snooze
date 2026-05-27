package ntfy

import (
	"context"
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
	}
}

// newPluginForTest builds a Plugin with the http.Client overridden so that
// httptest servers are reachable without TLS.
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "ntfy"})
	require.NoError(t, err)
	np := p.(*Plugin)
	np.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return np
}

// captured is a goroutine-safe store for what the fake server received.
type captured struct {
	mu         sync.Mutex
	method     string
	path       string
	body       string
	title      string
	priority   string
	tags       string
	click      string
	authHeader string
	basicUser  string
	basicPass  string
}

// newRecordingServer starts an httptest server that stores the interesting
// parts of every inbound request and always responds 200 OK.
func newRecordingServer(t *testing.T) (*httptest.Server, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		c.mu.Lock()
		c.method = r.Method
		c.path = r.URL.Path
		c.body = string(body)
		c.title = r.Header.Get("Title")
		c.priority = r.Header.Get("Priority")
		c.tags = r.Header.Get("Tags")
		c.click = r.Header.Get("Click")
		c.authHeader = r.Header.Get("Authorization")
		c.basicUser = user
		c.basicPass = pass
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

// ---------------------------------------------------------------------------
// Registration and interface contract
// ---------------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "ntfy"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "ntfy"})
	require.NoError(t, err)
	require.Equal(t, "ntfy", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// ---------------------------------------------------------------------------
// Send — happy path
// ---------------------------------------------------------------------------

func TestSendHappyPath(t *testing.T) {
	srv, c := newRecordingServer(t)
	rec := sampleRecord()
	p := newPluginForTest(t)

	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"server": srv.URL,
			"topic":  "alerts",
		},
	})
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, http.MethodPost, c.method)
	require.Equal(t, "/alerts", c.path)

	// Default title template: "<severity> on <host>"
	require.Equal(t, "warning on db-1.example.com", c.title)

	// Default message template: "<message>"
	require.Equal(t, "disk full", c.body)

	// warning → priority 4
	require.Equal(t, "4", c.priority)

	// warning → tags "warning"
	require.Equal(t, "warning", c.tags)
}

// ---------------------------------------------------------------------------
// Send — custom title, message, priority, tags
// ---------------------------------------------------------------------------

func TestSendCustomFields(t *testing.T) {
	srv, c := newRecordingServer(t)
	p := newPluginForTest(t)

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"server":   srv.URL,
			"topic":    "ops",
			"title":    "ALERT: {{ .Host }}",
			"message":  "{{ .Severity }}: {{ .Message }}",
			"priority": "5",
			"tags":     "skull,warning",
			"click":    "https://snooze.example.com/alerts",
		},
	})
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, "/ops", c.path)
	require.Equal(t, "ALERT: db-1.example.com", c.title)
	require.Equal(t, "warning: disk full", c.body)
	require.Equal(t, "5", c.priority)
	require.Equal(t, "skull,warning", c.tags)
	require.Equal(t, "https://snooze.example.com/alerts", c.click)
}

// ---------------------------------------------------------------------------
// Send — severity → priority+tags derivation
// ---------------------------------------------------------------------------

func TestSendSeverityMapping(t *testing.T) {
	cases := []struct {
		severity     string
		wantPriority string
		wantTags     string
	}{
		{"info", "2", "information_source"},
		{"notice", "2", "information_source"},
		{"debug", "2", "information_source"},
		{"warning", "4", "warning"},
		{"error", "4", "warning"},
		{"err", "4", "warning"},
		{"critical", "5", "rotating_light"},
		{"emergency", "5", "rotating_light"},
		{"unknown_xyz", "2", "information_source"}, // graceful fallback
	}

	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			srv, c := newRecordingServer(t)
			p := newPluginForTest(t)
			rec := sampleRecord()
			rec.Severity = tc.severity

			err := p.Send(context.Background(), rec, plugins.NotificationPayload{
				Meta: map[string]any{
					"server": srv.URL,
					"topic":  "test",
				},
			})
			require.NoError(t, err)

			c.mu.Lock()
			defer c.mu.Unlock()
			require.Equal(t, tc.wantPriority, c.priority, "priority for severity=%s", tc.severity)
			require.Equal(t, tc.wantTags, c.tags, "tags for severity=%s", tc.severity)
		})
	}
}

// ---------------------------------------------------------------------------
// Send — authentication
// ---------------------------------------------------------------------------

func TestSendBearerToken(t *testing.T) {
	srv, c := newRecordingServer(t)
	p := newPluginForTest(t)

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"server": srv.URL,
			"topic":  "secure",
			"token":  "tk_s3cr3t",
		},
	})
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, "Bearer tk_s3cr3t", c.authHeader)
}

func TestSendBasicAuth(t *testing.T) {
	srv, c := newRecordingServer(t)
	p := newPluginForTest(t)

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"server":   srv.URL,
			"topic":    "secure",
			"username": "alice",
			"password": "wonderland",
		},
	})
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, "alice", c.basicUser)
	require.Equal(t, "wonderland", c.basicPass)
}

// Bearer takes precedence when both token and username are set.
func TestSendBearerTakesPrecedenceOverBasic(t *testing.T) {
	srv, c := newRecordingServer(t)
	p := newPluginForTest(t)

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"server":   srv.URL,
			"topic":    "secure",
			"token":    "tk_bearer",
			"username": "alice",
			"password": "wonderland",
		},
	})
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, "Bearer tk_bearer", c.authHeader)
	// When Bearer is set, SetBasicAuth must not have been called too.
	require.Empty(t, c.basicUser, "basic auth must not be applied when token is set")
}

// ---------------------------------------------------------------------------
// Send — error paths
// ---------------------------------------------------------------------------

func TestSendNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"access denied"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"server": srv.URL,
			"topic":  "blocked",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
	require.Contains(t, err.Error(), "access denied")
}

func TestSendMissingTopicReturnsError(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"server": "https://ntfy.sh",
			// topic intentionally absent
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "topic")
}

func TestSendNilMetaReturnsError(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "topic")
}

// ---------------------------------------------------------------------------
// Template rendering
// ---------------------------------------------------------------------------

func TestSendTemplateRendersRecord(t *testing.T) {
	srv, c := newRecordingServer(t)
	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Host = "web-prod-01"
	rec.Severity = "critical"
	rec.Message = "OOM killed"

	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"server":  srv.URL,
			"topic":   "alerts",
			"title":   "{{ .Severity }} — {{ .Host }}",
			"message": "host={{ .Host }} msg={{ .Message }} uid={{ .UID }}",
		},
	})
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, "critical — web-prod-01", c.title)
	require.Equal(t, "host=web-prod-01 msg=OOM killed uid=rec-1", c.body)
}

// TestSendBadTemplatReturnsError verifies that a malformed title template
// yields a render error instead of silently sending a broken notification.
func TestSendBadTemplateReturnsError(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"server": "https://ntfy.sh",
			"topic":  "test",
			"title":  "{{ .UnknownFunc .Host }}", // unknown function → parse error
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "title")
}
