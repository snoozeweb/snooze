package googlechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

// TestRecordsThreadNameForInboundLookup: the daemon resolves an inbound Chat
// command back to its alert by thread name. Before the notifier recorded it,
// that lookup had nothing to match against and every command answered "cannot
// find the corresponding alert!".
func TestRecordsThreadNameForInboundLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"spaces/S/messages/M","thread":{"name":"spaces/S/threads/T1"}}`))
	}))
	t.Cleanup(srv.Close)

	stamped := map[string]any{}
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:   map[string]any{"webhook_url": srv.URL, "action_name": "Chat ops"},
		Inject: func(field string, value any) { stamped[field] = value },
	}))

	// The flat list the daemon searches...
	require.Equal(t, []string{"spaces/S/threads/T1"}, stamped[chatThreadsField])
	// ...and the per-action handle every other notifier uses.
	require.Equal(t, "spaces/S/threads/T1",
		plugins.NotifyRefString(snoozetypes.Record{Extra: stamped}, "Chat ops", "thread_name"))
}

// The list accumulates across an alert's occurrences without duplicating, since
// aggregaterule carries it forward.
func TestThreadListAccumulatesWithoutDuplicates(t *testing.T) {
	thread := "spaces/S/threads/T1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"thread":{"name":"` + thread + `"}}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	stamped := map[string]any{}
	inject := func(field string, value any) { stamped[field] = value }

	// A record already carrying a different thread, in the []any shape a driver
	// round-trip produces.
	rec := sampleRecord()
	rec.Extra = map[string]any{chatThreadsField: []any{"spaces/S/threads/T0"}}

	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta:   map[string]any{"webhook_url": srv.URL, "action_name": "Chat ops"},
		Inject: inject,
	}))
	require.Equal(t, []string{"spaces/S/threads/T0", thread}, stamped[chatThreadsField])

	// Re-posting into a thread already recorded must not append it twice.
	clear(stamped)
	rec.Extra = map[string]any{chatThreadsField: []any{thread}}
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta:   map[string]any{"webhook_url": srv.URL, "action_name": "Chat ops"},
		Inject: inject,
	}))
	require.NotContains(t, stamped, chatThreadsField)
}

// A long-lived alert must not grow the record without bound.
func TestThreadListIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"thread":{"name":"spaces/S/threads/NEW"}}`))
	}))
	t.Cleanup(srv.Close)

	existing := make([]any, 0, maxTrackedThreads+5)
	for i := range maxTrackedThreads + 5 {
		existing = append(existing, "old-"+strconv.Itoa(i))
	}
	rec := sampleRecord()
	rec.Extra = map[string]any{chatThreadsField: existing}

	stamped := map[string]any{}
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta:   map[string]any{"webhook_url": srv.URL, "action_name": "Chat ops"},
		Inject: func(field string, value any) { stamped[field] = value },
	}))

	got, _ := stamped[chatThreadsField].([]string)
	require.Len(t, got, maxTrackedThreads)
	require.Equal(t, "spaces/S/threads/NEW", got[len(got)-1],
		"the newest thread — the one an operator is plausibly replying in — must survive the trim")
}

// A response with no thread name must not fail the notification, which already
// landed; the only cost is an unresolvable inbound command.
func TestNoThreadNameInResponseIsTolerated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	stamped := map[string]any{}
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:   map[string]any{"webhook_url": srv.URL, "action_name": "Chat ops"},
		Inject: func(field string, value any) { stamped[field] = value },
	}))
	require.Empty(t, stamped)
}
