package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootTokenSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/root_token", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"root_token":"top-secret","expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Server = srv.URL
	// Steer the admin client at the http test server by overriding the runtime
	// httpClient and pointing the request URL through its transport.
	rt.httpClient = newHostRewriteClient(srv)

	out, _, err := executeCmd(t, rt, "root-token", "--socket", "/dev/null")
	require.NoError(t, err)
	require.Contains(t, out, "top-secret")
}

func TestRootTokenServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"boom"}}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Server = srv.URL
	rt.httpClient = newHostRewriteClient(srv)

	_, _, err := executeCmd(t, rt, "root-token", "--socket", "/dev/null")
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

func TestRootTokenJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"root_token":"abc"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Server = srv.URL
	rt.httpClient = newHostRewriteClient(srv)
	rt.flags.JSON = true

	out, _, err := executeCmd(t, rt, "--json", "root-token", "--socket", "/dev/null")
	require.NoError(t, err)
	require.Contains(t, out, `"root_token"`)
	require.Contains(t, out, "abc")
}

// newHostRewriteClient returns an *http.Client whose transport rewrites every
// outgoing request to point at srv.URL. The root-token command always builds
// a URL of the shape http://admin/api/root_token — we replace the scheme +
// host so the request lands on the httptest server.
func newHostRewriteClient(srv *httptest.Server) *http.Client {
	base := srv.URL
	tr := srv.Client().Transport
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Mutate the request URL so it points at the test server.
			newURL, _ := req.URL.Parse(base + req.URL.Path)
			req.URL = newURL
			req.Host = newURL.Host
			return tr.RoundTrip(req)
		}),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
