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

func TestSendCreatesIssue(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
	}))
	defer srv.Close()

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
	prio, _ := fields["priority"].(map[string]any)
	if prio["name"] != "High" {
		t.Fatalf("priority = %v", fields["priority"])
	}
}

func TestSendCloseIsNoop(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":{"project":"required"}}`))
	}))
	defer srv.Close()
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
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &gotBody)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
			}))
			defer srv.Close()

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
	}))
	defer srv.Close()

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
