// Package googlechat implements the "googlechat" Notifier plugin: it posts an
// alert to a Google Chat space via an Incoming Webhook URL. The plugin
// supports two body modes — a plain {"text":"..."} message and a structured
// cardsV2 card — and optional reply threading via a threadKey.
//
// net/http only; no SDK. All configuration is carried per-call through
// plugins.NotificationPayload.Meta (action_form values).
//
// Note: the bidirectional Google Chat bot (snooze-googlechat daemon) is a
// separate, in-progress component not covered by this plugin.
package googlechat

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
	plugins.Register("googlechat", metaYAML, factory)
}

// defaultTimeout matches the webhook plugin baseline.
const defaultTimeout = 10 * time.Second

// maxResponseBytes caps the body we read for error diagnostics.
const maxResponseBytes = 64 << 10

// defaultMessage is rendered when the operator omits the "message" knob.
const defaultMessage = `*{{ .Severity }}* on {{ .Host }}: {{ .Message }}`

// Plugin is the Google Chat notifier. Send is safe for concurrent calls: every
// call builds its own HTTP client and holds no shared mutable state.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is overridable from tests so httptest can intercept. In
	// production defaultClientFn is used.
	newClient func(timeout time.Duration) *http.Client
}

// factory builds the plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClientFn}, nil
}

// defaultClientFn returns a plain http.Client with the given timeout.
func defaultClientFn(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "googlechat" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host. There is no DB collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClientFn
	}
	return nil
}

// Reload is a no-op: the plugin holds no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send posts a Google Chat message to the configured webhook URL. The Meta map
// carries the action_form field values (webhook_url, message, use_card,
// thread_key, timeout).
//
// On non-2xx the error contains the HTTP status code and a response body
// excerpt. The caller (notification worker) is responsible for retries.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("googlechat: config: %w", err)
	}

	// Render the message template.
	msgText, err := renderTemplate("message", cfg.message, rec)
	if err != nil {
		return fmt.Errorf("googlechat: render message: %w", err)
	}

	// Render the thread_key template (may be empty).
	threadKey, err := renderTemplate("thread_key", cfg.threadKey, rec)
	if err != nil {
		return fmt.Errorf("googlechat: render thread_key: %w", err)
	}
	threadKey = strings.TrimSpace(threadKey)

	// Build the request URL, appending the threading query parameter when
	// a threadKey is present.
	reqURL := cfg.webhookURL
	if threadKey != "" {
		if strings.Contains(reqURL, "?") {
			reqURL += "&messageReplyOption=REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD"
		} else {
			reqURL += "?messageReplyOption=REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD"
		}
	}

	// Build the JSON body.
	body, err := buildBody(rec, msgText, threadKey, cfg.useCard)
	if err != nil {
		return fmt.Errorf("googlechat: build body: %w", err)
	}

	timeout := cfg.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("googlechat: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := p.newClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("googlechat: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("googlechat: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// Compile-time proof we satisfy the contract.
var _ plugins.Notifier = (*Plugin)(nil)

// ---- config -----------------------------------------------------------------

// config holds the per-call knobs decoded from NotificationPayload.Meta.
type config struct {
	webhookURL string
	message    string
	useCard    bool
	threadKey  string
	timeout    time.Duration
}

// configFromMeta decodes config from the payload Meta map. Missing fields fall
// back to sensible defaults; a missing webhook_url is a hard error.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		message: defaultMessage,
		useCard: true,
		timeout: defaultTimeout,
	}

	if meta == nil {
		return cfg, fmt.Errorf("webhook_url is required")
	}

	if v, ok := meta["webhook_url"].(string); ok {
		cfg.webhookURL = strings.TrimSpace(v)
	}
	if cfg.webhookURL == "" {
		return cfg, fmt.Errorf("webhook_url is required")
	}

	if v, ok := meta["message"].(string); ok && v != "" {
		cfg.message = v
	}

	// use_card may arrive as bool (Switch component) or string "true".
	switch v := meta["use_card"].(type) {
	case bool:
		cfg.useCard = v
	case string:
		cfg.useCard = strings.EqualFold(v, "true")
	}

	if v, ok := meta["thread_key"].(string); ok {
		cfg.threadKey = v
	}

	if d, ok := parseTimeout(meta["timeout"]); ok {
		cfg.timeout = d
	}

	return cfg, nil
}

// ---- body builders ----------------------------------------------------------

// cardBody is the JSON shape Google Chat expects for a cardsV2 message.
type cardBody struct {
	CardsV2 []cardEntry    `json:"cardsV2"`
	Thread  *threadWrapper `json:"thread,omitempty"`
}

// plainBody is the JSON shape for a plain text message.
type plainBody struct {
	Text   string         `json:"text"`
	Thread *threadWrapper `json:"thread,omitempty"`
}

type threadWrapper struct {
	ThreadKey string `json:"threadKey"`
}

type cardEntry struct {
	CardID string   `json:"cardId"`
	Card   cardItem `json:"card"`
}

type cardItem struct {
	Header   cardHeader    `json:"header"`
	Sections []cardSection `json:"sections"`
}

type cardHeader struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

type cardSection struct {
	Widgets []cardWidget `json:"widgets"`
}

type cardWidget struct {
	DecoratedText decoratedText `json:"decoratedText"`
}

type decoratedText struct {
	Text string `json:"text"`
}

// buildBody returns the JSON-encoded request body for the given mode.
func buildBody(rec snoozetypes.Record, msgText, threadKey string, useCard bool) ([]byte, error) {
	var thread *threadWrapper
	if threadKey != "" {
		thread = &threadWrapper{ThreadKey: threadKey}
	}

	if useCard {
		b := cardBody{
			CardsV2: []cardEntry{
				{
					CardID: "snooze",
					Card: cardItem{
						Header: cardHeader{
							Title:    rec.Host,
							Subtitle: rec.Severity,
						},
						Sections: []cardSection{
							{
								Widgets: []cardWidget{
									{DecoratedText: decoratedText{Text: msgText}},
								},
							},
						},
					},
				},
			},
			Thread: thread,
		}
		return json.Marshal(b)
	}

	pb := plainBody{Text: msgText, Thread: thread}
	return json.Marshal(pb)
}

// ---- template helpers -------------------------------------------------------

// renderTemplate executes a Go text/template over the record fields. When the
// template string is empty, an empty string is returned without error.
func renderTemplate(name, tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	if !strings.Contains(tmpl, "{{") {
		// Fast-path: no template directives, return verbatim.
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

// ---- utility ----------------------------------------------------------------

// parseTimeout accepts a duration string, int/float64 seconds, or
// time.Duration. Anything else yields (0, false).
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

// truncate returns at most n bytes of b as a string, with an ellipsis when
// the input was longer.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
