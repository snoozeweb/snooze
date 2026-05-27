package slack

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
// Test helpers
// ---------------------------------------------------------------------------

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
	}
}

// newPluginForTest creates a *Plugin with the http.Client builder replaced so
// httptest servers are used without proxy indirection.
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "slack"})
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

// capturedRequest holds the last request the test server saw.
type capturedRequest struct {
	mu     sync.Mutex
	method string
	ct     string
	auth   string
	body   []byte
}

func (c *capturedRequest) record(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.method = r.Method
	c.ct = r.Header.Get("Content-Type")
	c.auth = r.Header.Get("Authorization")
	c.body = b
}

func (c *capturedRequest) snapshot() (method, ct, auth string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.ct, c.auth, c.body
}

// newCaptureSrv returns an httptest.Server that captures the request and
// replies with statusCode / responseBody.
func newCaptureSrv(t *testing.T, statusCode int, responseBody string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	capt := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.record(r)
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(srv.Close)
	return srv, capt
}

// ---------------------------------------------------------------------------
// Registration & interface contract
// ---------------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "slack"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Notifier = (*Plugin)(nil)

	p, err := factory(plugins.Metadata{Name: "slack"})
	require.NoError(t, err)
	require.Equal(t, "slack", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// ---------------------------------------------------------------------------
// Webhook mode
// ---------------------------------------------------------------------------

// TestSendWebhookMode verifies the Incoming Webhook path: POST to webhook_url,
// JSON body with text/blocks/attachments, colour matches severity.
func TestSendWebhookMode(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK, "ok")

	p := newPluginForTest(t)
	rec := sampleRecord() // severity=warning → "warning" colour
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/services/T/B/X",
		},
	})
	require.NoError(t, err)

	method, ct, auth, body := capt.snapshot()
	require.Equal(t, http.MethodPost, method)
	require.Contains(t, ct, "application/json")
	require.Empty(t, auth, "webhook mode must not set Authorization header")

	var got webhookPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.NotEmpty(t, got.Text, "text field must be populated")
	require.NotEmpty(t, got.Blocks, "blocks must be populated")
	require.Len(t, got.Attachments, 1)
	require.Equal(t, "warning", got.Attachments[0].Color)
}

// TestSendWebhookMessageTemplate verifies that message is rendered as a Go
// text/template over the record's fields.
func TestSendWebhookMessageTemplate(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK, "ok")

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"message":     "host={{ .Host }} msg={{ .Message }}",
		},
	})
	require.NoError(t, err)

	_, _, _, body := capt.snapshot()
	var got webhookPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Contains(t, got.Text, "host=db-1")
	require.Contains(t, got.Text, "msg=disk full")
}

// TestSendSeverityColors verifies the three-tier colour mapping.
func TestSendSeverityColors(t *testing.T) {
	cases := []struct {
		severity string
		want     string
	}{
		{"info", "good"},
		{"notice", "good"},
		{"debug", "good"},
		{"warning", "warning"},
		{"error", "danger"},
		{"critical", "danger"},
		{"emergency", "danger"},
		{"unknown-level", "danger"},
	}

	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			srv, capt := newCaptureSrv(t, http.StatusOK, "ok")
			p := newPluginForTest(t)
			rec := sampleRecord()
			rec.Severity = tc.severity

			require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
				Meta: map[string]any{"webhook_url": srv.URL},
			}))

			_, _, _, body := capt.snapshot()
			var got webhookPayload
			require.NoError(t, json.Unmarshal(body, &got))
			require.Len(t, got.Attachments, 1)
			require.Equal(t, tc.want, got.Attachments[0].Color, "severity=%s", tc.severity)
		})
	}
}

// TestSendResolvePath verifies that rec.State=="close" produces a green
// attachment with a "✅ Resolved" prefix.
func TestSendResolvePath(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK, "ok")

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"
	rec.Severity = "critical" // without resolved, this would be "danger"

	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	}))

	_, _, _, body := capt.snapshot()
	var got webhookPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Contains(t, got.Text, "✅ Resolved")
	require.Len(t, got.Attachments, 1)
	require.Equal(t, "good", got.Attachments[0].Color, "resolved alerts must use green")
}

