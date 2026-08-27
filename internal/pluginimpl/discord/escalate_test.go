package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

func discordCapture(t *testing.T) (*string, *httptest.Server) {
	t.Helper()
	body := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		*body = string(raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return body, srv
}

// TestEscalationBanner: Discord webhooks cannot create a thread on their own
// message (that needs a bot token), so a re-escalation is marked in the message
// instead of being threaded.
func TestEscalationBanner(t *testing.T) {
	body, srv := discordCapture(t)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       map[string]any{"webhook_url": srv.URL},
		Escalation: plugins.Escalation{Count: 2, Reason: "manual"},
	}))
	require.Contains(t, *body, "New escalation #2")
	require.Contains(t, *body, "manual")
}

func TestFirstDeliveryHasNoBanner(t *testing.T) {
	body, srv := discordCapture(t)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: map[string]any{"webhook_url": srv.URL}}))
	require.NotContains(t, *body, "New escalation")
}

// The banner must land in the embed description when embeds are on, not be
// silently dropped by the embed builder.
func TestEscalationBannerInEmbedMode(t *testing.T) {
	body, srv := discordCapture(t)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       map[string]any{"webhook_url": srv.URL, "use_embed": true},
		Escalation: plugins.Escalation{Count: 1},
	}))

	var msg map[string]any
	require.NoError(t, json.Unmarshal([]byte(*body), &msg))
	embeds, _ := msg["embeds"].([]any)
	require.NotEmpty(t, embeds)
	first, _ := embeds[0].(map[string]any)
	desc, _ := first["description"].(string)
	require.Contains(t, desc, "New escalation #1")
}
