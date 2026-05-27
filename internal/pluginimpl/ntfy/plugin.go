// Package ntfy implements the "ntfy" Notifier plugin: it delivers alert
// notifications to a self-hostable ntfy server (https://ntfy.sh) by POSTing
// the rendered message body to {server}/{topic}.
//
// ntfy encodes all metadata as HTTP request headers, so no JSON body is
// required — the body is the human-readable message text.  Optional auth via
// Bearer token or HTTP Basic is supported.  Severity is mapped to ntfy's
// 1-5 priority scale and a set of emoji tags automatically unless both are
// overridden by the operator.
//
// The plugin owns no database collection.  PostInit stores the host; Reload is
// a no-op.
package ntfy

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("ntfy", metaYAML, factory)
}

// defaultTimeout is the per-request deadline applied when the operator does
// not configure a timeout.
const defaultTimeout = 10 * time.Second

// defaultServer is the public ntfy instance used when the operator leaves the
// server field blank.
const defaultServer = "https://ntfy.sh"

// defaultTitleTemplate is the Go text/template used for the ntfy Title header
// when the operator does not supply their own.
const defaultTitleTemplate = "{{ .Severity }} on {{ .Host }}"

// defaultMessageTemplate is the Go text/template used for the request body
// when the operator does not supply their own.
const defaultMessageTemplate = "{{ .Message }}"

// severityPriority maps Snooze severity keywords to ntfy priority (1..5).
// info→2, warning→4, error→4, critical/emergency→5.  debug/notice fall back
// to the info bucket (priority 2).
var severityPriority = map[string]string{
	"emergency": "5",
	"critical":  "5",
	"error":     "4",
	"err":       "4",
	"warning":   "4",
	"notice":    "2",
	"info":      "2",
	"debug":     "2",
}

// severityTags maps Snooze severity keywords to a comma-separated list of ntfy
// tag emoji names.
var severityTags = map[string]string{
	"emergency": "rotating_light",
	"critical":  "rotating_light",
	"error":     "warning",
	"err":       "warning",
	"warning":   "warning",
	"notice":    "information_source",
	"info":      "information_source",
	"debug":     "information_source",
}

// Config holds the decoded per-action knobs supplied by the operator via the
// action_form (stored in NotificationPayload.Meta).
type Config struct {
	Server   string
	Topic    string
	Title    string // Go template string
	Message  string // Go template string
	Priority string // "auto" | "1".."5"
	Tags     string // comma-separated list of ntfy tag names; empty = derive from severity
	Token    string // Bearer auth token
	Username string // Basic auth username
	Password string // Basic auth password
	Click    string // optional click URL
	Timeout  time.Duration
}

// Plugin is the ntfy Notifier.
//
// Concurrency: Send is safe for concurrent calls.  A new http.Client is
// constructed per-call because timeout is per-action.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is overridable from tests so httptest can intercept.
	newClient func(timeout time.Duration) *http.Client
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "ntfy" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host.  There is no database collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin has no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send renders the notification and POSTs it to the configured ntfy topic.
// It returns an error on any non-2xx response.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("ntfy: config: %w", err)
	}

	// Render the title from the template.
	titleTmpl := cfg.Title
	if titleTmpl == "" {
		titleTmpl = defaultTitleTemplate
	}
	title, err := renderTemplate("title", titleTmpl, rec)
	if err != nil {
		return fmt.Errorf("ntfy: render title: %w", err)
	}

	// Render the message body from the template.
	msgTmpl := cfg.Message
	if msgTmpl == "" {
		msgTmpl = defaultMessageTemplate
	}
	body, err := renderTemplate("message", msgTmpl, rec)
	if err != nil {
		return fmt.Errorf("ntfy: render message: %w", err)
	}

	// Resolve the effective priority.
	priority := cfg.Priority
	if priority == "" || priority == "auto" {
		priority = derivePriority(rec.Severity)
	}

	// Resolve the effective tags.
	tags := cfg.Tags
	if tags == "" {
		tags = deriveTags(rec.Severity)
	}

	// Build the target URL: {server}/{topic}.  Strip any trailing slash from
	// the server URL to avoid a double slash.
	server := strings.TrimRight(cfg.Server, "/")
	targetURL := server + "/" + cfg.Topic

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, targetURL, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("ntfy: build request: %w", err)
	}

	// ntfy uses plain UTF-8 text as the body.
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	if title != "" {
		req.Header.Set("Title", title)
	}
	if priority != "" {
		req.Header.Set("Priority", priority)
	}
	if tags != "" {
		req.Header.Set("Tags", tags)
	}
	if cfg.Click != "" {
		req.Header.Set("Click", cfg.Click)
	}

	// Auth: Bearer takes precedence over Basic.
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	} else if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	client := p.newClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Drain a bounded amount to allow connection reuse and to surface the
	// error body in non-2xx diagnostics.
	preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}

	return nil
}

// factory builds the plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// defaultClient returns a plain http.Client with the given timeout.
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// configFromMeta decodes Config from NotificationPayload.Meta.  Missing fields
// fall back to sensible defaults; only a missing topic is fatal.
func configFromMeta(m map[string]any) (Config, error) {
	cfg := Config{
		Server:   defaultServer,
		Priority: "auto",
		Timeout:  defaultTimeout,
	}
	if m == nil {
		return cfg, fmt.Errorf("topic is required")
	}

	if v, ok := m["server"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.Server = strings.TrimSpace(v)
	}
	if v, ok := m["topic"].(string); ok {
		cfg.Topic = strings.TrimSpace(v)
	}
	if cfg.Topic == "" {
		return cfg, fmt.Errorf("topic is required")
	}
	if v, ok := m["title"].(string); ok {
		cfg.Title = v
	}
	if v, ok := m["message"].(string); ok {
		cfg.Message = v
	}
	if v, ok := m["priority"].(string); ok && v != "" {
		cfg.Priority = v
	}
	if v, ok := m["tags"].(string); ok {
		cfg.Tags = v
	}
	if v, ok := m["token"].(string); ok {
		cfg.Token = v
	}
	if v, ok := m["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := m["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := m["click"].(string); ok {
		cfg.Click = v
	}
	if t, ok := parseTimeout(m["timeout"]); ok {
		cfg.Timeout = t
	}

	return cfg, nil
}

// derivePriority returns the ntfy priority string for a given Snooze severity.
// Unknown severities fall back to "2" (low).
func derivePriority(severity string) string {
	if p, ok := severityPriority[strings.ToLower(severity)]; ok {
		return p
	}
	return "2"
}

// deriveTags returns the ntfy tags string for a given Snooze severity.
// Unknown severities fall back to the info tag.
func deriveTags(severity string) string {
	if t, ok := severityTags[strings.ToLower(severity)]; ok {
		return t
	}
	return "information_source"
}

// renderTemplate executes a Go text/template over the record fields directly.
// The template data is the record struct itself so operators write
// {{ .Severity }}, {{ .Host }}, {{ .Message }}, etc.
func renderTemplate(name, tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	if !strings.Contains(tmpl, "{{") {
		// Fast path: literal string with no template directives.
		return tmpl, nil
	}
	t, err := template.New(name).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// parseTimeout accepts a duration string, integer seconds, or time.Duration.
func parseTimeout(v any) (time.Duration, bool) {
	switch x := v.(type) {
	case time.Duration:
		if x > 0 {
			return x, true
		}
	case string:
		d, err := time.ParseDuration(x)
		if err == nil && d > 0 {
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

// truncate returns at most n bytes of b as a string, appending "..." when the
// input was longer.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// Compile-time proof we satisfy the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)
