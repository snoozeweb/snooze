// Package teams implements the "teams" Notifier plugin: it posts an alert to a
// Microsoft Teams channel via an Incoming Webhook / Workflows URL as an
// Adaptive Card. net/http only; no SDK.
//
// Microsoft is retiring the legacy O365 "MessageCard" connector; the supported
// path is a Workflows (Power Automate) webhook that accepts an Adaptive Card
// wrapped in an attachments envelope:
//
//	{"type":"message","attachments":[{"contentType":
//	  "application/vnd.microsoft.card.adaptive","content":{…AdaptiveCard…}}]}
//
// The title colour follows severity (Good / Warning / Attention). A resolved
// alert (rec.State == "close") is rendered green with a "✅ Resolved" prefix.
//
// This in-process notifier is the simple "fire a card at a webhook URL" path.
// The heavyweight, bidirectional snooze-teams daemon (threading + @-mention
// triage) is a separate binary and is unaffected.
package teams

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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
	plugins.Register("teams", metaYAML, factory)
}

// defaultTimeout matches the slack/webhook plugin baseline.
const defaultTimeout = 10 * time.Second

const (
	defaultTitleTmpl   = "{{ .Severity }} on {{ .Host }}"
	defaultMessageTmpl = "{{ .Message }}"
)

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the Microsoft Teams notifier. Send is safe for concurrent calls;
// the HTTP client is built per-call (same pattern as the slack/webhook plugins).
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder; overridable from tests so
	// httptest servers are used without proxy indirection.
	newClient func(timeout time.Duration) *http.Client
}

func (p *Plugin) Name() string                   { return "teams" }
func (p *Plugin) Metadata() plugins.Metadata     { return p.meta }
func (p *Plugin) Reload(_ context.Context) error { return nil }

func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Send renders the card and POSTs it to the configured webhook URL.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("teams: config: %w", err)
	}
	title, err := renderTemplate(cfg.Title, rec)
	if err != nil {
		return fmt.Errorf("teams: render title: %w", err)
	}
	text, err := renderTemplate(cfg.Message, rec)
	if err != nil {
		return fmt.Errorf("teams: render message: %w", err)
	}
	body, err := buildPayload(rec, title, text)
	if err != nil {
		return fmt.Errorf("teams: build payload: %w", err)
	}
	return p.post(ctx, cfg, body)
}

func (p *Plugin) post(ctx context.Context, cfg config, body []byte) error {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("teams: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := p.newClient(timeout).Do(req)
	if err != nil {
		return fmt.Errorf("teams: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("teams: HTTP %d", resp.StatusCode)
	}
	return nil
}

func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// Compile-time proof that *Plugin satisfies the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

type config struct {
	WebhookURL string
	Title      string
	Message    string
	Timeout    time.Duration
}

func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		Title:   defaultTitleTmpl,
		Message: defaultMessageTmpl,
		Timeout: defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("webhook_url is required")
	}
	cfg.WebhookURL = metaString(meta, "webhook_url")
	if t := metaString(meta, "title"); t != "" {
		cfg.Title = t
	}
	if m := metaString(meta, "message"); m != "" {
		cfg.Message = m
	}
	if d, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = d
	}
	if cfg.WebhookURL == "" {
		return cfg, fmt.Errorf("webhook_url is required")
	}
	return cfg, nil
}

func metaString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
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
// Rendering
// ---------------------------------------------------------------------------

func renderTemplate(tmpl string, rec snoozetypes.Record) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}
	t, err := template.New("teams").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// severityToken maps a Snooze severity to an Adaptive Card colour token.
func severityToken(severity string, resolved bool) string {
	if resolved {
		return "Good"
	}
	switch strings.ToLower(severity) {
	case "info", "notice", "debug":
		return "Good"
	case "warning", "warn":
		return "Warning"
	default:
		return "Attention"
	}
}

// ---------------------------------------------------------------------------
// Adaptive Card payload
// ---------------------------------------------------------------------------

type textBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Weight string `json:"weight,omitempty"`
	Size   string `json:"size,omitempty"`
	Color  string `json:"color,omitempty"`
	Wrap   bool   `json:"wrap"`
}

type fact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

type factSet struct {
	Type  string `json:"type"`
	Facts []fact `json:"facts"`
}

type adaptiveCard struct {
	Schema  string `json:"$schema"`
	Type    string `json:"type"`
	Version string `json:"version"`
	Body    []any  `json:"body"`
}

type attachment struct {
	ContentType string       `json:"contentType"`
	Content     adaptiveCard `json:"content"`
}

type teamsMessage struct {
	Type        string       `json:"type"`
	Attachments []attachment `json:"attachments"`
}

func buildPayload(rec snoozetypes.Record, title, text string) ([]byte, error) {
	resolved := rec.State == "close"
	color := severityToken(rec.Severity, resolved)

	displayTitle := title
	if resolved && !strings.HasPrefix(displayTitle, "✅") {
		displayTitle = "✅ Resolved: " + displayTitle
	}

	card := adaptiveCard{
		Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
		Type:    "AdaptiveCard",
		Version: "1.4",
		Body: []any{
			textBlock{Type: "TextBlock", Text: displayTitle, Weight: "Bolder", Size: "Medium", Color: color, Wrap: true},
			textBlock{Type: "TextBlock", Text: text, Wrap: true},
			factSet{Type: "FactSet", Facts: []fact{
				{Title: "Host", Value: rec.Host},
				{Title: "Severity", Value: rec.Severity},
				{Title: "Source", Value: rec.Source},
				{Title: "State", Value: rec.State},
			}},
		},
	}
	msg := teamsMessage{
		Type: "message",
		Attachments: []attachment{{
			ContentType: "application/vnd.microsoft.card.adaptive",
			Content:     card,
		}},
	}
	return json.Marshal(msg)
}
