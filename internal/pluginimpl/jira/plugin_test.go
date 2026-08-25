package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snoozeweb/snooze/internal/jirapriority"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func testMeta() map[string]any {
	return map[string]any{
		"jira_url":    "",
		"email":       "bot@example.com",
		"api_token":   "tok",
		"project_key": "OPS",
	}
}

// jiraScheme is the JIRA-default 5-entry priority scheme (ids 1..5, most
// severe first) served by newFakeJira.
var jiraScheme = []map[string]any{
	{"id": "1", "name": "Highest"},
	{"id": "2", "name": "High"},
	{"id": "3", "name": "Medium"},
	{"id": "4", "name": "Low"},
	{"id": "5", "name": "Lowest"},
}

// newFakeJira starts a JIRA stand-in that answers priority-scheme discovery
// with scheme (nil = jiraScheme) and routes everything else to h.
func newFakeJira(t *testing.T, scheme []map[string]any, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	if scheme == nil {
		scheme = jiraScheme
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{
					"issuetypes": []map[string]any{{
						"id":     "10001",
						"fields": map[string]any{"priority": map[string]any{"allowedValues": scheme}},
					}},
				}},
			})
		case "/rest/api/3/priority":
			_ = json.NewEncoder(w).Encode(scheme)
		default:
			h(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSendCreatesIssue(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := newFakeJira(t, nil, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
	})

	p := &Plugin{newClient: func(time.Duration) *http.Client { return srv.Client() }}
	meta := testMeta()
	meta["jira_url"] = srv.URL
	rec := snoozetypes.Record{Host: "db-01", Severity: "critical", Message: "down"}

	if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/rest/api/3/issue" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(gotAuth) < 6 || gotAuth[:6] != "Basic " {
		t.Fatalf("auth = %q", gotAuth)
	}
	raw := strings.TrimPrefix(gotAuth, "Basic ")
	dec, derr := base64.StdEncoding.DecodeString(raw)
	if derr != nil || string(dec) != "bot@example.com:tok" {
		t.Fatalf("auth creds = %q (decode err %v)", string(dec), derr)
	}
	fields, _ := gotBody["fields"].(map[string]any)
	if fields == nil {
		t.Fatalf("no fields in body: %v", gotBody)
	}
	proj, _ := fields["project"].(map[string]any)
	if proj["key"] != "OPS" {
		t.Fatalf("project = %v", fields["project"])
	}
	// severity=critical → second position of the live scheme → id 2, never a
	// localized name.
	prio, _ := fields["priority"].(map[string]any)
	if prio["id"] != "2" || prio["name"] != nil {
		t.Fatalf("priority = %v", fields["priority"])
	}
}

func TestSendCloseIsNoop(t *testing.T) {
	called := false
	srv := newFakeJira(t, nil, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})
	p := &Plugin{newClient: func(time.Duration) *http.Client { return srv.Client() }}
	meta := testMeta()
	meta["jira_url"] = srv.URL
	rec := snoozetypes.Record{Host: "db-01", State: "close"}
	if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if called {
		t.Fatal("close event must not POST to JIRA")
	}
}

func TestSendErrorOnNon201(t *testing.T) {
	srv := newFakeJira(t, nil, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":{"project":"required"}}`))
	})
	p := &Plugin{newClient: func(time.Duration) *http.Client { return srv.Client() }}
	meta := testMeta()
	meta["jira_url"] = srv.URL
	rec := snoozetypes.Record{Host: "db-01", Severity: "info", Message: "x"}
	if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err == nil {
		t.Fatal("expected error on HTTP 400")
	}
}

func TestConfigRequiresFields(t *testing.T) {
	if _, err := configFromMeta(map[string]any{"email": "x"}); err == nil {
		t.Fatal("expected error when jira_url/project_key missing")
	}
	if _, err := configFromMeta(map[string]any{"jira_url": "https://x", "email": "a@b", "project_key": "OPS"}); err == nil {
		t.Fatal("expected error when api_token missing")
	}
}

