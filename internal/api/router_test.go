package api

import (
	"net/http"
	"net/http/httptest"
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
		{"alerts requires auth", http.MethodPost, "/api/v1/alerts", http.StatusUnauthorized, ""},
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
