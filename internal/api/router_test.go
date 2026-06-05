package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// TestRouter_BuildIncludesPublicAndPluginRoutes exercises the assembled
// chain end-to-end: /healthz responds without auth, /api/v1/permissions
// works without auth (it's public for the bootstrap UI), /api/v1/alerts
// is reachable via the injected processor, and an unauthenticated CRUD
// request to a mounted plugin is rejected by the auth middleware.
func TestRouter_BuildIncludesPublicAndPluginRoutes(t *testing.T) {
	stub := &stubPlugin{name: "record"}
	rt := &Router{
		Auth:      testTokenEngine(t),
		Plugins:   map[string]plugins.Plugin{"record": stub},
		Processor: &fakeProcessor{},
	}
	h := rt.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()

	cases := []struct {
		name    string
		method  string
		path    string
		want    int
		wantSub string
	}{
		{"healthz", http.MethodGet, "/healthz", http.StatusOK, ""},
		{"permissions requires auth", http.MethodGet, "/api/v1/permissions", http.StatusUnauthorized, ""},
		// /api/v1/alerts is the public ingest endpoint (mirrors 1.5.0
		// AlertRoute.authentication=False). A bad body without auth
		// should reach the handler and produce a 4xx from there.
		{"alerts is public", http.MethodPost, "/api/v1/alerts", http.StatusBadRequest, ""},
		{"plugin crud needs auth", http.MethodGet, "/api/v1/record", http.StatusUnauthorized, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			require.Equal(t, tc.want, resp.StatusCode)
		})
	}
}

// TestRouter_PluginAuthenticationFalse exercises the new per-plugin
// `authentication: false` knob: plugins that declare it in metadata.yaml
// extend the global Auth skip list so their CRUD subtree is reachable
// without a Bearer token.
func TestRouter_PluginAuthenticationFalse(t *testing.T) {
	authOff := false
	publicPlugin := &stubPlugin{
		name: "publicthing",
		meta: plugins.Metadata{
			Name: "publicthing",
			RouteDefaults: plugins.Route{
				Authentication: &authOff,
				AuthorizationPolicy: &plugins.AuthorizationPolicy{
					Write: []string{"any"},
				},
			},
		},
	}
	privatePlugin := &stubPlugin{
		name: "lockeddown",
		meta: plugins.Metadata{Name: "lockeddown"},
	}
	rt := &Router{
		Auth: testTokenEngine(t),
		Plugins: map[string]plugins.Plugin{
			"publicthing": publicPlugin,
			"lockeddown":  privatePlugin,
		},
	}
	h := rt.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()

	cases := []struct {
		name string
		path string
		want int
	}{
		// Public plugin: no token → 200/404/etc, just NOT 401.
		// (We assert "not 401" rather than the exact code because the
		// generic CRUD list handler against a nil driver may panic into
		// 500; the point of this test is the auth gate.)
		{"public plugin bypasses auth", "/api/v1/publicthing", 0},
		// Locked-down plugin: still requires a token.
		{"private plugin still requires auth", "/api/v1/lockeddown", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+tc.path, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			if tc.want == 0 {
				require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
					"public plugin should not 401")
			} else {
				require.Equal(t, tc.want, resp.StatusCode)
			}
		})
	}
}

// webhookStub is a minimal WebhookReceiver for the router test. It records
// that HandleWebhook fired so the test can distinguish "route mounted, body
// reached the handler" from "route mounted but never reached".
type webhookStub struct {
	stubPlugin
	wantSeg  string
	called   bool
	handleFn func(http.ResponseWriter, *http.Request)
}

func (w *webhookStub) WebhookPath() string { return w.wantSeg }

func (w *webhookStub) HandleWebhook(rw http.ResponseWriter, r *http.Request) {
	w.called = true
	if w.handleFn != nil {
		w.handleFn(rw, r)
		return
	}
	rw.WriteHeader(http.StatusOK)
}

// TestRouter_MountWebhooks verifies plugins.WebhookReceiver gets a route at
// /api/v1/webhook/{plugin} and that the plugin's metadata-driven
// `authentication: false` extends to that mount point so unauthenticated
// callers reach HandleWebhook.
func TestRouter_MountWebhooks(t *testing.T) {
	authOff := false
	wr := &webhookStub{
		wantSeg: "/alertmanager",
		stubPlugin: stubPlugin{
			name: "alertmanager",
			meta: plugins.Metadata{
				Name: "alertmanager",
				RouteDefaults: plugins.Route{
					Authentication: &authOff,
					AuthorizationPolicy: &plugins.AuthorizationPolicy{
						Write: []string{"any"},
					},
				},
			},
		},
	}
	rt := &Router{
		Auth:    testTokenEngine(t),
		Plugins: map[string]plugins.Plugin{"alertmanager": wr},
	}
	h := rt.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 1. The webhook route exists at the canonical 2.0 path.
	resp, err := http.Post(srv.URL+"/api/v1/webhook/alertmanager",
		"application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.True(t, wr.called, "HandleWebhook should have been invoked")

	// 2. The legacy 1.5.0 path is intentionally NOT served (we said no
	// alias). Without auth that's a 401 from the global middleware; with
	// auth it would be a 404. Either way: not 200.
	resp2, err := http.Post(srv.URL+"/api/webhook/alertmanager/v4",
		"application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp2.Body.Close()
	require.NotEqual(t, http.StatusOK, resp2.StatusCode)
}

func TestRouter_StaticFallback(t *testing.T) {
	rt := &Router{Auth: testTokenEngine(t)}
	h := rt.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
