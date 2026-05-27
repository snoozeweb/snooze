package telegram

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

// sampleRecord returns a representative alert record for use in tests.
func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
	}
}

// sampleMeta returns the minimal payload.Meta that satisfies configFromMeta.
func sampleMeta(apiBase string) map[string]any {
	return map[string]any{
		"bot_token": "123456789:ABCDefGhIJKlmNoPQRstuVwxYZ",
		"chat_id":   "-100987654321",
		"api_base":  apiBase,
	}
}

// newPluginForTest builds a Plugin wired with a simple http.Client builder
// that honours the timeout but uses no TLS customisation (safe for httptest).
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "telegram"})
	require.NoError(t, err)
	tp := p.(*Plugin)
	tp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return tp
}

// --- registration & contract -----------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "telegram"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "telegram"})
	require.NoError(t, err)
	require.Equal(t, "telegram", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// --- successful send -------------------------------------------------------

// TestSendHitsCorrectPathAndBody verifies that Send posts to
// /bot<token>/sendMessage and that the JSON body carries the expected fields.
func TestSendHitsCorrectPathAndBody(t *testing.T) {
	var (
		mu      sync.Mutex
		gotPath string
		gotBody []byte
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: sampleMeta(srv.URL),
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, gotPath, "/bot123456789:ABCDefGhIJKlmNoPQRstuVwxYZ/sendMessage")

	var req sendMessageRequest
	require.NoError(t, json.Unmarshal(gotBody, &req))
	require.Equal(t, "-100987654321", req.ChatID)
	require.NotEmpty(t, req.Text)
	require.Equal(t, "HTML", req.ParseMode)
}

// TestSendParseModeMarkdownV2 verifies that the MarkdownV2 selector propagates.
func TestSendParseModeMarkdownV2(t *testing.T) {
	var (
		mu      sync.Mutex
		gotMode string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		var req sendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotMode = req.ParseMode
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := sampleMeta(srv.URL)
	meta["parse_mode"] = "MarkdownV2"
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "MarkdownV2", gotMode)
}

// TestSendParseModeNone verifies that parse_mode "none" results in an empty
// parse_mode field in the JSON body (the Telegram API omits the field).
func TestSendParseModeNone(t *testing.T) {
	var (
		mu      sync.Mutex
		gotMode string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		var req sendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotMode = req.ParseMode
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := sampleMeta(srv.URL)
	meta["parse_mode"] = "none"
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, gotMode, "parse_mode should be omitted when set to 'none'")
}

// TestSendDisableNotification verifies the boolean flag is forwarded.
func TestSendDisableNotification(t *testing.T) {
	var (
		mu         sync.Mutex
		gotDisable bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		var req sendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotDisable = req.DisableNotification
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := sampleMeta(srv.URL)
	meta["disable_notification"] = true
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.True(t, gotDisable)
}

// TestSendCustomMessage verifies a custom message template is rendered.
func TestSendCustomMessage(t *testing.T) {
	var (
		mu      sync.Mutex
		gotText string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		var req sendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotText = req.Text
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := sampleMeta(srv.URL)
	meta["message"] = "ALERT: {{ .Severity }} on {{ .Host }}"
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "ALERT: warning on db-1.example.com", gotText)
}

// --- error paths -----------------------------------------------------------

// TestSendAPIReturnsFalse verifies that {"ok":false,...} propagates as an error.
func TestSendAPIReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: sampleMeta(srv.URL),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "chat not found")
}

// TestSendHTTPErrorStatus verifies that a non-200 HTTP response yields an error.
func TestSendHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: sampleMeta(srv.URL),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
}

// TestSendMissingBotToken verifies that an empty bot_token returns an error
// before any network activity.
func TestSendMissingBotToken(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"chat_id": "-100987654321",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bot_token")
}

// TestSendMissingChatID verifies that an empty chat_id returns an error before
// any network activity.
func TestSendMissingChatID(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"bot_token": "123456789:ABCDefGhIJKlmNoPQRstuVwxYZ",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "chat_id")
}

// TestSendNilMeta verifies that a nil Meta map returns a config error.
func TestSendNilMeta(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: nil})
	require.Error(t, err)
}
