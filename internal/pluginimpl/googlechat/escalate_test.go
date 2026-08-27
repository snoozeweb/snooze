package googlechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// gcQuery / gcBody read the capture's snapshot, keeping the assertions terse.
func gcQuery(c *capture) string {
	_, q, _, _ := c.snapshot()
	return q
}

func gcBody(c *capture) []byte {
	_, _, _, b := c.snapshot()
	return b
}

// gcServer records the last request so the threading behaviour can be asserted.
func gcServer(t *testing.T, capt *capture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.set(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestThreadKeyDefaultsToHash: before this default, every re-escalation started
// a new Chat conversation, so a channel watching a flapping alert filled with
// identical unlinked cards.
func TestThreadKeyDefaultsToHash(t *testing.T) {
	capt := &capture{}
	srv := gcServer(t, capt)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	}))

	require.Contains(t, gcQuery(capt), "messageReplyOption=REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD")
	var body map[string]any
	require.NoError(t, json.Unmarshal(gcBody(capt), &body))
	thread, _ := body["thread"].(map[string]any)
	require.Equal(t, "abc123", thread["threadKey"], "the thread key must default to the alert hash")
}

// An operator who wants one conversation per message must be able to say so.
func TestThreadKeyCanBeCleared(t *testing.T) {
	capt := &capture{}
	srv := gcServer(t, capt)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL, "thread_key": ""},
	}))

	require.NotContains(t, gcQuery(capt), "messageReplyOption")
	var body map[string]any
	require.NoError(t, json.Unmarshal(gcBody(capt), &body))
	require.NotContains(t, body, "thread")
}

// A re-escalation posts a short text reply in the thread rather than repeating
// the card: the root already shows host / severity / message, so a second card
// buries the one new fact.
func TestEscalationPostsShortTextReply(t *testing.T) {
	capt := &capture{}
	srv := gcServer(t, capt)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       map[string]any{"webhook_url": srv.URL},
		Escalation: plugins.Escalation{Count: 4, Reason: "watchlist"},
	}))

	var body map[string]any
	require.NoError(t, json.Unmarshal(gcBody(capt), &body))
	require.NotContains(t, body, "cardsV2", "a re-escalation must not repeat the full card")
	text, _ := body["text"].(string)
	require.Contains(t, text, "New escalation #4")
	require.Contains(t, text, "(watchlist)")
	thread, _ := body["thread"].(map[string]any)
	require.Equal(t, "abc123", thread["threadKey"])
}

// With threading deliberately off there is no thread to reply into, so the card
// still goes out — but it must carry the escalation marker.
func TestEscalationWithoutThreadingKeepsTheCard(t *testing.T) {
	capt := &capture{}
	srv := gcServer(t, capt)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       map[string]any{"webhook_url": srv.URL, "thread_key": ""},
		Escalation: plugins.Escalation{Count: 1},
	}))

	require.Contains(t, string(gcBody(capt)), "cardsV2")
}

// A first delivery is unchanged: a full card, no marker.
func TestFirstDeliveryKeepsTheCard(t *testing.T) {
	capt := &capture{}
	srv := gcServer(t, capt)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: map[string]any{"webhook_url": srv.URL}}))

	raw := string(gcBody(capt))
	require.Contains(t, raw, "cardsV2")
	require.False(t, strings.Contains(raw, "New escalation"))
}
