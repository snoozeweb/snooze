package cli

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
)

// listQ captures the condition `record list` sent, decoded back into a Cond so
// the tests compare structure, not JSON spelling.
func listQ(t *testing.T, r *http.Request) condition.Cond {
	t.Helper()
	var c condition.Cond
	require.NoError(t, json.Unmarshal([]byte(decodedQ(t, r.URL.Query().Get("q"))), &c))
	return c
}

// wire passes a Cond through its JSON form, as the server receives it (numbers
// come back as float64), so an expected Cond compares equal to a decoded one.
func wire(t *testing.T, c condition.Cond) condition.Cond {
	t.Helper()
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	var out condition.Cond
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestRecordListFilters(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want condition.Cond
	}{
		"host+severity": {
			[]string{"--host", "db-1", "--severity", "critical"},
			condition.And(condition.Equals("host", "db-1"), condition.Equals("severity", "critical")),
		},
		"state open means untouched or re-opened": {
			[]string{"--state", "open,ack"},
			condition.Or(condition.Not(condition.Exists("state")), condition.Equals("state", ""),
				condition.Equals("state", "open"), condition.Equals("state", "ack")),
		},
		"owner none":  {[]string{"--owner", "none"}, unowned()},
		"owner login": {[]string{"--owner", "bob"}, condition.Equals("owner", "bob")},
		"owner me from --user": {
			[]string{"--user", "alice", "--owner", "me"}, condition.Equals("owner", "alice"),
		},
		"active + extra condition": {
			[]string{"--active", "-c", `["=","environment","prod"]`},
			condition.And(activeAlerts(), condition.Equals("environment", "prod")),
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/v1/record/", r.URL.Path)
				require.Equal(t, "date_epoch", r.URL.Query().Get("orderby"))
				require.Equal(t, "false", r.URL.Query().Get("asc"))
				require.Equal(t, wire(t, tc.want), listQ(t, r))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer srv.Close()
			rt, _, _ := newTestRuntime(t, srv)
			rt.flags.Token = "tok"

			_, _, err := executeCmd(t, rt, append([]string{"record", "list"}, tc.args...)...)
			require.NoError(t, err)
		})
	}
}

func TestRecordListOwnerMeFromToken(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"carol","method":"ldap"}`))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, condition.Equals("owner", "carol"), listQ(t, r))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "h." + payload + ".sig"

	_, _, err := executeCmd(t, rt, "record", "list", "--owner", "me")
	require.NoError(t, err)
}

func TestRecordListRejectsBadFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("server must not be called, got %s", r.URL)
	}))
	defer srv.Close()
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"unknown state":    {[]string{"--state", "firing"}, "must be one of"},
		"bad condition":    {[]string{"-c", `["=","a"`}, "invalid --condition JSON"},
		"me without login": {[]string{"--owner", "me"}, "cannot tell who you are"},
	} {
		t.Run(name, func(t *testing.T) {
			rt, _, _ := newTestRuntime(t, srv)
			rt.flags.Token = "not-a-jwt"
			_, _, err := executeCmd(t, rt, append([]string{"record", "list"}, tc.args...)...)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRecordReopenEscalateComment(t *testing.T) {
	for name, tc := range map[string]struct {
		args     []string
		wantType string
		wantOut  string
	}{
		"reopen":   {[]string{"reopen", "u-1"}, "open", "Re-opened u-1"},
		"escalate": {[]string{"escalate", "u-1"}, "esc", "Re-escalated u-1"},
		"comment":  {[]string{"comment", "u-1", "-m", "rotated the logs"}, "comment", "Commented on u-1"},
	} {
		t.Run(name, func(t *testing.T) {
			var got map[string]any
			srv := ownershipServer(t, "", &got)
			defer srv.Close()
			rt, _, _ := newTestRuntime(t, srv)
			rt.flags.Token = "tok"

			out, _, err := executeCmd(t, rt, append([]string{"record"}, tc.args...)...)
			require.NoError(t, err)
			require.Equal(t, tc.wantType, got["type"])
			require.Contains(t, out, tc.wantOut)
		})
	}
}

func TestRecordCommentRequiresMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("server must not be called, got %s", r.URL)
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	_, _, err := executeCmd(t, rt, "record", "comment", "u-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "needs a message")
}

func TestRecordComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/comment/", r.URL.Path)
		require.Equal(t, condition.Equals("record_uid", "u-1"), listQ(t, r))
		require.Equal(t, "date_epoch", r.URL.Query().Get("orderby"))
		require.Equal(t, "true", r.URL.Query().Get("asc"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"type":"ack","user":"root","message":"on it","date_epoch":1790000000},
			{"type":"assign","user":"root","assignee":"bob","date_epoch":1790000060},
			{"type":"open","auto":true,"message":"Auto re-opened\nsecond line","date_epoch":1790000120}]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "record", "comments", "u-1")
	require.NoError(t, err)
	require.Regexp(t, `2026-09-21 \d\d:\d\dZ\s+ack\s+root\s+on it`, out)
	require.Regexp(t, `assign\s+root\s+→ bob`, out)
	require.Regexp(t, `open\s+system\s+Auto re-opened second line`, out)
}

func TestRecordAgenticStatus(t *testing.T) {
	var put map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/record/u-1/agentic", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"uid":"u-1","agentic":{
				"root_cause":{"summary":"disk full","confidence":"high","evidence":["df: 100%"]},
				"remediation_plan":{"status":"action_required","steps":[{"action":"rotate logs","risk":"low","when":"now"}]},
				"analysis":{"at":"2026-09-21T09:40:00Z","by":"alert-agent","source":"alert-rca"}}}`))
		case http.MethodPut:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&put))
			_, _ = w.Write([]byte(`{"uid":"u-1","agentic":{}}`))
		default:
			t.Fatalf("unexpected %s", r.Method)
		}
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "record", "agentic", "status", "u-1", "resolved", "--source", "snooze-skill")
	require.NoError(t, err)
	require.Contains(t, out, "Verdict on u-1: action_required -> resolved")
	// Only the verdict changed; provenance is never sent back.
	require.NotContains(t, put, "analysis")
	require.Equal(t, "snooze-skill", put["source"])
	plan := put["remediation_plan"].(map[string]any)
	require.Equal(t, "resolved", plan["status"])
	require.Len(t, plan["steps"], 1)
	require.Equal(t, []any{"df: 100%"}, put["root_cause"].(map[string]any)["evidence"])
}

func TestRecordAgenticStatusRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		status, get string
		code        int
		want        string
	}{
		"unknown status": {"fixed", `{"uid":"u-1","agentic":{"root_cause":{"summary":"x","confidence":"low"},` +
			`"remediation_plan":{"steps":[{"action":"a","risk":"low"}]}}}`, 200, "failed schema validation"},
		"no analysis": {"resolved", `{"error":{"code":"not_found","message":"no analysis"}}`, 404, "no agentic analysis"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method, "a doomed update must not PUT")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.get))
			}))
			defer srv.Close()
			rt, _, _ := newTestRuntime(t, srv)
			rt.flags.Token = "tok"
			_, _, err := executeCmd(t, rt, "record", "agentic", "status", "u-1", tc.status)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
