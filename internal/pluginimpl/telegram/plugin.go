// Package telegram implements the "telegram" Notifier plugin: it sends an
// alert message to a Telegram chat using the Telegram Bot API
// (https://core.telegram.org/bots/api#sendmessage).
//
// The plugin POSTs to {api_base}/bot{bot_token}/sendMessage with a JSON body
// containing the chat_id, text, parse_mode, and disable_notification fields.
// The Telegram API returns {"ok": true, ...} on success and {"ok": false,
// "description": "..."} on failure — the plugin propagates the description as
// an error in that case.
//
// This plugin owns no database collection. PostInit stores the host reference;
// Reload is a no-op. The newClient field is overridable from tests so that
// httptest can intercept outbound requests without a real Telegram endpoint.
package telegram

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
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
	plugins.Register("telegram", metaYAML, factory)
}

// defaultTimeout matches the 10 s baseline used by webhook/mail.
const defaultTimeout = 10 * time.Second

// defaultAPIBase is the public Telegram Bot API endpoint.
const defaultAPIBase = "https://api.telegram.org"

// defaultMessage is the Go text/template used when the action_form does not
// supply a custom message. Dynamic fields are HTML-escaped so they render
// safely with parse_mode HTML (the default).
const defaultMessage = "<b>{{ .Severity | htmlEscape }}</b> on {{ .Host | htmlEscape }}\n{{ .Message | htmlEscape }}"

// sendMessageRequest mirrors the relevant fields of the Telegram Bot API
// sendMessage request body.
type sendMessageRequest struct {
	ChatID              string `json:"chat_id"`
	Text                string `json:"text"`
	ParseMode           string `json:"parse_mode,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
}

// sendMessageResponse is the envelope returned by the Telegram Bot API.
// On success ok==true; on failure ok==false and Description contains
// the human-readable reason.
type sendMessageResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

// Config collects the per-action knobs extracted from payload.Meta.
type Config struct {
	BotToken            string
	ChatID              string
	ParseMode           string
	DisableNotification bool
	APIBase             string
	Message             string
	Timeout             time.Duration
}

// Plugin is the Telegram notifier.
//
// Concurrency: Send is safe for concurrent calls. Each call builds its own
// HTTP client (the timeout may vary per action), so there is no shared
// transport state to guard.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. Tests override it to point at an
	// httptest.Server without touching the real Telegram API.
	newClient func(timeout time.Duration) *http.Client
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "telegram" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit stores the host. The plugin has no database collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin holds no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send renders the message template, POSTs it to the Telegram Bot API, and
// returns an error when the API responds with {"ok": false} or with a
// non-200 status code.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("telegram: config: %w", err)
	}

	text, err := renderMessage(cfg.Message, rec)
	if err != nil {
		return fmt.Errorf("telegram: render message: %w", err)
	}

	// Normalise parse_mode: "none" → empty string (omitted from the API request).
	parseMode := cfg.ParseMode
	if strings.EqualFold(parseMode, "none") {
		parseMode = ""
	}

	reqBody := sendMessageRequest{
		ChatID:              cfg.ChatID,
		Text:                text,
		ParseMode:           parseMode,
		DisableNotification: cfg.DisableNotification,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("telegram: marshal request: %w", err)
	}

	apiURL := strings.TrimRight(cfg.APIBase, "/") + "/bot" + cfg.BotToken + "/sendMessage"

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := p.newClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: HTTP %d", resp.StatusCode)
	}

	var apiResp sendMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("telegram: decode response: %w", err)
	}
	if !apiResp.OK {
		desc := apiResp.Description
		if desc == "" {
			desc = "unknown error"
		}
		return fmt.Errorf("telegram: API error: %s", desc)
	}
	return nil
}

// defaultClient returns an http.Client with the given timeout.
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// factory builds the plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// configFromMeta decodes Config from a NotificationPayload.Meta map. Missing
// fields fall back to defaults; missing required fields (bot_token, chat_id)
// return an error.
func configFromMeta(meta map[string]any) (Config, error) {
	cfg := Config{
		ParseMode: "HTML",
		APIBase:   defaultAPIBase,
		Message:   defaultMessage,
		Timeout:   defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("bot_token is required")
	}

	if v, ok := meta["bot_token"].(string); ok && v != "" {
		cfg.BotToken = v
	}
	if v, ok := meta["chat_id"].(string); ok && v != "" {
		cfg.ChatID = v
	}
	if v, ok := meta["parse_mode"].(string); ok && v != "" {
		cfg.ParseMode = v
	}
	if v, ok := meta["message"].(string); ok && v != "" {
		cfg.Message = v
	}
	if v, ok := meta["api_base"].(string); ok && v != "" {
		cfg.APIBase = v
	}
	if v, ok := meta["disable_notification"].(bool); ok {
		cfg.DisableNotification = v
	}
	if t, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = t
	}

	if cfg.BotToken == "" {
		return cfg, fmt.Errorf("bot_token is required")
	}
	if cfg.ChatID == "" {
		return cfg, fmt.Errorf("chat_id is required")
	}
	return cfg, nil
}

// parseTimeout accepts a Go duration string, an int/float64 number of seconds,
// or a time.Duration. Anything else yields (0, false).
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

// templateFuncs returns the function map exposed to message templates.
// htmlEscape is provided so that templates using the default HTML parse_mode
// can safely embed dynamic record fields without injecting rogue HTML tags.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"htmlEscape": html.EscapeString,
	}
}

// renderMessage executes the message Go text/template over the record.
// The template data exposes the record fields directly (not nested) for
// brevity: {{ .Severity }}, {{ .Host }}, {{ .Message }}, etc.
func renderMessage(tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		tmpl = defaultMessage
	}
	t, err := template.New("message").
		Option("missingkey=zero").
		Funcs(templateFuncs()).
		Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Compile-time proof that Plugin satisfies the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)
