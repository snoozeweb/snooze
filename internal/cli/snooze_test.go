package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnoozeList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/snooze/", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"s1","name":"mute-disk","ql":"host = db-1","ttl":3600}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL
	out, _, err := executeCmd(t, rt, "snooze", "list")
	require.NoError(t, err)
	require.Contains(t, out, "mute-disk")
	require.Contains(t, out, "s1")
}

func TestSnoozeGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/snooze/s1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uid":"s1","name":"foo"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL
	out, _, err := executeCmd(t, rt, "snooze", "get", "s1")
	require.NoError(t, err)
	require.Contains(t, out, "name: foo")
	require.Contains(t, out, "uid: s1")
}

func TestSnoozePost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/snooze/", r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "foo", body["name"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"uid":"s-new","name":"foo"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL
	out, _, err := executeCmd(t, rt, "snooze", "post", `{"name":"foo"}`)
	require.NoError(t, err)
	require.Contains(t, out, "s-new")
}

func TestSnoozeDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/api/v1/snooze/abc", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deleted":1}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL
	out, _, err := executeCmd(t, rt, "snooze", "delete", "abc")
	require.NoError(t, err)
	require.Contains(t, out, "deleted")
}
