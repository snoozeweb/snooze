package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthBothOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "health")
	require.NoError(t, err)
	require.Contains(t, out, "/healthz")
	require.Contains(t, out, "/readyz")
	require.Contains(t, out, "200")
}

func TestHealthReadyFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"degraded"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "health")
	require.Error(t, err)
	require.Contains(t, err.Error(), "/readyz")
}

func TestHealthJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "--json", "health")
	require.NoError(t, err)
	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 2)
	require.EqualValues(t, 200, got[0]["status"])
}
