package mattermost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// TestEscalationBanner: an incoming webhook returns no message identity, so
// this mode cannot thread. A re-escalation must still read as one rather than
// arriving as an indistinguishable repeat of the first delivery.
func TestEscalationBanner(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		body = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       map[string]any{"webhook_url": srv.URL},
		Escalation: plugins.Escalation{Count: 3, Reason: "timeout"},
	}))
	require.Contains(t, body, "New escalation #3")
	require.Contains(t, body, "timeout")
}

// A first delivery must be indistinguishable from the pre-escalation behaviour.
func TestFirstDeliveryHasNoBanner(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		body = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: map[string]any{"webhook_url": srv.URL}}))
	require.NotContains(t, body, "New escalation")
}
