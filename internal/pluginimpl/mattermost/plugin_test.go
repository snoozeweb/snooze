package mattermost

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
	p, err := factory(plugins.Metadata{Name: "mattermost"})
	require.NoError(t, err)
	mp, ok := p.(*Plugin)
	require.True(t, ok)
	mp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return mp
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
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv, capt
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "mattermost"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "mattermost"})
	require.NoError(t, err)
	require.Equal(t, "mattermost", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

func TestSendWebhook(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK)
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	}))

	method, ct, body := capt.snapshot()
	require.Equal(t, http.MethodPost, method)
	require.Contains(t, ct, "application/json")

	var got mmPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got.Attachments, 1)
	require.Equal(t, "#d00000", got.Attachments[0].Color)
	require.Contains(t, got.Attachments[0].Text, "disk full")
}

func TestSendTemplate(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK)
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"message":     "host={{ .Host }} msg={{ .Message }}",
		},
	}))
	_, _, body := capt.snapshot()
	require.Contains(t, string(body), "host=db-1")
	require.Contains(t, string(body), "msg=disk full")
}

func TestSendSeverityColors(t *testing.T) {
	cases := []struct{ severity, want string }{
		{"info", "#36a64f"}, {"notice", "#36a64f"}, {"debug", "#36a64f"},
		{"warning", "#daa038"},
		{"error", "#d00000"}, {"critical", "#d00000"}, {"emergency", "#d00000"},
		{"weird-level", "#d00000"},
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
			var got mmPayload
			require.NoError(t, json.Unmarshal(body, &got))
			require.Len(t, got.Attachments, 1)
			require.Equal(t, tc.want, got.Attachments[0].Color, "severity=%s", tc.severity)
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
	var got mmPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Contains(t, got.Attachments[0].Text, "✅ Resolved")
	require.Equal(t, "#36a64f", got.Attachments[0].Color)
}

func TestSendChannelUsernameIcon(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK)
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"channel":     "alerts",
			"username":    "snooze-bot",
			"icon_url":    "https://example.com/i.png",
		},
	}))
	_, _, body := capt.snapshot()
	var got mmPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "alerts", got.Channel)
	require.Equal(t, "snooze-bot", got.Username)
	require.Equal(t, "https://example.com/i.png", got.IconURL)
}

func TestSendNon2xx(t *testing.T) {
	srv, _ := newCaptureSrv(t, http.StatusTooManyRequests)
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "429")
}

func TestSendMissingWebhookURL(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: map[string]any{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhook_url is required")
}
