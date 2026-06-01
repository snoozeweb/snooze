package teams

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

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID: "rec-1", Host: "db-1", Source: "syslog", Severity: "critical", Message: "disk full",
	}
}

func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "teams"})
	require.NoError(t, err)
	tp, ok := p.(*Plugin)
	require.True(t, ok)
	tp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return tp
}

type capturedRequest struct {
	mu     sync.Mutex
	method string
	ct     string
	body   []byte
}

func (c *capturedRequest) record(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.method = r.Method
	c.ct = r.Header.Get("Content-Type")
	c.body = b
}

func (c *capturedRequest) snapshot() (method, ct string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.ct, c.body
}

func newCaptureSrv(t *testing.T, status int) (*httptest.Server, *capturedRequest) {
	t.Helper()
	capt := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.record(r)
		w.WriteHeader(status)
		_, _ = w.Write([]byte("1"))
	}))
	t.Cleanup(srv.Close)
	return srv, capt
}

// testPayload captures the outer envelope plus the raw Adaptive Card so tests
// can assert on substrings without modelling the whole card schema.
type testPayload struct {
	Type        string `json:"type"`
	Attachments []struct {
		ContentType string          `json:"contentType"`
		Content     json.RawMessage `json:"content"`
	} `json:"attachments"`
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "teams"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "teams"})
	require.NoError(t, err)
	require.Equal(t, "teams", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

func TestSendAdaptiveCard(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK)
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	}))

	method, ct, body := capt.snapshot()
	require.Equal(t, http.MethodPost, method)
	require.Contains(t, ct, "application/json")

	var got testPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "message", got.Type)
	require.Len(t, got.Attachments, 1)
	require.Equal(t, "application/vnd.microsoft.card.adaptive", got.Attachments[0].ContentType)

	card := string(got.Attachments[0].Content)
	require.Contains(t, card, "AdaptiveCard")
	require.Contains(t, card, "critical on db-1")
	require.Contains(t, card, "disk full")
	require.Contains(t, card, "Attention")
}

func TestSendTemplate(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK)
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"title":       "ALERT {{ .Host }}",
			"message":     "msg={{ .Message }}",
		},
	}))
	_, _, body := capt.snapshot()
	require.Contains(t, string(body), "ALERT db-1")
	require.Contains(t, string(body), "msg=disk full")
}

func TestSendSeverityColors(t *testing.T) {
	cases := []struct{ severity, want string }{
		{"info", "Good"}, {"notice", "Good"}, {"debug", "Good"},
		{"warning", "Warning"},
		{"error", "Attention"}, {"critical", "Attention"}, {"emergency", "Attention"},
		{"weird-level", "Attention"},
	}
	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			srv, capt := newCaptureSrv(t, http.StatusOK)
			p := newPluginForTest(t)
			rec := sampleRecord()
			rec.Severity = tc.severity
			require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
				Meta: map[string]any{"webhook_url": srv.URL},
			}))
			_, _, body := capt.snapshot()
			require.Contains(t, string(body), tc.want, "severity=%s", tc.severity)
		})
	}
}

func TestSendResolved(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK)
	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	}))
	_, _, body := capt.snapshot()
	require.Contains(t, string(body), "✅ Resolved")
	require.Contains(t, string(body), "Good")
}

func TestSendNon2xx(t *testing.T) {
	srv, _ := newCaptureSrv(t, http.StatusInternalServerError)
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

func TestSendMissingWebhookURL(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: map[string]any{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhook_url is required")
}
