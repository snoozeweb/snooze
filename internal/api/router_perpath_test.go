package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// TestRouter_MixedAuthWebhook verifies a single plugin can keep its CRUD
// subtree authenticated while exposing a PUBLIC webhook sub-path via a
// per-path Routes override — the heartbeat shape: an authenticated
// `heartbeat` collection plus a public ping at /api/v1/webhook/heartbeat.
func TestRouter_MixedAuthWebhook(t *testing.T) {
	authOff := false
	authOn := true
	wr := &webhookStub{
		wantSeg: "/heartbeat",
		stubPlugin: stubPlugin{
			name: "heartbeat",
			meta: plugins.Metadata{
				Name:          "heartbeat",
				RouteDefaults: plugins.Route{Authentication: &authOn}, // CRUD authenticated
				Routes: map[string]plugins.Route{
					"/heartbeat": { // matches WebhookPath()
						Authentication:      &authOff, // ping is public
						AuthorizationPolicy: &plugins.AuthorizationPolicy{Write: []string{"any"}},
					},
				},
			},
		},
	}
	rt := &Router{
		Auth:    testTokenEngine(t),
		Plugins: map[string]plugins.Plugin{"heartbeat": wr},
	}
	srv := httptest.NewServer(rt.Build())
	defer srv.Close()

	// Public ping sub-path: no token → reaches the handler.
	resp, err := http.Post(srv.URL+"/api/v1/webhook/heartbeat",
		"application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "ping sub-path must be public")
	require.True(t, wr.called, "HandleWebhook should have been invoked")

	// CRUD subtree: no token → 401 (authentication stays on).
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/heartbeat", nil)
	require.NoError(t, err)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp2.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp2.StatusCode, "CRUD subtree must require auth")
}
