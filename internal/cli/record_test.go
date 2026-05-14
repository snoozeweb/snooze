package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/alerts", r.URL.Path)
		var rec map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rec))
		require.Equal(t, "db-1", rec["host"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"db-1","severity":"warn","message":"oops"}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "record", "post", `{"host":"db-1","severity":"warn","message":"oops"}`)
	require.NoError(t, err)
	require.Contains(t, out, "uid=u-1")
	require.Contains(t, out, "host=db-1")
}

func TestRecordPostJSONFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"db-1"}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL
	rt.flags.JSON = true

	out, _, err := executeCmd(t, rt, "--json", "record", "post", `{"host":"db-1"}`)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Equal(t, "u-1", got["uid"])
}

func TestRecordPostInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for invalid JSON args")
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	_, _, err := executeCmd(t, rt, "record", "post", `not-json`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode record JSON")
}

func TestRecordList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/record/", r.URL.Path)
		require.Equal(t, "25", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"db-1","severity":"warn","message":"oops"}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "record", "list", "--limit", "25")
	require.NoError(t, err)
	require.Contains(t, out, "uid")
	require.Contains(t, out, "u-1")
}

func TestRecordListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "record", "list")
	require.NoError(t, err)
	require.Contains(t, out, "no records")
}
