package googlechat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// sampleRecord returns a representative alert record for tests.
func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
		Hash:     "abc123",
	}
}

// newPluginForTest builds a Plugin whose HTTP client does NOT use TLS so it
// works with plain httptest.NewServer. Race-safe: each test gets its own Plugin.
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "googlechat"})
	require.NoError(t, err)
	gp, ok := p.(*Plugin)
	require.True(t, ok)
	gp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return gp
}

// capture collects the HTTP request fields the test server received.
type capture struct {
	mu          sync.Mutex
	method      string
	rawQuery    string
	body        []byte
	contentType string
}

func (c *capture) set(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.method = r.Method
	c.rawQuery = r.URL.RawQuery
	c.body = b
	c.contentType = r.Header.Get("Content-Type")
}

func (c *capture) snapshot() (method, rawQuery, contentType string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.rawQuery, c.contentType, c.body
}

// newRecordingServer starts an httptest.Server that captures every POST and
// responds 200 OK.
func newRecordingServer(t *testing.T, status int) (*httptest.Server, *capture) {
	t.Helper()
	capt := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.set(r)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, capt
}

// ---- Tests ------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "googlechat"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)

	p, err := factory(plugins.Metadata{Name: "googlechat"})
	require.NoError(t, err)
	require.Equal(t, "googlechat", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// TestSendCard verifies that use_card=true (the default) posts a cardsV2
// JSON body whose card header and widget contain the rendered message text.
func TestSendCard(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusOK)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/v1/spaces/AAA/messages",
			"use_card":    true,
			"message":     "*{{ .Severity }}* on {{ .Host }}: {{ .Message }}",
		},
	})
	require.NoError(t, err)

	method, _, ct, body := capt.snapshot()
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "application/json", ct)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))

	// Must have cardsV2, not text.
	require.Contains(t, payload, "cardsV2", "expected cardsV2 key in card payload")
	require.NotContains(t, payload, "text", "plain text key must be absent in card mode")

	// Verify the rendered message appears somewhere in the card structure.
	raw, _ := json.Marshal(payload)
	require.Contains(t, string(raw), rec.Host)
	require.Contains(t, string(raw), rec.Severity)
}

// TestSendPlainText verifies that use_card=false sends {"text": "..."}.
func TestSendPlainText(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusOK)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/v1/spaces/AAA/messages",
			"use_card":    false,
			"message":     "alert: {{ .Host }}",
		},
	})
	require.NoError(t, err)

	_, _, _, body := capt.snapshot()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))

	require.Contains(t, payload, "text", "expected text key in plain message")
	require.NotContains(t, payload, "cardsV2", "cardsV2 must be absent in plain mode")
	require.Equal(t, "alert: "+rec.Host, payload["text"])
}

// TestSendThreadKey verifies that when thread_key is set, the request URL
// gains the messageReplyOption query parameter and the body contains the
// thread.threadKey field.
func TestSendThreadKey(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusOK)

	p := newPluginForTest(t)
	rec := sampleRecord() // Hash = "abc123"
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/v1/spaces/AAA/messages",
			"use_card":    false,
			"thread_key":  "{{ .Hash }}",
		},
	})
	require.NoError(t, err)

	_, rawQuery, _, body := capt.snapshot()

	// The messageReplyOption query param must be appended.
	q, err := url.ParseQuery(rawQuery)
	require.NoError(t, err)
	require.Equal(t, "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD", q.Get("messageReplyOption"),
		"messageReplyOption must be set when thread_key is configured")

	// The body must carry the thread object.
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	thread, ok := payload["thread"].(map[string]any)
	require.True(t, ok, "thread key should be a JSON object")
	require.Equal(t, rec.Hash, thread["threadKey"])
}

// TestSendNonTwoXXReturnsError verifies that a non-2xx response becomes an error.
func TestSendNonTwoXXReturnsError(t *testing.T) {
	srv, _ := newRecordingServer(t, http.StatusForbidden)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/hook",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
}

// TestSendMissingWebhookURL verifies that an empty webhook_url is rejected
// before any network call.
func TestSendMissingWebhookURL(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhook_url")
}

// TestSendDefaultMessage verifies the default message template is rendered
// when the message field is absent.
func TestSendDefaultMessage(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusOK)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/hook",
			"use_card":    false,
			// no "message" key — should use defaultMessage
		},
	})
	require.NoError(t, err)

	_, _, _, body := capt.snapshot()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))

	text, _ := payload["text"].(string)
	require.Contains(t, text, rec.Severity)
	require.Contains(t, text, rec.Host)
	require.Contains(t, text, rec.Message)
}

// TestSendTimeout verifies the per-call timeout fires and returns an error.
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
			"webhook_url": srv.URL + "/hook",
			"timeout":     "150ms",
		},
	})
	elapsed := time.Since(start)
	require.Error(t, err)
	require.Less(t, elapsed, 2*time.Second, "timeout should fire well before the default")
}

// TestSendCardContainsRenderedText verifies that in card mode the widget
// decoratedText.text contains the rendered message.
func TestSendCardContainsRenderedText(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusOK)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/hook",
			"use_card":    true,
			"message":     "HELLO {{ .Host }}",
		},
	})
	require.NoError(t, err)

	_, _, _, body := capt.snapshot()
	// The rendered message must appear somewhere in the card JSON.
	require.Contains(t, string(body), "HELLO "+rec.Host)

	// Decode the full cardsV2 structure to assert the path.
	var payload struct {
		CardsV2 []struct {
			CardID string `json:"cardId"`
			Card   struct {
				Header struct {
					Title    string `json:"title"`
					Subtitle string `json:"subtitle"`
				} `json:"header"`
				Sections []struct {
					Widgets []struct {
						DecoratedText struct {
							Text string `json:"text"`
						} `json:"decoratedText"`
					} `json:"widgets"`
				} `json:"sections"`
			} `json:"card"`
		} `json:"cardsV2"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Len(t, payload.CardsV2, 1)
	require.Equal(t, "snooze", payload.CardsV2[0].CardID)

	card := payload.CardsV2[0].Card
	require.NotEmpty(t, card.Header.Title)
	require.Len(t, card.Sections, 1)
	require.Len(t, card.Sections[0].Widgets, 1)
	require.Equal(t, "HELLO "+rec.Host, card.Sections[0].Widgets[0].DecoratedText.Text)
}

// TestSendCardThreadKeyPresence verifies that thread_key works with card mode too.
func TestSendCardThreadKeyPresence(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusOK)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/hook",
			"use_card":    true,
			"thread_key":  "fixed-key",
		},
	})
	require.NoError(t, err)

	_, rawQuery, _, body := capt.snapshot()
	q, err := url.ParseQuery(rawQuery)
	require.NoError(t, err)
	require.Equal(t, "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD", q.Get("messageReplyOption"))

	require.True(t, strings.Contains(string(body), "fixed-key"))
}
