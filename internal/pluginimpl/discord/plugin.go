// Package discord implements the "discord" Notifier plugin: it posts an alert
// to a Discord channel via an Incoming Webhook as either a rich embed or a
// plain text message.
//
// The plugin uses net/http only; no Discord SDK or other third-party dependency
// is required. Per-call configuration is carried in
// plugins.NotificationPayload.Meta (the action_form values the notification
// dispatcher attaches). The plugin owns no database state: PostInit just stores
// the host, and Reload is a no-op.
//
// # Discord webhook contract
//
// Discord webhooks accept a JSON POST to the webhook URL. On success they
// respond with 204 No Content (the plugin also accepts 200 for compatibility
// with proxies). Non-2xx responses are surfaced as an error that includes the
// HTTP status and a truncated response body for diagnostics.
package discord

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
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
	plugins.Register("discord", metaYAML, factory)
}

// defaultTimeout is used when no timeout is configured on the action form.
const defaultTimeout = 10 * time.Second

// defaultMessageTemplate is the Go text/template applied to the record when the
// "message" action_form field is absent or empty.
const defaultMessageTemplate = "**{{ .Severity }}** on {{ .Host }}: {{ .Message }}"

// maxResponseBytes caps the body read from non-2xx responses for error messages.
const maxResponseBytes = 4 << 10

// discordMessage is the top-level JSON payload sent to a Discord webhook.
// Fields are omitted when empty so optional knobs don't appear in the wire
// format unless configured.
type discordMessage struct {
	Content   string         `json:"content,omitempty"`
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []discordEmbed `json:"embeds,omitempty"`
}

// discordEmbed represents a single Discord embed object.
type discordEmbed struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color,omitempty"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
}

// discordEmbedField is a name/value pair shown as a field row inside an embed.
type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// Plugin is the Discord Notifier implementation.
//
// Concurrency: Send is safe for concurrent calls. The HTTP client is built
// per-call from the newClient factory so per-action timeout knobs are honoured
// without shared state.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. It is overridable from tests so
	// httptest servers can intercept requests without TLS setup.
	newClient func(timeout time.Duration) *http.Client
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "discord" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host. No database state is needed.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin has no cached state to refresh.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send dispatches a single Discord webhook request rendered from the
// payload's Meta map. The Meta map carries the action_form fields as supplied
// by the notification configuration.
//
// Success is defined as HTTP 204 (Discord's canonical response) or HTTP 200
// (proxy compatibility). Any other status code is returned as an error
// containing the status and a truncated response body.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("discord: config: %w", err)
	}

	msg, err := buildMessage(cfg, rec)
	if err != nil {
		return fmt.Errorf("discord: build message: %w", err)
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("discord: marshal message: %w", err)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := p.newClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Drain a bounded amount so the connection can be reused by the transport.
	// The preview is surfaced in the error message for non-2xx responses.
	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	// Discord returns 204 on success; also accept 200 from proxies.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// factory builds the plugin instance and wires the default HTTP client.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// defaultClient returns an http.Client with the given timeout.
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// config holds the decoded per-action knobs.
type config struct {
	WebhookURL string
	Username   string
	AvatarURL  string
	Message    string
	UseEmbed   bool
	Timeout    time.Duration
}

// configFromMeta decodes config from NotificationPayload.Meta. Missing fields
// fall back to sensible defaults; webhook_url is required.
func configFromMeta(m map[string]any) (config, error) {
	cfg := config{
		Message:  defaultMessageTemplate,
		UseEmbed: true, // default: send a rich embed
		Timeout:  defaultTimeout,
	}
	if m == nil {
		return cfg, fmt.Errorf("webhook_url is required")
	}

	if v, ok := m["webhook_url"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.WebhookURL = strings.TrimSpace(v)
	} else {
		return cfg, fmt.Errorf("webhook_url is required")
	}

	if v, ok := m["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := m["avatar_url"].(string); ok {
		cfg.AvatarURL = v
	}
	if v, ok := m["message"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.Message = v
	}
	if v, ok := m["use_embed"].(bool); ok {
		cfg.UseEmbed = v
	}
	if t, ok := parseTimeout(m["timeout"]); ok {
		cfg.Timeout = t
	}

	return cfg, nil
}

// buildMessage renders the discord payload for the given record.
func buildMessage(cfg config, rec snoozetypes.Record) (discordMessage, error) {
	text, err := renderTemplate("message", cfg.Message, rec)
	if err != nil {
		return discordMessage{}, err
	}

	msg := discordMessage{
		Username:  cfg.Username,
		AvatarURL: cfg.AvatarURL,
	}

	if cfg.UseEmbed {
		// Title: "db-1.example.com | warning" — concise identifier for the embed.
		title := rec.Host
		if rec.Source != "" {
			title = rec.Host + " | " + rec.Source
		}

		// Determine color from severity; for a resolved alert use the resolved color.
		colorKey := rec.Severity
		if rec.State == "close" {
			colorKey = "close"
		}

		embed := discordEmbed{
			Title:       title,
			Description: text,
			Color:       severityColor(colorKey),
			Timestamp:   rec.Timestamp.UTC().Format(time.RFC3339),
		}

		// Add a few useful fields for at-a-glance triage.
		if rec.Source != "" {
			embed.Fields = append(embed.Fields, discordEmbedField{
				Name:   "Source",
				Value:  rec.Source,
				Inline: true,
			})
		}
		if rec.Severity != "" {
			embed.Fields = append(embed.Fields, discordEmbedField{
				Name:   "Severity",
				Value:  rec.Severity,
				Inline: true,
			})
		}

		msg.Embeds = []discordEmbed{embed}
	} else {
		msg.Content = text
	}

	return msg, nil
}

// severityColor maps a Snooze severity keyword onto a Discord embed decimal
// color integer.
//
//	info / notice / debug / unknown → green  0x36a64f
//	warning                         → amber  0xdaa038
//	error / err / critical /
//	  emergency                     → red    0xd00000
//	close (resolved)                → teal   0x2eb886
func severityColor(severity string) int {
	switch strings.ToLower(severity) {
	case "warning":
		return 0xdaa038
	case "error", "err", "critical", "emergency":
		return 0xd00000
	case "close":
		return 0x2eb886
	default: // info, notice, debug, ""
		return 0x36a64f
	}
}

// renderTemplate executes a Go text/template over the record fields. The
// template data is a flat map of record fields so authors can write
// {{ .Severity }}, {{ .Host }}, etc. without a wrapping struct layer.
func renderTemplate(name, tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	t, err := template.New(name).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	data := map[string]any{
		"UID":       rec.UID,
		"Host":      rec.Host,
		"Source":    rec.Source,
		"Process":   rec.Process,
		"Severity":  rec.Severity,
		"Message":   rec.Message,
		"State":     rec.State,
		"Timestamp": rec.Timestamp,
		"Tags":      rec.Tags,
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// parseTimeout accepts a duration string, an int (seconds), an int64
// (seconds), or a float64 (seconds). Returns (0, false) for anything else.
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

// truncate returns at most n bytes of b as a string, appending "..." when
// the input was longer.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// Compile-time proof that Plugin satisfies the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)
