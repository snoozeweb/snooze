package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/jiraadf"
)

// testScheme is the JIRA-default 5-entry priority scheme served by
// newTestJira: ids 1..5, ordered most severe first.
var testScheme = []map[string]any{
	{"id": "1", "name": "Highest"},
	{"id": "2", "name": "High"},
	{"id": "3", "name": "Medium"},
	{"id": "4", "name": "Low"},
	{"id": "5", "name": "Lowest"},
}

// writeCreateMeta replies with a createmeta document exposing scheme as the
// priority field's allowed values.
func writeCreateMeta(w http.ResponseWriter, scheme []map[string]any) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"projects": []map[string]any{{
			"issuetypes": []map[string]any{{
				"id":     "10001",
				"fields": map[string]any{"priority": map[string]any{"allowedValues": scheme}},
			}},
		}},
	})
}

// newTestJira spins up an httptest.Server speaking enough of the JIRA REST v3
// API to drive the forwarder. Priority-scheme discovery is answered with
// testScheme so individual tests only handle the calls they assert on; use
// newTestJiraRaw when the test wants to drive discovery itself.
func newTestJira(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	return newTestJiraRaw(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta":
			writeCreateMeta(w, testScheme)
		case "/rest/api/3/priority":
			_ = json.NewEncoder(w).Encode(testScheme)
		default:
			h(w, r)
		}
	})
}

// newTestJiraRaw is newTestJira without the priority-scheme stubs.
func newTestJiraRaw(t *testing.T, h http.HandlerFunc) *Client {
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

	rec := jiraadf.RecordSummary{
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
	// severity=critical → positional in the live scheme → High, sent by id
	require.Equal(t, map[string]any{"id": "2"}, fields["priority"])
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

	rec := jiraadf.RecordSummary{
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
		Alert:      jiraadf.RecordSummary{"severity": "critical", "hash": "h"},
	}}, "jira-action")
	// The override names a real priority, so it wins over the severity — and
	// still goes on the wire as an id.
	require.Equal(t, map[string]any{"id": "5"}, got["fields"].(map[string]any)["priority"])
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
		Alert:       jiraadf.RecordSummary{"hash": "h"},
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
		Alert:      jiraadf.RecordSummary{"hash": "h"},
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

// When the site can't be introspected at all we fall back to the pre-resolver
// behaviour: send the operator's priority name, and honour JIRA's
// "priority must be a string" hint.
func TestForward_priorityFallbackToString(t *testing.T) {
	var attempts atomic.Int32
	client := newTestJiraRaw(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/issue/createmeta" || r.URL.Path == "/rest/api/3/priority" {
			w.WriteHeader(http.StatusNotFound) // no scheme discoverable
			return
		}
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
		Alert:      jiraadf.RecordSummary{"hash": "h"},
	}}, "jira-action")
	require.Equal(t, "OPS-5", out["h"].IssueKey)
	require.Equal(t, int32(2), attempts.Load(), "expected one retry after string-priority hint")
}

func TestForward_emailToAccountIDResolution(t *testing.T) {
	var lookups atomic.Int32
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/user/search":
			lookups.Add(1)
			require.Equal(t, "alice@example.com", r.URL.Query().Get("query"))
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"accountId": "acc-alice", "emailAddress": "alice@example.com"},
			})
		case "/rest/api/3/issue":
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
		{ProjectKey: "OPS", Alert: jiraadf.RecordSummary{"hash": "h1"}},
		{ProjectKey: "OPS", Alert: jiraadf.RecordSummary{"hash": "h2"}},
	}, "jira-action")
	require.Equal(t, int32(1), lookups.Load())
}

