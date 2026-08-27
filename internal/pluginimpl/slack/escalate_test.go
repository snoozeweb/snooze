package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// slackCapture records every posted body and serves a chat.postMessage
// response carrying a ts, which is the handle a later escalation threads under.
type slackCapture struct {
	mu     sync.Mutex
	bodies []map[string]any
	ts     string
}

func newSlackCapture(t *testing.T, ts string) (*slackCapture, *httptest.Server) {
	t.Helper()
	c := &slackCapture{ts: ts}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": c.ts})
	}))
	t.Cleanup(srv.Close)
	return c, srv
}

func (c *slackCapture) all() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]map[string]any, len(c.bodies))
	copy(out, c.bodies)
	return out
}

func botMeta() map[string]any {
	return map[string]any{
		"bot_token":   "xoxb-test",
		"channel":     "#alerts",
		"action_name": "Slack alerts",
	}
}

// TestBotModeThreadsEscalationUnderTheRoot: the first delivery's ts is stored
// and the escalation replies under it, broadcast to the channel so the people
// not already following the thread still see it.
func TestBotModeThreadsEscalationUnderTheRoot(t *testing.T) {
	capt, srv := newSlackCapture(t, "1700000000.000100")
	p := newPluginForTest(t)
	p.apiURL = srv.URL

	stamped := map[string]any{}
	payload := plugins.NotificationPayload{
		Meta:   botMeta(),
		Inject: func(field string, value any) { stamped[field] = value },
	}
	require.NoError(t, p.Send(context.Background(), sampleRecord(), payload))

	// The root ts must have been remembered.
	require.Equal(t, "1700000000.000100",
		plugins.NotifyRefString(snoozetypes.Record{Extra: stamped}, "Slack alerts", refThreadTS))

	// Now escalate.
	rec := sampleRecord()
	rec.Extra = stamped
	escPayload := plugins.NotificationPayload{
		Meta:       botMeta(),
		Inject:     func(field string, value any) { stamped[field] = value },
		Escalation: plugins.Escalation{Count: 1, Reason: "timeout"},
	}
	require.NoError(t, p.Send(context.Background(), rec, escPayload))

	bodies := capt.all()
	require.Len(t, bodies, 2)
	require.NotContains(t, bodies[0], "thread_ts", "a first delivery is a root message")
	require.Equal(t, "1700000000.000100", bodies[1]["thread_ts"])
	require.Equal(t, true, bodies[1]["reply_broadcast"],
		"a threaded escalation must still reach the channel, not just thread followers")
	text, _ := bodies[1]["text"].(string)
	require.Contains(t, text, "New escalation #1")
	require.Contains(t, text, "(timeout)")
}

// A reply's own ts must not overwrite the stored root, or the thread would nest
// one level deeper on every escalation.
func TestThreadRootIsNotOverwrittenByReplies(t *testing.T) {
	_, srv := newSlackCapture(t, "1700000000.000999")
	p := newPluginForTest(t)
	p.apiURL = srv.URL

	stamped := map[string]any{"notify_ref_Slack alerts": map[string]any{refThreadTS: "1700000000.000100"}}
	rec := sampleRecord()
	rec.Extra = stamped

	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta:       botMeta(),
		Inject:     func(field string, value any) { stamped[field] = value },
		Escalation: plugins.Escalation{Count: 2},
	}))

	require.Equal(t, "1700000000.000100",
		plugins.NotifyRefString(snoozetypes.Record{Extra: stamped}, "Slack alerts", refThreadTS))
}

// Webhook mode cannot thread — an incoming webhook returns no message identity.
// It must still make the escalation visible rather than silently doing nothing.
func TestWebhookModeFallsBackToAnEscalationBanner(t *testing.T) {
	capt, srv := newSlackCapture(t, "ignored")
	p := newPluginForTest(t)

	stamped := map[string]any{}
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"action_name": "Slack alerts",
		},
		Inject:     func(field string, value any) { stamped[field] = value },
		Escalation: plugins.Escalation{Count: 3},
	}))

	body := capt.all()[0]
	require.NotContains(t, body, "thread_ts")
	text, _ := body["text"].(string)
	require.Contains(t, text, "New escalation #3")
	require.Empty(t, stamped, "webhook mode has no ts to store")
}

// A first delivery must be byte-identical to the pre-escalation behaviour.
func TestFirstDeliveryCarriesNoEscalationMarker(t *testing.T) {
	capt, srv := newSlackCapture(t, "1700000000.000100")
	p := newPluginForTest(t)
	p.apiURL = srv.URL

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: botMeta()}))

	text, _ := capt.all()[0]["text"].(string)
	require.NotContains(t, text, "escalation")
}
