package cli

import (
	"encoding/base64"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

// decodedQ decodes the base64url `q=` param into the raw JSON condition that
// the CLI sent. Lets each show/ack/close test assert it queried by uid
// without coupling the test to the encoding details.
func decodedQ(t *testing.T, raw string) string {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(raw)
	require.NoError(t, err)
	return string(b)
}

func TestRecordShow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/record/", r.URL.Path)
		require.Equal(t, `["=","uid","u-1"]`, decodedQ(t, r.URL.Query().Get("q")))
		require.Equal(t, "1", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"db-1","severity":"warn","message":"oops"}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "record", "show", "u-1")
	require.NoError(t, err)
	require.Contains(t, out, "uid: u-1")
	require.Contains(t, out, "host: db-1")
}

func TestRecordShowNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "record", "show", "missing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no record found with uid missing")
}

func TestRecordAck(t *testing.T) {
	var (
		postSeen   bool
		commentBod map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/comment":
			postSeen = true
			require.NoError(t, json.NewDecoder(r.Body).Decode(&commentBod))
			_, _ = w.Write([]byte(`{"data":[{"uid":"c-1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/record/":
			require.Equal(t, `["=","uid","u-1"]`, decodedQ(t, r.URL.Query().Get("q")))
			_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"db-1","message":"disk full on db-1"}]}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL
	rt.flags.User = "alice"

	out, _, err := executeCmd(t, rt, "record", "ack", "u-1", "-m", "looking into it")
	require.NoError(t, err)
	require.True(t, postSeen)
	require.Equal(t, "ack", commentBod["type"])
	require.Equal(t, "u-1", commentBod["record_uid"])
	require.Equal(t, "alice", commentBod["name"])
	require.Equal(t, "local", commentBod["method"])
	require.Equal(t, "looking into it", commentBod["message"])
	require.Contains(t, out, "Acked u-1")
	require.Contains(t, out, "db-1")
}

func TestRecordAckDefaultMessage(t *testing.T) {
	var commentBod map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&commentBod))
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"db-1","message":"x"}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, _, err := executeCmd(t, rt, "record", "ack", "u-1")
	require.NoError(t, err)
	require.Equal(t, "Acked via snooze CLI", commentBod["message"])
}

func TestRecordClose(t *testing.T) {
	var commentBod map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			require.Equal(t, "/api/v1/comment", r.URL.Path)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&commentBod))
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"uid":"u-2","host":"web-3","message":"503s"}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "record", "close", "u-2", "--message", "resolved upstream")
	require.NoError(t, err)
	require.Equal(t, "close", commentBod["type"])
	require.Equal(t, "u-2", commentBod["record_uid"])
	require.Equal(t, "resolved upstream", commentBod["message"])
	require.Contains(t, out, "Closed u-2")
	require.Contains(t, out, "web-3")
}
