package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginSuccess(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/login/local", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"new-token","method":"local"}`))
	}))
	defer srv.Close()

	rt, stdout, _ := newTestRuntime(t, srv)
	rt.passwordReader = func() (string, error) { return "ignored", nil }
	rt.flags.User = "alice"
	rt.flags.Password = "hunter2"
	rt.flags.Method = "local"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "login")
	require.NoError(t, err)
	require.Contains(t, out, "Logged in")
	require.Equal(t, "alice", gotBody["username"])
	require.Equal(t, "hunter2", gotBody["password"])
	require.NotEmpty(t, stdout.String())

	// Token cache file was written and contains the new token.
	raw, err := os.ReadFile(rt.flags.Cache)
	require.NoError(t, err)
	require.Equal(t, "new-token", strings.TrimSpace(string(raw)))
}

func TestLoginPromptsForPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "prompted-pass", body["password"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"t"}`))
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.User = "alice"
	rt.flags.Method = "local"
	rt.flags.Server = srv.URL
	rt.passwordReader = func() (string, error) { return "prompted-pass", nil }

	_, _, err := executeCmd(t, rt, "login")
	require.NoError(t, err)
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"bad creds"}}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.User = "alice"
	rt.flags.Password = "wrong"
	rt.flags.Method = "local"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "login")
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad creds")
}

func TestLoginAnonymousSkipsUsername(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/login/anonymous", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"anon"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Method = "anonymous"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "login")
	require.NoError(t, err)
}