func TestForward_messageLimitDropsExcess(t *testing.T) {
	var creations atomic.Int32
	client := newTestJira(t, func(w http.ResponseWriter, _ *http.Request) {
		creations.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	cfg.MessageLimit = 2
	f := newForwarder(cfg, client, nil)
	envs := []envelope{
		{ProjectKey: "OPS", Alert: jiraadf.RecordSummary{"hash": "a"}},
		{ProjectKey: "OPS", Alert: jiraadf.RecordSummary{"hash": "b"}},
		{ProjectKey: "OPS", Alert: jiraadf.RecordSummary{"hash": "c"}},
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
		Alert:      jiraadf.RecordSummary{"hash": "h", "message": long},
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
		go func(_ int) {
			defer wg.Done()
			_ = f.handleEnvelopes(context.Background(), []envelope{{
				ProjectKey: "OPS",
				Alert:      jiraadf.RecordSummary{"hash": "h"},
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

// The envelope can override the issue title, either as a literal or as a
// ${var} template, and `summary` wins over `summary_template`.
func TestForward_summaryOverriddenByEnvelope(t *testing.T) {
	cases := []struct {
		name string
		env  envelope
		want string
	}{
		{
			name: "config default",
			env:  envelope{},
			want: "[critical] srv-1 - disk full",
		},
		{
			name: "literal summary",
			env:  envelope{Summary: "Disk full on the primary"},
			want: "Disk full on the primary",
		},
		{
			name: "templated summary",
			env:  envelope{Summary: "DISK ${host}/${severity}"},
			want: "DISK srv-1/critical",
		},
		{
			name: "summary_template only",
			env:  envelope{SummaryTemplate: "T: ${message}"},
			want: "T: disk full",
		},
		{
			name: "summary wins over summary_template",
			env:  envelope{Summary: "winner", SummaryTemplate: "loser"},
			want: "winner",
		},
		{
			name: "blank override falls back to config",
			env:  envelope{Summary: "   "},
			want: "[critical] srv-1 - disk full",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
				got = readBodyMap(t, r)
				_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-8"})
			})
			cfg, err := minimalCfg().WithDefaults()
			require.NoError(t, err)
			f := newForwarder(cfg, client, nil)
			env := tc.env
			env.ProjectKey = "OPS"
			env.Alert = jiraadf.RecordSummary{
				"hash": "h", "host": "srv-1", "severity": "critical", "message": "disk full",
			}
			_ = f.handleEnvelopes(context.Background(), []envelope{env}, "jira-action")
			require.Equal(t, tc.want, got["fields"].(map[string]any)["summary"])
		})
	}
}

// A per-envelope override is clamped to JIRA's 255-character limit too.
func TestForward_envelopeSummaryClampedTo255(t *testing.T) {
	var got map[string]any
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		got = readBodyMap(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-9"})
	})
	cfg, err := minimalCfg().WithDefaults()
	require.NoError(t, err)
	f := newForwarder(cfg, client, nil)
	_ = f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Summary:    strings.Repeat("y", 400),
		Alert:      jiraadf.RecordSummary{"hash": "h"},
	}}, "jira-action")
	require.Len(t, got["fields"].(map[string]any)["summary"].(string), 255)
}

// The /alert wire format accepts the summary override keys.
func TestEnvelope_decodesSummaryKeys(t *testing.T) {
	var env envelope
	require.NoError(t, json.Unmarshal([]byte(
		`{"summary":"custom title","summary_template":"T ${host}"}`), &env))
	require.Equal(t, "custom title", env.Summary)
	require.Equal(t, "T ${host}", env.SummaryTemplate)
}

// frenchScheme is a localized 4-entry scheme: the exact shape that made the
// English default mapping fail with `400 priority: … invalide`.
var frenchScheme = []map[string]any{
	{"id": "1", "name": "Critique"},
	{"id": "2", "name": "Grave"},
	{"id": "3", "name": "Moyen"},
	{"id": "4", "name": "Faible"},
}

// A mapping written in English against a localized site must not fail the
// create: the unresolvable name is ignored and the severity is placed
// positionally in the real scheme.
func TestForward_priorityMappingIgnoredWhenNotInScheme(t *testing.T) {
	var got map[string]any
	client := newTestJiraRaw(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta":
			writeCreateMeta(w, frenchScheme)
		case "/rest/api/3/issue":
			got = readBodyMap(t, r)
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "CG-1"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	cfg, err := minimalCfg().WithDefaults()
	require.NoError(t, err)
	cfg.PriorityMapping = map[string]string{"critical": "Highest"} // English: no such name
	cfg.Priority = "Medium"                                        // also absent
	f := newForwarder(cfg, client, nil)

	out := f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      jiraadf.RecordSummary{"hash": "h", "severity": "critical"},
	}}, "jira-action")

	require.Equal(t, "CG-1", out["h"].IssueKey)
	// critical → second position of a 4-entry scheme → Grave (id 2).
	require.Equal(t, map[string]any{"id": "2"}, got["fields"].(map[string]any)["priority"])
}

