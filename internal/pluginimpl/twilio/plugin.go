// Package twilio implements the "twilio" Notifier plugin: it delivers alerts
// via Twilio's REST API as either an SMS message or a voice call.
//
// Two modes are supported:
//
//   - sms (default): POST to the Messages resource with a To, From, and Body.
//     Twilio returns HTTP 201 on success.
//   - voice: POST to the Calls resource with a To, From, and a TwiML <Say>
//     document constructed from the rendered voice_message template. The text
//     is XML-escaped so alert content cannot break the TwiML structure.
//
// Multiple recipients are supported: the `to` field is a comma-separated list
// of E.164 phone numbers. One Twilio API request is made per recipient; errors
// from individual recipients are accumulated and returned together, after all
// recipients have been attempted.
//
// Authentication uses HTTP Basic auth (AccountSID:AuthToken) as required by
// the Twilio REST API.
//
// The plugin owns no database collection. PostInit just stores the host;
// Reload is a no-op.
package twilio

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("twilio", metaYAML, factory)
}

// defaultTimeout matches the webhook plugin baseline.
const defaultTimeout = 10 * time.Second

// defaultMessageTmpl is the SMS body when the action_form message field is empty.
const defaultMessageTmpl = "{{ .Severity }} on {{ .Host }}: {{ .Message }}"

// defaultVoiceMessageTmpl is the <Say> text when voice_message is empty.
const defaultVoiceMessageTmpl = "Snooze alert. {{ .Severity }} on {{ .Host }}. {{ .Message }}"

// factory builds the Plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the Twilio Notifier.
//
// Concurrency: Send is safe for concurrent calls. Config is read from the
// payload on each call; no mutable state is shared across goroutines.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. Overridable from tests so that
	// httptest servers can intercept outbound calls without needing a proxy.
	newClient func(timeout time.Duration) *http.Client
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "twilio" }

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

// Send dispatches an SMS or voice call for each recipient listed in the `to`
// action_form field. It returns an aggregated error if any individual delivery
// fails, after all recipients have been attempted.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("twilio: config: %w", err)
	}

	var errs []string
	for _, to := range cfg.Recipients {
		if err := p.sendOne(ctx, cfg, rec, to); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", to, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("twilio: send failed for %d recipient(s): %s",
			len(errs), strings.Join(errs, "; "))
	}
	return nil
}

