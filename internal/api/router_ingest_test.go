package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

// TestRouter_IngestTenant_WebhookSetsDefaultTenant verifies that an inbound
// webhook request with no token lands with DefaultTenant in context when a
// TenantResolver is wired but has no matching entry.
func TestRouter_IngestTenant_WebhookSetsDefaultTenant(t *testing.T) {
	authOff := false
	var capturedTenant string
	wr := &webhookStub{
		wantSeg: "/ping",
		stubPlugin: stubPlugin{
			name: "ping",
			meta: plugins.Metadata{
				Name: "ping",
				RouteDefaults: plugins.Route{
					Authentication:      &authOff,
					AuthorizationPolicy: &plugins.AuthorizationPolicy{Write: []string{"any"}},
				},
			},
		},
	}
	// Override HandleWebhook to capture the tenant from context.
	wr.handleFn = func(w http.ResponseWriter, r *http.Request) {
		capturedTenant, _ = auth.TenantFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}

	resolver := middleware.NewTenantResolver()
	resolver.Replace(map[string]string{"tok-acme": "acme"}) // no match for empty token

	rt := &Router{
		Auth:           testTokenEngine(t),
		Plugins:        map[string]plugins.Plugin{"ping": wr},
		Config:         &config.Config{},
		TenantResolver: resolver,
	}
	srv := httptest.NewServer(rt.Build())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/webhook/ping",
		"application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, snoozetypes.DefaultTenant, capturedTenant)
}

// TestRouter_IngestKillSwitch_AlertsAnd_Webhooks is the end-to-end contract for
// the runtime ingest kill-switch: with IngestAllowed→false BOTH /api/v1/alerts
// and /api/v1/webhook/<receiver> return 503; flip it to →true and both return
// 200. Driven through a full httptest.NewServer so the middleware chain (incl.
// the webhook subrouter) is exercised exactly as in production.
func TestRouter_IngestKillSwitch_AlertsAnd_Webhooks(t *testing.T) {
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

	allow := false // start disabled
	rt := &Router{
		Auth:          testTokenEngine(t),
		Processor:     &fakeProcessor{},
		Plugins:       map[string]plugins.Plugin{"alertmanager": wr},
		Config:        &config.Config{},
		IngestAllowed: func(context.Context) bool { return allow },
	}
	srv := httptest.NewServer(rt.Build())
	defer srv.Close()

	post := func(path string) int {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(`{"host":"h1"}`))
		require.NoError(t, err)
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	// Disabled → both endpoints 503.
	require.Equal(t, http.StatusServiceUnavailable, post("/api/v1/alerts"),
		"alerts must be 503 while ingest is disabled")
	require.Equal(t, http.StatusServiceUnavailable, post("/api/v1/webhook/alertmanager"),
		"webhook must be 503 while ingest is disabled")
	require.False(t, wr.called, "webhook handler must not run while disabled")

	// Re-enable → both endpoints 200.
	allow = true
	require.Equal(t, http.StatusOK, post("/api/v1/alerts"),
		"alerts must be 200 once ingest is re-enabled")
	require.Equal(t, http.StatusOK, post("/api/v1/webhook/alertmanager"),
		"webhook must be 200 once ingest is re-enabled")
	require.True(t, wr.called, "webhook handler must run once re-enabled")
}

// TestRouter_IngestTenant_AlertsRouteSetsTenant verifies that /api/v1/alerts
// also receives the tenant resolved by IngestTenant.
func TestRouter_IngestTenant_AlertsRouteSetsTenant(t *testing.T) {
	resolver := middleware.NewTenantResolver()
	resolver.Replace(map[string]string{"tok-acme": "acme"})

	var capturedCtx context.Context
	fp := &fakeProcessor{}
	fp.captureCtx = func(ctx context.Context) { capturedCtx = ctx }

	rt := &Router{
		Auth:           testTokenEngine(t),
		Processor:      fp,
		Config:         &config.Config{},
		TenantResolver: resolver,
	}
	srv := httptest.NewServer(rt.Build())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/alerts",
		strings.NewReader(`{"host":"h1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tok-acme")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	tenant, ok := auth.TenantFrom(capturedCtx)
	require.True(t, ok)
	require.Equal(t, "acme", tenant)
}
