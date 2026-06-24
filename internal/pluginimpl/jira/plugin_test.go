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
