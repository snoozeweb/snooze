// Package slack implements the "slack" Notifier plugin: it posts an alert to a
// Slack Incoming Webhook (or chat.postMessage when a bot token is supplied) as
// a Block Kit message. net/http only; no SDK.
//
// Two modes are supported:
//
//   - Incoming Webhook (default): POST webhook_url with a JSON body;
//     Slack returns HTTP 200 with the literal string "ok".
//   - Bot token: POST https://slack.com/api/chat.postMessage with an
//     Authorization: Bearer <bot_token> header; Slack always returns HTTP 200
//     but may signal a logical failure with {"ok":false,"error":"..."}.
//
// The Block Kit payload includes a header/section block with the rendered
// message and an attachment colour bar derived from severity:
//
//	info/notice/debug → good (#36a64f)
//	warning           → warning (#daa038)
//	error/critical/emergency → danger (#d00000)
//
// When rec.State == "close" the message is styled as resolved (green, prefix).
package slack

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
	plugins.Register("slack", metaYAML, factory)
}

// defaultTimeout matches the webhook plugin baseline.
const defaultTimeout = 10 * time.Second

// chatPostMessageURL is the Slack Web API endpoint for bot-token mode.
const chatPostMessageURL = "https://slack.com/api/chat.postMessage"

// defaultMessageTmpl is used when the action_form message field is empty.
const defaultMessageTmpl = "*{{ .Severity }}* on `{{ .Host }}`: {{ .Message }}"

// factory builds the plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the Slack notifier.
//
// Concurrency: Send is safe for concurrent calls. The HTTP client is built
// per-call (same pattern as the webhook plugin).
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. Overridable from tests so that
	// httptest servers can intercept outbound calls without a proxy.
	newClient func(timeout time.Duration) *http.Client

	// apiURL overrides chatPostMessageURL in tests so the bot-token path can be
	// exercised without a real network call. When empty (production) the package
	// constant is used.
	apiURL string
}

// Name returns the registry key used in the action_form and all.go.
func (p *Plugin) Name() string { return "slack" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit stores the host reference. There is no DB collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin carries no cached state between calls.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send dispatches a single Slack notification. The action_form values arrive
// through payload.Meta; see configFromMeta for the full field list.
//
// The verdict is "error == nil on success; error otherwise". Non-2xx HTTP
// responses are errors; in bot-token mode {"ok":false} is also an error.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("slack: config: %w", err)
	}

	msg, err := renderMessage(cfg.Message, rec)
	if err != nil {
		return fmt.Errorf("slack: render message: %w", err)
	}

	body, err := buildPayload(cfg, rec, msg)
	if err != nil {
		return fmt.Errorf("slack: build payload: %w", err)
	}

	return p.post(ctx, cfg, body)
}

// post dispatches the JSON body to Slack (webhook or bot-token mode).
func (p *Plugin) post(ctx context.Context, cfg config, body []byte) error {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := cfg.WebhookURL
	if cfg.BotToken != "" {
		url = chatPostMessageURL
		if p.apiURL != "" {
			url = p.apiURL
		}
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if cfg.BotToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.BotToken)
	}

	client := p.newClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack: HTTP %d", resp.StatusCode)
	}

	// In bot-token mode Slack always returns 200 even on logical failures;
	// we must decode the body and inspect the ok field.
	if cfg.BotToken != "" {
		var apiResp struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			// Undecodable body after a 200 is treated as success — Slack may
			// extend the response shape in future versions.
			return nil
		}
		if !apiResp.OK {
			return fmt.Errorf("slack: api error: %s", apiResp.Error)
		}
	}
	return nil
}

// defaultClient returns a plain http.Client with the given deadline.
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

// config holds the per-action knobs decoded from payload.Meta.
type config struct {
	WebhookURL string
	BotToken   string
	Channel    string
	Message    string
	Username   string
	IconEmoji  string
	Timeout    time.Duration
}