// sendOne dispatches a single SMS or voice call to one recipient.
func (p *Plugin) sendOne(ctx context.Context, cfg config, rec snoozetypes.Record, to string) error {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		apiPath string
		body    url.Values
		err     error
	)

	switch cfg.Mode {
	case "voice":
		apiPath, body, err = buildVoiceRequest(cfg, rec, to)
	default: // "sms" or empty
		apiPath, body, err = buildSMSRequest(cfg, rec, to)
	}
	if err != nil {
		return err
	}

	fullURL := cfg.APIBase + apiPath
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, fullURL,
		strings.NewReader(body.Encode()))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(cfg.AccountSID, cfg.AuthToken)

	client := p.newClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to surface the Twilio error message from the JSON body.
		var apiErr struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		}
		if json.Unmarshal(preview, &apiErr) == nil && apiErr.Message != "" {
			return fmt.Errorf("HTTP %d (code %d): %s", resp.StatusCode, apiErr.Code, apiErr.Message)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// buildSMSRequest constructs the URL path and form body for a Messages API call.
func buildSMSRequest(cfg config, rec snoozetypes.Record, to string) (string, url.Values, error) {
	msgTmpl := cfg.Message
	if msgTmpl == "" {
		msgTmpl = defaultMessageTmpl
	}
	msg, err := renderTemplate(msgTmpl, rec)
	if err != nil {
		return "", nil, fmt.Errorf("render message: %w", err)
	}

	path := fmt.Sprintf("/2010-04-01/Accounts/%s/Messages.json", cfg.AccountSID)
	form := url.Values{
		"To":   {to},
		"From": {cfg.From},
		"Body": {msg},
	}
	return path, form, nil
}

// buildVoiceRequest constructs the URL path and form body for a Calls API call.
// The voice_message is rendered as a Go template and then XML-escaped before
// embedding in the <Say> element so alert content cannot break the TwiML structure.
func buildVoiceRequest(cfg config, rec snoozetypes.Record, to string) (string, url.Values, error) {
	voiceTmpl := cfg.VoiceMessage
	if voiceTmpl == "" {
		voiceTmpl = defaultVoiceMessageTmpl
	}
	sayText, err := renderTemplate(voiceTmpl, rec)
	if err != nil {
		return "", nil, fmt.Errorf("render voice_message: %w", err)
	}

	twiml := fmt.Sprintf("<Response><Say>%s</Say></Response>", xmlEscape(sayText))
	path := fmt.Sprintf("/2010-04-01/Accounts/%s/Calls.json", cfg.AccountSID)
	form := url.Values{
		"To":    {to},
		"From":  {cfg.From},
		"Twiml": {twiml},
	}
	return path, form, nil
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

// config holds the per-action knobs decoded from NotificationPayload.Meta.
type config struct {
	AccountSID   string
	AuthToken    string
	From         string
	Recipients   []string
	Mode         string // "sms" | "voice"
	Message      string // SMS body template
	VoiceMessage string // <Say> template
	APIBase      string
	Timeout      time.Duration
}

// configFromMeta decodes config from the action_form Meta map. All required
// fields are validated; optional fields fall back to documented defaults.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		Mode:    "sms",
		APIBase: "https://api.twilio.com",
		Timeout: defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("account_sid is required")
	}

	cfg.AccountSID = metaString(meta, "account_sid")
	cfg.AuthToken = metaString(meta, "auth_token")
	cfg.From = metaString(meta, "from")
	toRaw := metaString(meta, "to")

	// Validate required fields.
	if cfg.AccountSID == "" {
		return cfg, fmt.Errorf("account_sid is required")
	}
	if cfg.AuthToken == "" {
		return cfg, fmt.Errorf("auth_token is required")
	}
	if cfg.From == "" {
		return cfg, fmt.Errorf("from is required")
	}
	if toRaw == "" {
		return cfg, fmt.Errorf("to is required")
	}

	// Parse the comma-separated recipient list; trim whitespace around each entry.
	for _, r := range strings.Split(toRaw, ",") {
		if r := strings.TrimSpace(r); r != "" {
			cfg.Recipients = append(cfg.Recipients, r)
		}
	}
	if len(cfg.Recipients) == 0 {
		return cfg, fmt.Errorf("to is required")
	}

	if m := metaString(meta, "mode"); m != "" {
		cfg.Mode = strings.ToLower(m)
	}
	if m := metaString(meta, "message"); m != "" {
		cfg.Message = m
	}
	if m := metaString(meta, "voice_message"); m != "" {
		cfg.VoiceMessage = m
	}
	if b := metaString(meta, "api_base"); b != "" {
		cfg.APIBase = strings.TrimRight(b, "/")
	}
	if t, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = t
	}
	return cfg, nil
}

// metaString reads key from m as a string; returns "" when absent or wrong type.
func metaString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// parseTimeout accepts a duration string, int/float64 seconds, or time.Duration.
// Returns (0, false) on anything else.
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
// Template rendering
// ---------------------------------------------------------------------------

// renderTemplate executes a Go text/template against the record's top-level
// fields. The record is passed directly as the template data so callers can
// write "{{ .Severity }}", "{{ .Host }}", etc.
func renderTemplate(tmpl string, rec snoozetypes.Record) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}
	t, err := template.New("msg").Option("missingkey=zero").Parse(tmpl)
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
// Utilities
// ---------------------------------------------------------------------------

// xmlEscape replaces the five XML-special characters so the text can be safely
// embedded in a TwiML element. We use a simple string replacer rather than
// encoding/xml to avoid importing an extra package for a trivial substitution.
func xmlEscape(s string) string {
	// The order matters: & must be replaced first to avoid double-escaping.
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// truncate returns at most n bytes of b as a string, with a trailing ellipsis
// when the input was longer. Mirrors the helper in the webhook plugin.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
