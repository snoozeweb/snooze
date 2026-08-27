package telegram

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

type tgCapture struct {
	mu        sync.Mutex
	bodies    []map[string]any
	messageID int64
}

func newTGCapture(t *testing.T, messageID int64) (*tgCapture, *httptest.Server) {
	t.Helper()
	c := &tgCapture{messageID: messageID}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": c.messageID},
		})
	}))
	t.Cleanup(srv.Close)
	return c, srv
}

func (c *tgCapture) all() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]map[string]any, len(c.bodies))
	copy(out, c.bodies)
	return out
}

func metaWithAction(apiBase string) map[string]any {
	m := sampleMeta(apiBase)
	m["action_name"] = "Telegram alerts"
	return m
}

// TestEscalationRepliesUnderTheOriginalMessage: one alert becomes one Telegram
// conversation instead of a wall of unlinked messages.
func TestEscalationRepliesUnderTheOriginalMessage(t *testing.T) {
	capt, srv := newTGCapture(t, 4242)
	p := newPluginForTest(t)

	stamped := map[string]any{}
	inject := func(field string, value any) { stamped[field] = value }

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: metaWithAction(srv.URL), Inject: inject}))

	require.EqualValues(t, 4242,
		notifyRefInt64(snoozetypes.Record{Extra: stamped}, "Telegram alerts", refMessageID))

	rec := sampleRecord()
	rec.Extra = stamped
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta:       metaWithAction(srv.URL),
		Inject:     inject,
		Escalation: plugins.Escalation{Count: 2, Reason: "manual"},
	}))

	bodies := capt.all()
	require.Len(t, bodies, 2)
	require.NotContains(t, bodies[0], "reply_to_message_id")
	require.EqualValues(t, 4242, bodies[1]["reply_to_message_id"])
	// A deleted root must not make Telegram reject the whole send.
	require.Equal(t, true, bodies[1]["allow_sending_without_reply"])
	text, _ := bodies[1]["text"].(string)
	require.Contains(t, text, "New escalation #2")
	require.Contains(t, text, "(manual)")
}

// Only the first delivery's id is stored: replying to a reply would nest the
// conversation deeper on every escalation.
func TestThreadRootIsNotOverwritten(t *testing.T) {
	_, srv := newTGCapture(t, 9999)
	p := newPluginForTest(t)

	stamped := map[string]any{"notify_ref_Telegram alerts": map[string]any{refMessageID: int64(4242)}}
	rec := sampleRecord()
	rec.Extra = stamped

	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta:       metaWithAction(srv.URL),
		Inject:     func(field string, value any) { stamped[field] = value },
		Escalation: plugins.Escalation{Count: 1},
	}))

	require.EqualValues(t, 4242,
		notifyRefInt64(snoozetypes.Record{Extra: stamped}, "Telegram alerts", refMessageID))
}

// With no stored root (the first delivery predates this feature, or failed) the
// escalation must still be delivered — as a plain message.
func TestEscalationWithoutStoredRootStillSends(t *testing.T) {
	capt, srv := newTGCapture(t, 4242)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       metaWithAction(srv.URL),
		Escalation: plugins.Escalation{Count: 1},
	}))

	body := capt.all()[0]
	require.NotContains(t, body, "reply_to_message_id")
	text, _ := body["text"].(string)
	require.Contains(t, text, "New escalation #1",
		"a re-escalation must read as one even when there is no thread to hang it under")
}

// notifyRefInt64 must survive a JSON round-trip, which is how the handle comes
// back off the record in production.
func TestNotifyRefInt64NumericShapes(t *testing.T) {
	for name, v := range map[string]any{
		"int64":   int64(7),
		"int":     7,
		"float64": float64(7),
	} {
		t.Run(name, func(t *testing.T) {
			rec := snoozetypes.Record{Extra: map[string]any{
				"notify_ref_a": map[string]any{refMessageID: v},
			}}
			require.EqualValues(t, 7, notifyRefInt64(rec, "a", refMessageID))
		})
	}
	require.Zero(t, notifyRefInt64(snoozetypes.Record{}, "a", refMessageID))
}