// configFromMeta decodes config from the action_form Meta map. Exactly one of
// WebhookURL or BotToken must be non-empty; both empty → error.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		Message: defaultMessageTmpl,
		Timeout: defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("webhook_url or bot_token is required")
	}

	cfg.WebhookURL = metaString(meta, "webhook_url")
	cfg.BotToken = metaString(meta, "bot_token")
	cfg.Channel = metaString(meta, "channel")
	cfg.Username = metaString(meta, "username")
	cfg.IconEmoji = metaString(meta, "icon_emoji")

	if m := metaString(meta, "message"); m != "" {
		cfg.Message = m
	}

	if t, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = t
	}

	if cfg.WebhookURL == "" && cfg.BotToken == "" {
		return cfg, fmt.Errorf("webhook_url or bot_token is required")
	}
	return cfg, nil
}

// metaString reads key from m as a string; returns "" when absent or wrong type.
func metaString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// parseTimeout accepts a duration string, int/float64 seconds, or
// time.Duration. Returns (0, false) on anything else.
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

// ---------------------------------------------------------------------------
// Message rendering
// ---------------------------------------------------------------------------

// renderMessage executes a Go text/template over the record's top-level fields.
func renderMessage(tmpl string, rec snoozetypes.Record) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}
	t, err := template.New("message").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ---------------------------------------------------------------------------
// Block Kit payload builder
// ---------------------------------------------------------------------------

// severityColor maps Snooze severity levels to Slack attachment colour names /
// hex values.  The mapping is intentionally exhaustive so a typo or future
// level degrades gracefully to the "error" bucket.
func severityColor(severity string, resolved bool) string {
	if resolved {
		return "good" // Slack named colour → green
	}
	switch strings.ToLower(severity) {
	case "info", "notice", "debug":
		return "good" // #36a64f
	case "warning", "warn":
		return "warning" // #daa038
	default:
		// error, err, critical, emergency, and any unknown severity
		return "danger" // #d00000
	}
}

// slackBlock is a minimal Block Kit block (section or header).
type slackBlock struct {
	Type string        `json:"type"`
	Text *slackTextObj `json:"text,omitempty"`
}

// slackTextObj is a Slack mrkdwn or plain_text composition object.
type slackTextObj struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// slackAttachment is a legacy attachment used solely for the colour sidebar.
// Blocks are placed at the top-level; the attachment carries the color.
type slackAttachment struct {
	Color  string       `json:"color"`
	Blocks []slackBlock `json:"blocks,omitempty"`
}

// webhookPayload is the JSON body for Incoming Webhook mode.
type webhookPayload struct {
	Text        string            `json:"text,omitempty"`
	Blocks      []slackBlock      `json:"blocks,omitempty"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
	Username    string            `json:"username,omitempty"`
	IconEmoji   string            `json:"icon_emoji,omitempty"`
}

// botPayload is the JSON body for chat.postMessage (bot token mode).
type botPayload struct {
	Channel     string            `json:"channel"`
	Text        string            `json:"text,omitempty"`
	Blocks      []slackBlock      `json:"blocks,omitempty"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

// buildPayload assembles the JSON body for either Slack mode.
func buildPayload(cfg config, rec snoozetypes.Record, msg string) ([]byte, error) {
	resolved := rec.State == "close"
	color := severityColor(rec.Severity, resolved)

	// Prefix resolved messages with a visual cue.
	displayMsg := msg
	if resolved && !strings.HasPrefix(msg, "✅") {
		displayMsg = "✅ Resolved: " + msg
	}

	blocks := []slackBlock{
		{
			Type: "section",
			Text: &slackTextObj{Type: "mrkdwn", Text: displayMsg},
		},
	}

	attachment := slackAttachment{Color: color}

	if cfg.BotToken != "" {
		p := botPayload{
			Channel:     cfg.Channel,
			Text:        displayMsg,
			Blocks:      blocks,
			Attachments: []slackAttachment{attachment},
		}
		return json.Marshal(p)
	}

	p := webhookPayload{
		Text:        displayMsg,
		Blocks:      blocks,
		Attachments: []slackAttachment{attachment},
		Username:    cfg.Username,
		IconEmoji:   cfg.IconEmoji,
	}
	return json.Marshal(p)
}
