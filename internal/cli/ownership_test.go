package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// ownershipServer fakes the comment POST + the record re-fetch that
// postRecordComment does, capturing the comment body.
func ownershipServer(t *testing.T, owner string, got *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/comment":
			require.NoError(t, json.NewDecoder(r.Body).Decode(got))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/record/":
			_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"db-1","message":"disk full","owner":"` + owner + `"}]}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestRecordAssign(t *testing.T) {
	var got map[string]any
	srv := ownershipServer(t, "bob", &got)
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "record", "assign", "u-1", "bob", "--user-method", "ldap")
	require.NoError(t, err)
	require.Equal(t, "assign", got["type"])
	require.Equal(t, "u-1", got["record_uid"])
	require.Equal(t, "bob", got["assignee"])
	require.Equal(t, "ldap", got["assignee_method"])
	require.Contains(t, out, "Assigned u-1 (db-1: disk full) — owner bob")
}

func TestRecordAssignOmitsUnsetMethod(t *testing.T) {
	var got map[string]any
	srv := ownershipServer(t, "bob", &got)
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	_, _, err := executeCmd(t, rt, "record", "assign", "u-1", "bob")
	require.NoError(t, err)
	// The server fills the method in from the directory when it is absent.
	require.NotContains(t, got, "assignee_method")
}

func TestRecordRelease(t *testing.T) {
	var got map[string]any
	srv := ownershipServer(t, "", &got)
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "record", "release", "u-1", "-m", "end of shift")
	require.NoError(t, err)
	require.Equal(t, "release", got["type"])
	require.Equal(t, "end of shift", got["message"])
	require.Contains(t, out, "Released u-1 (db-1: disk full)")
	require.NotContains(t, out, "owner")
}

func TestRecordAckShowsOwner(t *testing.T) {
	var got map[string]any
	srv := ownershipServer(t, "root", &got)
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "record", "ack", "u-1")
	require.NoError(t, err)
	require.Contains(t, out, "Acked u-1 (db-1: disk full) — owner root")
}

func TestRecordOwners(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/record/owners", r.URL.Path)
		require.Equal(t, `["=","state","ack"]`, decodedQ(t, r.URL.Query().Get("q")))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"owner":"bob","count":3},{"owner":"alice","count":1}],"unowned":2,"total":6}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "record", "owners", "-c", `["=","state","ack"]`)
	require.NoError(t, err)
	require.Regexp(t, `bob\s+3`, out)
	require.Regexp(t, `alice\s+1`, out)
	require.Regexp(t, `\(unowned\)\s+2`, out)
	require.Regexp(t, `\(total\)\s+6`, out)
}

func TestRecordOwnersNoConditionSendsNoQ(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"unowned":0,"total":0}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	_, _, err := executeCmd(t, rt, "record", "owners")
	require.NoError(t, err)
}

func TestRecordBulk(t *testing.T) {
	for name, tc := range map[string]struct {
		args     []string
		path     string
		wantBody map[string]any
		resp     string
		wantOut  string
	}{
		"state": {
			args:     []string{"state", "ack", "-c", `["=","host","db-1"]`, "-m", "maintenance"},
			path:     "/api/v1/record/bulk_state",
			wantBody: map[string]any{"state": "ack", "message": "maintenance"},
			resp:     `{"matched":4,"updated":4,"state":"ack"}`,
			wantOut:  "ack: 4 matched, 4 updated",
		},
		"assign": {
			args:     []string{"assign", "bob", "-c", `["=","host","db-1"]`, "--user-method", "local"},
			path:     "/api/v1/record/bulk_owner",
			wantBody: map[string]any{"action": "assign", "assignee": "bob", "assignee_method": "local"},
			resp:     `{"matched":4,"updated":3,"action":"assign"}`,
			wantOut:  "assign: 4 matched, 3 updated",
		},
		"release": {
			args:     []string{"release", "-c", `["=","host","db-1"]`},
			path:     "/api/v1/record/bulk_owner",
			wantBody: map[string]any{"action": "release"},
			resp:     `{"matched":4,"updated":2,"action":"release"}`,
			wantOut:  "release: 4 matched, 2 updated",
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, tc.path, r.URL.Path)
				require.Equal(t, `["=","host","db-1"]`, decodedQ(t, r.URL.Query().Get("q")))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, tc.wantBody, body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.resp))
			}))
			defer srv.Close()
			rt, _, _ := newTestRuntime(t, srv)
			rt.flags.Token = "tok"

			out, _, err := executeCmd(t, rt, append([]string{"record", "bulk"}, tc.args...)...)
			require.NoError(t, err)
			require.Contains(t, out, tc.wantOut)
		})
	}
}

func TestRecordBulkAllSendsNoQ(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"matched":9,"updated":9,"action":"release"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "record", "bulk", "release", "--all")
	require.NoError(t, err)
	require.Contains(t, out, "release: 9 matched")
}

// A bulk write never goes out on a missing, ambiguous or malformed target.
func TestRecordBulkRefusesBadTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("server must not be called, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no target":     {[]string{"release"}, "--all"},
		"both targets":  {[]string{"release", "--all", "-c", `["=","a","b"]`}, "not both"},
		"bad json":      {[]string{"assign", "bob", "-c", `["=","a"`}, "invalid --condition JSON"},
		"unknown state": {[]string{"state", "shelved", "--all"}, "state must be one of"},
	} {
		t.Run(name, func(t *testing.T) {
			rt, _, _ := newTestRuntime(t, srv)
			rt.flags.Token = "tok"
			_, _, err := executeCmd(t, rt, append([]string{"record", "bulk"}, tc.args...)...)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRecordListShowsOwnerColumn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"db-1","state":"ack","owner":"bob","message":"oops"}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "record", "list")
	require.NoError(t, err)
	require.Regexp(t, `state\s+owner\s+message`, out)
	require.Regexp(t, `ack\s+bob\s+oops`, out)
}