// An id in the mapping is used as-is: the language-free way to pin a priority.
func TestForward_priorityMappingById(t *testing.T) {
	var got map[string]any
	client := newTestJiraRaw(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta":
			writeCreateMeta(w, frenchScheme)
		default:
			got = readBodyMap(t, r)
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "CG-2"})
		}
	})
	cfg, err := minimalCfg().WithDefaults()
	require.NoError(t, err)
	cfg.PriorityMapping = map[string]string{"critical": "1"}
	f := newForwarder(cfg, client, nil)
	_ = f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      jiraadf.RecordSummary{"hash": "h", "severity": "critical"},
	}}, "jira-action")
	require.Equal(t, map[string]any{"id": "1"}, got["fields"].(map[string]any)["priority"])
}

// A severity outside Snooze's ladder gets no priority field at all, so JIRA
// applies the scheme's own default instead of us guessing.
func TestForward_unrankedSeverityOmitsPriority(t *testing.T) {
	var got map[string]any
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		got = readBodyMap(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-6"})
	})
	cfg, err := minimalCfg().WithDefaults()
	require.NoError(t, err)
	f := newForwarder(cfg, client, nil)
	_ = f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      jiraadf.RecordSummary{"hash": "h", "severity": "major"},
	}}, "jira-action")
	_, present := got["fields"].(map[string]any)["priority"]
	require.False(t, present, "priority must be omitted for an unranked severity")
}

// The scheme is fetched once per (project, issue type), not once per alert.
func TestForward_prioritySchemeCached(t *testing.T) {
	var metaCalls, creates atomic.Int32
	client := newTestJiraRaw(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta":
			metaCalls.Add(1)
			writeCreateMeta(w, testScheme)
		default:
			n := creates.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"key": fmt.Sprintf("OPS-%d", n)})
		}
	})
	cfg, err := minimalCfg().WithDefaults()
	require.NoError(t, err)
	f := newForwarder(cfg, client, nil)

	envs := make([]envelope, 0, 5)
	for i := 0; i < 5; i++ {
		envs = append(envs, envelope{
			ProjectKey: "OPS",
			Alert:      jiraadf.RecordSummary{"hash": fmt.Sprintf("h%d", i), "severity": "warning"},
		})
	}
	out := f.handleEnvelopes(context.Background(), envs, "jira-action")
	require.Len(t, out, 5)
	require.Equal(t, int32(5), creates.Load())
	require.Equal(t, int32(1), metaCalls.Load(), "scheme must be cached across alerts")
}

// When JIRA rejects the priority we resolved, the cached scheme is dropped and
// the create is retried once against a freshly fetched one.
func TestForward_priorityRejectedRefetchesScheme(t *testing.T) {
	var metaCalls, creates atomic.Int32
	// The site is renumbered between the two createmeta calls: the id we first
	// resolve stops existing.
	staleScheme := []map[string]any{{"id": "77", "name": "Ancienne"}}
	client := newTestJiraRaw(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta":
			if metaCalls.Add(1) == 1 {
				writeCreateMeta(w, staleScheme)
				return
			}
			writeCreateMeta(w, frenchScheme)
		default:
			body := readBodyMap(t, r)
			prio, _ := body["fields"].(map[string]any)["priority"].(map[string]any)
			if creates.Add(1) == 1 {
				require.Equal(t, map[string]any{"id": "77"}, prio)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"errors": map[string]string{"priority": "La priorité sélectionnée n'est pas valide."},
				})
				return
			}
			require.Equal(t, map[string]any{"id": "1"}, prio, "retry must use the refetched scheme")
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "CG-3"})
		}
	})
	cfg, err := minimalCfg().WithDefaults()
	require.NoError(t, err)
	f := newForwarder(cfg, client, nil)
	out := f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      jiraadf.RecordSummary{"hash": "h", "severity": "emergency"},
	}}, "jira-action")

	require.Equal(t, "CG-3", out["h"].IssueKey)
	require.Equal(t, int32(2), creates.Load(), "expected exactly one retry")
	require.Equal(t, int32(2), metaCalls.Load(), "scheme must be refetched after a rejection")
}

// createmeta not exposing the priority field falls back to the site-wide list.
func TestForward_priorityFallsBackToGlobalList(t *testing.T) {
	var got map[string]any
	var listCalls atomic.Int32
	client := newTestJiraRaw(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta":
			// Priority is not on the create screen.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"issuetypes": []map[string]any{{"id": "10001", "fields": map[string]any{}}}}},
			})
		case "/rest/api/3/priority":
			listCalls.Add(1)
			_ = json.NewEncoder(w).Encode(frenchScheme)
		default:
			got = readBodyMap(t, r)
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "CG-4"})
		}
	})
	cfg, err := minimalCfg().WithDefaults()
	require.NoError(t, err)
	f := newForwarder(cfg, client, nil)
	_ = f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "OPS",
		Alert:      jiraadf.RecordSummary{"hash": "h", "severity": "warning"},
	}}, "jira-action")
	require.Equal(t, int32(1), listCalls.Load())
	require.Equal(t, map[string]any{"id": "3"}, got["fields"].(map[string]any)["priority"])
}

