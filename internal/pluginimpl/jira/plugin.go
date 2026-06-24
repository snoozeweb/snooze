// Package jira implements the "jira" Notifier plugin: a fire-and-forget create
// of a JIRA Cloud issue via the REST API v3 (POST /rest/api/3/issue), using
// HTTP Basic auth (Atlassian email + API token) and an ADF-formatted
// description.
//
// Fire-and-forget: each notification attempts a create. Deduplication,
// re-escalation comments, reopen, and auto-close on resolution are NOT handled
// here — those stateful/bidirectional features live in the optional snooze-jira
// daemon (internal/components/jira).
package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	_ "embed"

	"github.com/snoozeweb/snooze/internal/jiraadf"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("jira", metaYAML, factory)
}

const defaultTimeout = 10 * time.Second
const maxResponseBytes = 4 << 10

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the JIRA notifier. Send is safe for concurrent calls; the HTTP
// client is built per-call (same pattern as servicenow/webhook).
type Plugin struct {
	meta      plugins.Metadata
	host      plugins.Host
	newClient func(timeout time.Duration) *http.Client
}

func (p *Plugin) Name() string                   { return "jira" }
func (p *Plugin) Metadata() plugins.Metadata     { return p.meta }
func (p *Plugin) Reload(_ context.Context) error { return nil }

func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Send creates a JIRA issue for a firing record. Close events are a no-op —
// auto-close is the snooze-jira daemon's responsibility.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	if rec.State == "close" {
		return nil
	}
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("jira: config: %w", err)
	}
	return p.create(ctx, cfg, rec)
}

func (p *Plugin) create(ctx context.Context, cfg config, rec snoozetypes.Record) error {
	summary, err := renderTemplate(cfg.Summary, rec)
	if err != nil {
		return fmt.Errorf("jira: render summary: %w", err)
	}

	fields := map[string]any{
		"project":     map[string]any{"key": cfg.ProjectKey},
		"issuetype":   map[string]any{"name": cfg.IssueType},
		"summary":     summary,
		"description": p.description(cfg, rec),
	}
	if prio := priorityFor(cfg, rec.Severity); prio != "" {
		fields["priority"] = map[string]any{"name": prio}
	}
	if len(cfg.Labels) > 0 {
		fields["labels"] = cfg.Labels
	}

	body, err := json.Marshal(map[string]any{"fields": fields})
	if err != nil {
		return fmt.Errorf("jira: marshal body: %w", err)
	}

	issueURL := cfg.JiraURL + "/rest/api/3/issue"
	req, err := p.newRequest(ctx, cfg, http.MethodPost, issueURL, body)
	if err != nil {
		return fmt.Errorf("jira: build create request: %w", err)
	}

	resp, err := p.newClient(cfg.Timeout).Do(req)
	if err != nil {
		return fmt.Errorf("jira: create request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("jira: create: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// description renders the templated description when provided, else builds the
// structured default via the shared jiraadf package.
func (p *Plugin) description(cfg config, rec snoozetypes.Record) jiraadf.ADF {
	if strings.TrimSpace(cfg.Description) != "" {
		if rendered, err := renderTemplate(cfg.Description, rec); err == nil {
			return jiraadf.TextADF(rendered)
		} else if p.host != nil {
			if lg := p.host.Logger(); lg != nil {
				lg.Warn("jira: render description template failed, using default", "error", err)
			}
		}
	}
	ts := ""
	if !rec.Timestamp.IsZero() {
		ts = rec.Timestamp.Format(time.RFC3339)
	}
	return jiraadf.BuildDescriptionADF(jiraadf.RecordSummary{
		"host":      rec.Host,
		"source":    rec.Source,
		"process":   rec.Process,
		"severity":  rec.Severity,
		"timestamp": ts,
		"message":   rec.Message,
		"hash":      rec.Hash,
	}, "")
}

func (p *Plugin) newRequest(ctx context.Context, cfg config, method, rawURL string, body []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}
	creds := cfg.Email + ":" + cfg.APIToken
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(creds)))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

var _ plugins.Notifier = (*Plugin)(nil)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

type config struct {
	JiraURL     string
	Email       string
	APIToken    string
	ProjectKey  string
	IssueType   string
	Priority    string
	Summary     string
	Description string
	Labels      []string
	Timeout     time.Duration
}

func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		IssueType: "Task",
		Summary:   "[{{ .Severity }}] {{ .Host }} - {{ .Message }}",
		Labels:    []string{"snooze"},
		Timeout:   defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("jira_url is required")
	}
	cfg.JiraURL = strings.TrimRight(metaString(meta, "jira_url"), "/")
	cfg.Email = metaString(meta, "email")
	cfg.APIToken = metaString(meta, "api_token")
	cfg.ProjectKey = metaString(meta, "project_key")

	if cfg.JiraURL == "" {
		return cfg, fmt.Errorf("jira_url is required")
	}
	if cfg.Email == "" {
		return cfg, fmt.Errorf("email is required")
	}
	if cfg.APIToken == "" {
		return cfg, fmt.Errorf("api_token is required")
	}
	if cfg.ProjectKey == "" {
		return cfg, fmt.Errorf("project_key is required")
	}
	if v := metaString(meta, "issue_type"); v != "" {
		cfg.IssueType = v
	}
	cfg.Priority = metaString(meta, "priority")
	if v := metaString(meta, "summary"); v != "" {
		cfg.Summary = v
	}
	cfg.Description = metaString(meta, "description")
	if v := metaString(meta, "labels"); v != "" {
		cfg.Labels = splitCSV(v)
	}
	if to, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = to
	}
	return cfg, nil
}

func metaString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseTimeout(v any) (time.Duration, bool) {
	switch x := v.(type) {
	case time.Duration:
		if x > 0 {
			return x, true
		}
	case string:
		if d, err := time.ParseDuration(x); err == nil && d > 0 {
			return d, true
		}
	case int:
		if x > 0 {
			return time.Duration(x) * time.Second, true
		}
	case int64:
		if x > 0 {
			return time.Duration(x) * time.Second, true
		}
	case float64:
		if x > 0 {
			return time.Duration(x * float64(time.Second)), true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// severityPriority mirrors the snooze-jira daemon's default priority_mapping.
var severityPriority = map[string]string{
	"emergency": "Critical",
	"critical":  "High",
	"warning":   "Medium",
	"minor":     "Low",
	"info":      "Lowest",
}

func priorityFor(cfg config, severity string) string {
	if p, ok := severityPriority[strings.ToLower(severity)]; ok {
		return p
	}
	return cfg.Priority
}

func renderTemplate(tmpl string, rec snoozetypes.Record) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}
	t, err := template.New("jira").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