// The action's `summary` field overrides the issue title; a broken template
// falls back to the built-in one rather than dropping the notification.
func TestSummaryOverride(t *testing.T) {
	cases := []struct {
		name    string
		summary string
		want    string
	}{
		{name: "default", summary: "", want: "[critical] db-01 - down"},
		{name: "literal", summary: "Database down", want: "Database down"},
		{name: "template", summary: "{{ .Host }}: {{ .Message }}", want: "db-01: down"},
		{name: "broken template", summary: "{{ .Host", want: "[critical] db-01 - down"},
		{name: "blank template output", summary: "{{ .Process }}", want: "Snooze alert"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]any
			srv := newFakeJira(t, nil, func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &gotBody)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
			})

			p := &Plugin{newClient: func(time.Duration) *http.Client { return srv.Client() }}
			meta := testMeta()
			meta["jira_url"] = srv.URL
			if tc.summary != "" {
				meta["summary"] = tc.summary
			}
			rec := snoozetypes.Record{Host: "db-01", Severity: "critical", Message: "down"}
			if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err != nil {
				t.Fatalf("Send: %v", err)
			}
			fields, _ := gotBody["fields"].(map[string]any)
			if got, _ := fields["summary"].(string); got != tc.want {
				t.Fatalf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

// JIRA rejects summaries longer than 255 characters, so we clamp.
func TestSummaryClampedTo255(t *testing.T) {
	var gotBody map[string]any
	srv := newFakeJira(t, nil, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
	})

	p := &Plugin{newClient: func(time.Duration) *http.Client { return srv.Client() }}
	meta := testMeta()
	meta["jira_url"] = srv.URL
	rec := snoozetypes.Record{Host: "db-01", Severity: "critical", Message: strings.Repeat("x", 400)}
	if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	fields, _ := gotBody["fields"].(map[string]any)
	if got, _ := fields["summary"].(string); len(got) != 255 {
		t.Fatalf("summary length = %d, want 255", len(got))
	}
}

// frenchJiraScheme is a localized 4-entry scheme — the shape that made a
// hardcoded English priority name fail with `400 … priority`.
var frenchJiraScheme = []map[string]any{
	{"id": "1", "name": "Critique"},
	{"id": "2", "name": "Grave"},
	{"id": "3", "name": "Moyen"},
	{"id": "4", "name": "Faible"},
}

func TestPriorityResolution(t *testing.T) {
	cases := []struct {
		name     string
		scheme   []map[string]any
		severity string
		priority string // the action's Priority field
		want     any    // expected fields.priority, nil = key absent
	}{
		{
			name:     "severity placed positionally, localized site",
			scheme:   frenchJiraScheme,
			severity: "critical",
			want:     map[string]any{"id": "2"}, // Grave
		},
		{
			name:     "warning on a localized site",
			scheme:   frenchJiraScheme,
			severity: "warning",
			want:     map[string]any{"id": "3"}, // Moyen
		},
		{
			name:     "override by id",
			scheme:   frenchJiraScheme,
			severity: "warning",
			priority: "1",
			want:     map[string]any{"id": "1"},
		},
		{
			name:     "override by localized name",
			scheme:   frenchJiraScheme,
			severity: "warning",
			priority: "faible",
			want:     map[string]any{"id": "4"},
		},
		{
			name:     "override naming nothing is ignored, severity wins",
			scheme:   frenchJiraScheme,
			severity: "emergency",
			priority: "Highest", // English name, absent from this site
			want:     map[string]any{"id": "1"},
		},
		{
			name:     "unranked severity omits the field",
			scheme:   frenchJiraScheme,
			severity: "major",
			want:     nil,
		},
		{
			name:     "unranked severity with a resolvable override",
			scheme:   frenchJiraScheme,
			severity: "major",
			priority: "Moyen",
			want:     map[string]any{"id": "3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]any
			srv := newFakeJira(t, tc.scheme, func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &gotBody)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
			})
			p := &Plugin{newClient: func(time.Duration) *http.Client { return srv.Client() }}
			meta := testMeta()
			meta["jira_url"] = srv.URL
			if tc.priority != "" {
				meta["priority"] = tc.priority
			}
			rec := snoozetypes.Record{Host: "db-01", Severity: tc.severity, Message: "down"}
			if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err != nil {
				t.Fatalf("Send: %v", err)
			}
			fields, _ := gotBody["fields"].(map[string]any)
			got, present := fields["priority"]
			if tc.want == nil {
				if present {
					t.Fatalf("priority = %v, want absent", got)
				}
				return
			}
			want := tc.want.(map[string]any)
			gotMap, _ := got.(map[string]any)
			if gotMap == nil || gotMap["id"] != want["id"] {
				t.Fatalf("priority = %v, want %v", got, want)
			}
		})
	}
}