// TestForward_batchShapedResponseHandleComments is the regression for the
// production duplicate-ticket bug: the handle this daemon hands back is keyed
// by alert hash (`{"<hash>": {"issue_key": …}}`) and older servers stamped
// that envelope verbatim under `response_<action>`. Reading only
// `response_<action>.issue_key` missed it, so every re-escalation of the same
// alert opened a fresh ticket (CG-1811 then CG-1812, 2026-08-28 02:11/02:13).
func TestForward_batchShapedResponseHandleComments(t *testing.T) {
	var commented atomic.Pointer[string]
	client := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comment"):
			key := strings.Split(strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/"), "/")[0]
			commented.Store(&key)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request (a create means the handle was missed): %s %s", r.Method, r.URL.Path)
		}
	})
	cfg, _ := minimalCfg().WithDefaults()
	f := newForwarder(cfg, client, nil)

	rec := jiraadf.RecordSummary{
		"host": "K8S ovh", "severity": "critical", "message": "Deployment down",
		"hash": "ec2f9135",
		"response_Jira Ticket": map[string]any{
			"ec2f9135": map[string]any{"issue_key": "CG-1811"},
		},
	}
	out := f.handleEnvelopes(context.Background(), []envelope{{
		ProjectKey: "CG", Alert: rec,
	}}, "Jira Ticket")

	require.Equal(t, "CG-1811", out["ec2f9135"].IssueKey)
	require.NotNil(t, commented.Load(), "expected a comment on the existing issue")
	require.Equal(t, "CG-1811", *commented.Load())
}

func TestFindExistingIssue_shapes(t *testing.T) {
	tests := []struct {
		name   string
		record jiraadf.RecordSummary
		action string
		want   string
	}{
		{
			name:   "canonical handle",
			record: jiraadf.RecordSummary{"hash": "h1", "notify_ref_act": map[string]any{"issue_key": "OPS-1"}},
			action: "act",
			want:   "OPS-1",
		},
		{
			name:   "legacy response handle",
			record: jiraadf.RecordSummary{"hash": "h1", "response_act": map[string]any{"issue_key": "OPS-2"}},
			action: "act",
			want:   "OPS-2",
		},
		{
			name:   "batch envelope keyed by this record's hash",
			record: jiraadf.RecordSummary{"hash": "h1", "response_act": map[string]any{"h1": map[string]any{"issue_key": "OPS-3"}}},
			action: "act",
			want:   "OPS-3",
		},
		{
			name:   "batch envelope keyed by another alert's hash is not ours",
			record: jiraadf.RecordSummary{"hash": "h1", "response_act": map[string]any{"h2": map[string]any{"issue_key": "OPS-4"}}},
			action: "act",
			want:   "",
		},
		{
			name:   "unknown action falls back to any stored handle",
			record: jiraadf.RecordSummary{"hash": "h1", "response_act": map[string]any{"h1": map[string]any{"issue_key": "OPS-5"}}},
			action: "",
			want:   "OPS-5",
		},
		{
			name: "unknown action prefers the canonical prefix, then alphabetical",
			record: jiraadf.RecordSummary{
				"hash":           "h1",
				"response_bbb":   map[string]any{"issue_key": "OPS-6"},
				"notify_ref_zzz": map[string]any{"issue_key": "OPS-7"},
				"notify_ref_aaa": map[string]any{"issue_key": "OPS-8"},
			},
			action: "",
			want:   "OPS-8",
		},
		{
			name:   "wrong action name does not borrow another action's handle",
			record: jiraadf.RecordSummary{"hash": "h1", "response_other": map[string]any{"issue_key": "OPS-9"}},
			action: "act",
			want:   "",
		},
		{
			name:   "garbage handle",
			record: jiraadf.RecordSummary{"hash": "h1", "response_act": "not a map"},
			action: "act",
			want:   "",
		},
		{
			name: "1.x array still works",
			record: jiraadf.RecordSummary{"hash": "h1", "snooze_webhook_responses": []any{
				map[string]any{"action_name": "act", "content": map[string]any{"issue_key": "OPS-10"}},
			}},
			action: "act",
			want:   "OPS-10",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, findExistingIssue(tc.record, tc.action))
		})
	}
}