// TestSendUsernameIconEmoji verifies that optional cosmetic fields are
// forwarded in webhook mode.
func TestSendUsernameIconEmoji(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK, "ok")

	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"username":    "snooze-bot",
			"icon_emoji":  ":robot_face:",
		},
	}))

	_, _, _, body := capt.snapshot()
	var got webhookPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "snooze-bot", got.Username)
	require.Equal(t, ":robot_face:", got.IconEmoji)
}

// ---------------------------------------------------------------------------
// Bot-token mode
// ---------------------------------------------------------------------------

// botPlugin returns a *Plugin wired to use srv as the Slack API endpoint.
func botPlugin(t *testing.T, srv *httptest.Server) *Plugin {
	t.Helper()
	return &Plugin{
		meta: plugins.Metadata{Name: "slack"},
		newClient: func(d time.Duration) *http.Client {
			if d <= 0 {
				d = defaultTimeout
			}
			return &http.Client{Timeout: d}
		},
		apiURL: srv.URL, // redirect chatPostMessageURL to the test server
	}
}

// TestSendBotTokenMode verifies that bot-token mode sets the Authorization
// header and includes the channel in the JSON body.
func TestSendBotTokenMode(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK, `{"ok":true}`)
	p := botPlugin(t, srv)

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"bot_token": "xoxb-test-token",
			"channel":   "#alerts",
		},
	})
	require.NoError(t, err)

	_, _, auth, body := capt.snapshot()
	require.Equal(t, "Bearer xoxb-test-token", auth)

	var got botPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "#alerts", got.Channel)
	require.Len(t, got.Attachments, 1)
	require.NotEmpty(t, got.Text)
}

// TestSendBotTokenLogicalError verifies that {"ok":false,"error":"..."} is
// returned as an error even when HTTP status is 200.
func TestSendBotTokenLogicalError(t *testing.T) {
	srv, _ := newCaptureSrv(t, http.StatusOK, `{"ok":false,"error":"channel_not_found"}`)
	p := botPlugin(t, srv)

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"bot_token": "xoxb-bad",
			"channel":   "#missing",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "channel_not_found")
}

// TestSendBotTokenResolve verifies that the resolve path also works in
// bot-token mode (green colour, "✅ Resolved" prefix).
func TestSendBotTokenResolve(t *testing.T) {
	srv, capt := newCaptureSrv(t, http.StatusOK, `{"ok":true}`)
	p := botPlugin(t, srv)

	rec := sampleRecord()
	rec.State = "close"

	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"bot_token": "xoxb-test-token",
			"channel":   "#alerts",
		},
	}))

	_, _, _, body := capt.snapshot()
	var got botPayload
	require.NoError(t, json.Unmarshal(body, &got))
	require.Contains(t, got.Text, "✅ Resolved")
	require.Equal(t, "good", got.Attachments[0].Color)
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

// TestSendNon200Error verifies that a non-2xx Slack response surfaces the
// HTTP status code in the error.
func TestSendNon200Error(t *testing.T) {
	srv, _ := newCaptureSrv(t, http.StatusTooManyRequests, "")
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "429")
}

// TestSendMissingConfig verifies that omitting both webhook_url and bot_token
// yields a descriptive error.
func TestSendMissingConfig(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

// TestSendNilMeta verifies that a nil Meta map also yields a clear error.
func TestSendNilMeta(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

// TestSendTimeout verifies that the configured timeout fires before the server
// responds.
func TestSendTimeout(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-released:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })

	p := newPluginForTest(t)
	start := time.Now()
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"timeout":     "150ms",
		},
	})
	elapsed := time.Since(start)
	require.Error(t, err)
	require.Less(t, elapsed, 2*time.Second, "timeout should fire well before the default")
}
