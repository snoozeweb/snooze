package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/plugins"
)

// TestRouter_IngestTokenGatesWebhooks verifies that when config.ingest.token
// is set, the shared-secret middleware gates every webhook receiver — even a
// public (authentication:false) one — before the handler runs.
func TestRouter_IngestTokenGatesWebhooks(t *testing.T) {
	authOff := false
	wr := &webhookStub{
		wantSeg: "/alertmanager",
		stubPlugin: stubPlugin{
			name: "alertmanager",
			meta: plugins.Metadata{
				Name: "alertmanager",
				RouteDefaults: plugins.Route{
					Authentication:      &authOff,
					AuthorizationPolicy: &plugins.AuthorizationPolicy{Write: []string{"any"}},
				},
			},
		},
	}
	rt := &Router{
		Auth:    testTokenEngine(t),
		Plugins: map[string]plugins.Plugin{"alertmanager": wr},
		Config:  &config.Config{Ingest: schema.Ingest{Token: "ingest-secret"}},
	}
	srv := httptest.NewServer(rt.Build())
	defer srv.Close()

	// Missing ingest token → rejected before the handler.
	resp, err := http.Post(srv.URL+"/api/v1/webhook/alertmanager",
		"application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "missing ingest token must be rejected")
	require.False(t, wr.called, "handler must not run without the ingest token")

	// Correct token (query param) → reaches the handler.
	resp2, err := http.Post(srv.URL+"/api/v1/webhook/alertmanager?token=ingest-secret",
		"application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	require.True(t, wr.called)
}
