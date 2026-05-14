package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestJira spins up an httptest.Server speaking enough of the JIRA REST v3
// API to drive the forwarder. The handler does request inspection inline so
// tests stay close to the assertion they care about.
func newTestJira(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(ClientOptions{
		BaseURL:    srv.URL,
		Email:      "bot@example.com",
		Token:      "tok",
		VerifySSL:  true,
		HTTPClient: srv.Client(),
	})
}

// readBodyMap drains r.Body and decodes it as a generic JSON map.
func readBodyMap(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

func TestForward_createIssue_newAlert(t *testing.T) {
	var createReq atomic.Pointer[map[string]any]
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
			m := readBodyMap(t, r)
			createReq.Store(&m)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "10001", "key": "OPS-1"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	cfg, err := minimalCfg().WithDefaults()
	require.NoError(t, err)
	f := newForwarder(cfg, client, nil)

	rec := recordSummary{
		"host":     "srv-1",
		"severity": "critical",
		"message":  "disk full",
		"hash":     "abc123",
	}
	out := f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      rec,
		Message:    "be quick",
	}}, "jira-action")

	require.Equal(t, "OPS-1", out["abc123"].IssueKey)

	got := *createReq.Load()
	fields := got["fields"].(map[string]any)
	require.Equal(t, "OPS", fields["project"].(map[string]any)["key"])
	require.Equal(t, "Task", fields["issuetype"].(map[string]any)["name"])
	// severity=critical → priority_mapping → High
	require.Equal(t, "High", fields["priority"].(map[string]any)["name"])
	// Summary is rendered from the default template.
	require.Equal(t, "[critical] srv-1 - disk full", fields["summary"])
	// Description carries the message line.
	rawDesc, err := json.Marshal(fields["description"])
	require.NoError(t, err)
	require.Contains(t, string(rawDesc), "Custom message")
	require.Contains(t, string(rawDesc), "be quick")
}

func TestForward_existingIssue_comments(t *testing.T) {
	var commentBody atomic.Pointer[map[string]any]
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comment"):
			m := readBodyMap(t, r)
			commentBody.Store(&m)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	cfg, _ := minimalCfg().WithDefaults()
	f := newForwarder(cfg, client, nil)

	rec := recordSummary{
		"host":     "srv-1",
		"severity": "warning",
		"message":  "still failing",
		"hash":     "xyz",
		"snooze_webhook_responses": []any{
			map[string]any{
				"action_name": "jira-action",
				"content":     map[string]any{"issue_key": "OPS-99"},
			},
		},
	}
	out := f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      rec,
	}}, "jira-action")
	require.Equal(t, "OPS-99", out["xyz"].IssueKey)

	require.NotNil(t, commentBody.Load())
}

func TestForward_priorityOverride(t *testing.T) {
	var got map[string]any
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		got = readBodyMap(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-2"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	f := newForwarder(cfg, client, nil)
	_ = f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Priority:   "Lowest",
		Alert:      recordSummary{"severity": "critical", "hash": "h"},
	}}, "jira-action")
	require.Equal(t, "Lowest", got["fields"].(map[string]any)["priority"].(map[string]any)["name"])
}

func TestForward_issueTypeIDPrecedence(t *testing.T) {
	var got map[string]any
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		got = readBodyMap(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-3"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	cfg.IssueTypeID = "99999"
	f := newForwarder(cfg, client, nil)
	_ = f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey:  "OPS",
		IssueTypeID: jsonString("12345"),
		Alert:       recordSummary{"hash": "h"},
	}}, "jira-action")
	issuetype := got["fields"].(map[string]any)["issuetype"].(map[string]any)
	require.Equal(t, "12345", issuetype["id"])
	require.Nil(t, issuetype["name"])
}

func TestForward_customFieldsMerge(t *testing.T) {
	var got map[string]any
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		got = readBodyMap(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-4"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	cfg.CustomFields = map[string]any{
		"customfield_10100": map[string]any{"value": "Infrastructure"},
		"customfield_10200": "default-value",
	}
	f := newForwarder(cfg, client, nil)
	_ = f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      recordSummary{"hash": "h"},
		CustomFields: map[string]any{
			"customfield_10200": "payload-override",
		},
	}}, "jira-action")
	fields := got["fields"].(map[string]any)
	// Default kept.
	require.Equal(t, map[string]any{"value": "Infrastructure"}, fields["customfield_10100"])
	// Payload overrides config default.
	require.Equal(t, "payload-override", fields["customfield_10200"])
}

