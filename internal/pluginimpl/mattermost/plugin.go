// Package mattermost implements the "mattermost" Notifier plugin: it posts an
// alert to a Mattermost channel via an Incoming Webhook. Mattermost incoming
// webhooks are Slack-attachment compatible, so the payload mirrors Slack's
// webhook mode — a top-level text plus a single severity-coloured attachment.
// net/http only; no SDK.
//
// The attachment colour follows severity (#36a64f / #daa038 / #d00000). A
// resolved alert (rec.State == "close") is green with a "✅ Resolved" prefix.
//
// This in-process notifier is the simple "fire a message at a webhook URL"
// path; the bidirectional snooze-mattermost daemon is a separate binary.
package mattermost

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
	plugins.Register("mattermost", metaYAML, factory)
}

const defaultTimeout = 10 * time.Second

const defaultMessageTmpl = "*{{ .Severity }}* on `{{ .Host }}`: {{ .Message }}"

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the Mattermost notifier. Send is safe for concurrent calls; the
// HTTP client is built per-call (same pattern as the slack/webhook plugins).
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	newClient func(timeout time.Duration) *http.Client
}

func (p *Plugin) Name() string                   { return "mattermost" }
func (p *Plugin) Metadata() plugins.Metadata     { return p.meta }
func (p *Plugin) Reload(_ context.Context) error { return nil }

func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("mattermost: config: %w", err)
	}
	msg, err := renderMessage(cfg.Message, rec)
	if err != nil {
		return fmt.Errorf("mattermost: render message: %w", err)
	}
	body, err := buildPayload(cfg, rec, msg)
	if err != nil {
		return fmt.Errorf("mattermost: build payload: %w", err)
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
		return fmt.Errorf("mattermost: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := p.newClient(timeout).Do(req)
	if err != nil {
		return fmt.Errorf("mattermost: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mattermost: HTTP %d", resp.StatusCode)
	}
	return nil
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
	WebhookURL string
	Channel    string
	Username   string
	IconURL    string
	Message    string
	Timeout    time.Duration
}

func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		Message: defaultMessageTmpl,
		Timeout: defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("webhook_url is required")
	}
	cfg.WebhookURL = metaString(meta, "webhook_url")
	cfg.Channel = metaString(meta, "channel")
	cfg.Username = metaString(meta, "username")
	cfg.IconURL = metaString(meta, "icon_url")
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
// Rendering + payload
// ---------------------------------------------------------------------------

func renderMessage(tmpl string, rec snoozetypes.Record) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}
	t, err := template.New("mattermost").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// severityColor maps a Snooze severity to a Mattermost attachment hex colour.
func severityColor(severity string, resolved bool) string {
	if resolved {
		return "#36a64f"
	}
	switch strings.ToLower(severity) {
	case "info", "notice", "debug":
		return "#36a64f"
	case "warning", "warn":
		return "#daa038"
	default:
		return "#d00000"
	}
}

type mmAttachment struct {
	Color string `json:"color,omitempty"`
	Text  string `json:"text,omitempty"`
}

type mmPayload struct {
	Text        string         `json:"text,omitempty"`
	Channel     string         `json:"channel,omitempty"`
	Username    string         `json:"username,omitempty"`
	IconURL     string         `json:"icon_url,omitempty"`
	Attachments []mmAttachment `json:"attachments,omitempty"`
}

func buildPayload(cfg config, rec snoozetypes.Record, msg string) ([]byte, error) {
	resolved := rec.State == "close"
	color := severityColor(rec.Severity, resolved)

	display := msg
	if resolved && !strings.HasPrefix(display, "✅") {
		display = "✅ Resolved: " + msg
	}

	p := mmPayload{
		Channel:     cfg.Channel,
		Username:    cfg.Username,
		IconURL:     cfg.IconURL,
		Attachments: []mmAttachment{{Color: color, Text: display}},
	}
	return json.Marshal(p)
}
