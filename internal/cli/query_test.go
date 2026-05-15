package cli

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/rule/", r.URL.Path)
		require.Empty(t, r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"r1","name":"my-rule"}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "query", "rule")
	require.NoError(t, err)
	require.Contains(t, out, "r1")
	require.Contains(t, out, "my-rule")
}

func TestQueryWithCondition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		require.NotEmpty(t, q, "expected q= to be set when --condition is passed")
		raw, err := base64.RawURLEncoding.DecodeString(q)
		require.NoError(t, err)
		require.Contains(t, string(raw), `host`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "query", "record",
		"--condition", `{"type":"=","field":"host","value":"db-1"}`)
	require.NoError(t, err)
}

func TestQueryLimitAndOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "10", r.URL.Query().Get("limit"))
		require.Equal(t, "host", r.URL.Query().Get("orderby"))
		require.Equal(t, "false", r.URL.Query().Get("asc"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "query", "rule",
		"--limit", "10", "--orderby", "host", "--asc", "false")
	require.NoError(t, err)
}

func TestQueryBadAscFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called when --asc is invalid")
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL
	_, _, err := executeCmd(t, rt, "query", "rule", "--asc", "maybe")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--asc")
}

func TestBuildQueryPathNoFlags(t *testing.T) {
	path, err := buildQueryPath("rule", "", 0, 0, "", "")
	require.NoError(t, err)
	require.Equal(t, "/api/v1/rule/", path)
}