func TestForward_priorityFallbackToString(t *testing.T) {
	var attempts atomic.Int32
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		body := readBodyMap(t, r)
		fields := body["fields"].(map[string]any)
		if n == 1 {
			// First request: priority is an object — reply with the canonical
			// "priority must be string" error so the client retries.
			require.IsType(t, map[string]any{}, fields["priority"])
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": map[string]string{
					"priority": "Expected a String value, got an object.",
				},
			})
			return
		}
		// Second request: priority should be a bare string.
		require.Equal(t, "High", fields["priority"])
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-5"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	f := newForwarder(cfg, client, nil)
	out := f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Priority:   "High",
		Alert:      recordSummary{"hash": "h"},
	}}, "jira-action")
	require.Equal(t, "OPS-5", out["h"].IssueKey)
	require.Equal(t, int32(2), attempts.Load(), "expected one retry after string-priority hint")
}

func TestForward_emailToAccountIDResolution(t *testing.T) {
	var lookups atomic.Int32
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/user/search":
			lookups.Add(1)
			require.Equal(t, "alice@example.com", r.URL.Query().Get("query"))
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"accountId": "acc-alice", "emailAddress": "alice@example.com"},
			})
		case r.URL.Path == "/rest/api/3/issue":
			body := readBodyMap(t, r)
			fields := body["fields"].(map[string]any)
			require.Equal(t, "acc-alice", fields["assignee"].(map[string]any)["id"])
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-6"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	cfg, _ := minimalCfg().WithDefaults()
	cfg.Assignee = "alice@example.com"
	f := newForwarder(cfg, client, nil)
	// Two alerts → the email lookup should only run once thanks to the cache.
	_ = f.handleEnvelopes(context.Background(), []envelope{
		{ProjectKey: "OPS", Alert: recordSummary{"hash": "h1"}},
		{ProjectKey: "OPS", Alert: recordSummary{"hash": "h2"}},
	}, "jira-action")
	require.Equal(t, int32(1), lookups.Load())
}

func TestForward_messageLimitDropsExcess(t *testing.T) {
	var creations atomic.Int32
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		creations.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	cfg.MessageLimit = 2
	f := newForwarder(cfg, client, nil)
	envs := []envelope{
		{ProjectKey: "OPS", Alert: recordSummary{"hash": "a"}},
		{ProjectKey: "OPS", Alert: recordSummary{"hash": "b"}},
		{ProjectKey: "OPS", Alert: recordSummary{"hash": "c"}},
	}
	_ = f.handleEnvelopes(context.Background(), envs, "jira-action")
	require.Equal(t, int32(2), creations.Load())
}

func TestForward_summaryClampedTo255(t *testing.T) {
	var got map[string]any
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		got = readBodyMap(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-7"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	f := newForwarder(cfg, client, nil)
	long := strings.Repeat("x", 400)
	_ = f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      recordSummary{"hash": "h", "message": long},
	}}, "jira-action")
	summary := got["fields"].(map[string]any)["summary"].(string)
	require.LessOrEqual(t, len(summary), 255)
}

// concurrency smoke test: the forwarder's user cache uses a sync.Mutex; make
// sure parallel lookups don't deadlock or trigger the race detector.
func TestForward_userCacheConcurrent(t *testing.T) {
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/user/search":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"accountId": "acc"}})
		case "/rest/api/3/issue":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-9"})
		}
	})
	cfg, _ := minimalCfg().WithDefaults()
	cfg.Assignee = "alice@example.com"
	f := newForwarder(cfg, client, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = f.handleEnvelopes(context.Background(), []envelope{{
				ProjectKey: "OPS",
				Alert:      recordSummary{"hash": "h"},
			}}, "jira-action")
		}(i)
	}
	wg.Wait()
}

// minimalCfg returns the in-package equivalent of the external-test
// minimal() helper. config_test.go is package jira_test so it can't be
// shared, hence the duplicate.
func minimalCfg() Config {
	return Config{
		JiraURL:      "https://my.atlassian.net",
		JiraEmail:    "bot@example.com",
		JiraAPIToken: "tok",
		ProjectKey:   "OPS",
	}
}