// The scheme is fetched once per action config, not once per notification.
func TestPrioritySchemeCached(t *testing.T) {
	var metaCalls, creates int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta":
			metaCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{
					"issuetypes": []map[string]any{{
						"id":     "10001",
						"fields": map[string]any{"priority": map[string]any{"allowedValues": frenchJiraScheme}},
					}},
				}},
			})
		default:
			creates++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
		}
	}))
	defer srv.Close()

	p := &Plugin{
		newClient:  func(time.Duration) *http.Client { return srv.Client() },
		priorities: jirapriority.NewCache(0),
	}
	meta := testMeta()
	meta["jira_url"] = srv.URL
	for i := 0; i < 4; i++ {
		rec := snoozetypes.Record{Host: "db-01", Severity: "warning", Message: "x"}
		if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	if creates != 4 {
		t.Fatalf("creates = %d, want 4", creates)
	}
	if metaCalls != 1 {
		t.Fatalf("createmeta calls = %d, want 1 (scheme must be cached)", metaCalls)
	}
}

// A rejected priority drops the cached scheme and retries once with a fresh one.
func TestPriorityRejectedRefetchesScheme(t *testing.T) {
	var metaCalls, creates int
	stale := []map[string]any{{"id": "77", "name": "Ancienne"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/issue/createmeta" {
			metaCalls++
			scheme := frenchJiraScheme
			if metaCalls == 1 {
				scheme = stale
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{
					"issuetypes": []map[string]any{{
						"id":     "10001",
						"fields": map[string]any{"priority": map[string]any{"allowedValues": scheme}},
					}},
				}},
			})
			return
		}
		creates++
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		prio, _ := body["fields"].(map[string]any)["priority"].(map[string]any)
		if creates == 1 {
			if prio["id"] != "77" {
				t.Errorf("first attempt priority = %v, want id 77", prio)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":{"priority":"La priorité sélectionnée n'est pas valide."}}`))
			return
		}
		if prio["id"] != "1" {
			t.Errorf("retry priority = %v, want id 1 from the refetched scheme", prio)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"CG-1"}`))
	}))
	defer srv.Close()

	p := &Plugin{
		newClient:  func(time.Duration) *http.Client { return srv.Client() },
		priorities: jirapriority.NewCache(0),
	}
	meta := testMeta()
	meta["jira_url"] = srv.URL
	rec := snoozetypes.Record{Host: "db-01", Severity: "emergency", Message: "down"}
	if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if creates != 2 || metaCalls != 2 {
		t.Fatalf("creates = %d, createmeta = %d, want 2 and 2", creates, metaCalls)
	}
}

// A site we cannot introspect still creates tickets: the action's priority name
// goes on the wire as before, and no priority is sent when it is blank.
func TestPrioritySchemeUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		priority string
		want     any
	}{
		{name: "configured name is used", priority: "Moyen", want: "Moyen"},
		{name: "blank omits the field", priority: "", want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/createmeta") ||
					r.URL.Path == "/rest/api/3/priority" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &gotBody)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
			}))
			defer srv.Close()

			p := &Plugin{newClient: func(time.Duration) *http.Client { return srv.Client() }}
			meta := testMeta()
			meta["jira_url"] = srv.URL
			if tc.priority != "" {
				meta["priority"] = tc.priority
			}
			rec := snoozetypes.Record{Host: "db-01", Severity: "critical", Message: "down"}
			if err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}); err != nil {
				t.Fatalf("Send: %v", err)
			}
			fields, _ := gotBody["fields"].(map[string]any)
			got, present := fields["priority"]
			if tc.want == nil {
				if present {
					t.Fatalf("priority = %v, want absent", got)
				}
				return
			}
			gotMap, _ := got.(map[string]any)
			if gotMap["name"] != tc.want {
				t.Fatalf("priority = %v, want name %v", got, tc.want)
			}
		})
	}
}
