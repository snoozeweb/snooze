// Package pushover implements the "pushover" Notifier plugin: it delivers a
// mobile push notification via the Pushover Messages API
// (https://pushover.net/api).
//
// Every invocation of Send renders the title and message Go text/templates
// against the alert record, resolves the delivery priority from either the
// explicit action_form value or the record's severity, and POSTs an
// application/x-www-form-urlencoded request to
// {api_base}/1/messages.json.
//
// The plugin owns no database collection. PostInit just stores the host;
// Reload is a no-op.
package pushover

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("pushover", metaYAML, factory)
}

// defaultTimeout is the per-request deadline used when no timeout is
// configured in the action_form. Matches the Snooze-wide baseline of 10 s.
const defaultTimeout = 10 * time.Second

// defaultAPIBase is the canonical Pushover Messages API endpoint.
const defaultAPIBase = "https://api.pushover.net"

// defaultTitleTemplate and defaultMessageTemplate are the action_form defaults
// expressed as Go text/templates executed against a snoozetypes.Record.
const defaultTitleTemplate = "{{ .Severity }} on {{ .Host }}"
const defaultMessageTemplate = "{{ .Message }}"

// config holds the per-action knobs decoded from NotificationPayload.Meta.
type config struct {
	token    string
	user     string
	title    string // Go template string
	message  string // Go template string
	priority string // "auto" | "-2" | "-1" | "0" | "1" | "2"
	sound    string
	url      string
	urlTitle string
	apiBase  string
	timeout  time.Duration
}

// Plugin is the Pushover notifier.
//
// Concurrency: Send is safe for concurrent calls. The HTTP client is built
// per-call (via newClient) so per-action timeout knobs are honoured across
// concurrent invocations.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is overridable from tests so httptest servers can intercept.
	newClient func(timeout time.Duration) *http.Client
}

// factory builds the Plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "pushover" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host. There is no DB-backed state to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin has no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send delivers a push notification for rec using the action_form values in
// payload.Meta.
//
// On success the Pushover API returns HTTP 200 with {"status":1,...}. Any
// other HTTP status or a status field != 1 is returned as an error (the
// "errors" array from the API body is included when present).
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("pushover: config: %w", err)
	}

	title, err := renderField("title", cfg.title, rec)
	if err != nil {
		return fmt.Errorf("pushover: render title: %w", err)
	}
	message, err := renderField("message", cfg.message, rec)
	if err != nil {
		return fmt.Errorf("pushover: render message: %w", err)
	}

	priority, err := resolvePriority(cfg.priority, rec.Severity)
	if err != nil {
		return fmt.Errorf("pushover: priority: %w", err)
	}

	// Build the form-encoded request body.
	form := url.Values{}
	form.Set("token", cfg.token)
	form.Set("user", cfg.user)
	form.Set("title", title)
	form.Set("message", message)
	form.Set("priority", strconv.Itoa(priority))

	// Priority 2 (emergency) requires retry and expire parameters per the
	// Pushover API specification.
	if priority == 2 {
		form.Set("retry", "60")
		form.Set("expire", "3600")
	}
	if cfg.sound != "" {
		form.Set("sound", cfg.sound)
	}
	if cfg.url != "" {
		form.Set("url", cfg.url)
	}
	if cfg.urlTitle != "" {
		form.Set("url_title", cfg.urlTitle)
	}

	endpoint := strings.TrimRight(cfg.apiBase, "/") + "/1/messages.json"

	timeout := cfg.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("pushover: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := p.newClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pushover: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Pushover returns HTTP 200 on success; non-200 means the API rejected
	// the request before even parsing the body.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pushover: HTTP %d", resp.StatusCode)
	}

	// Decode the JSON response and check the API-level status field.
	// A Pushover error body looks like: {"status":0,"errors":["..."]}
	var apiResp struct {
		Status int      `json:"status"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		// If we cannot decode the response, we already got HTTP 200, so
		// treat this as success (the notification was likely delivered).
		return nil
	}
	if apiResp.Status != 1 {
		if len(apiResp.Errors) > 0 {
			return fmt.Errorf("pushover: API error: %s", strings.Join(apiResp.Errors, "; "))
		}
		return fmt.Errorf("pushover: API returned status %d", apiResp.Status)
	}
	return nil
}

// Compile-time proof we satisfy the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)

// defaultClient returns a plain http.Client with the given timeout.
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// configFromMeta decodes the action_form values from a NotificationPayload.Meta
// map. Missing optional fields fall back to documented defaults; the required
// fields (token, user) produce an error when absent.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		title:    defaultTitleTemplate,
		message:  defaultMessageTemplate,
		priority: "auto",
		apiBase:  defaultAPIBase,
		timeout:  defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("token is required")
	}

	if v, ok := metaString(meta, "token"); ok && v != "" {
		cfg.token = v
	}
	if v, ok := metaString(meta, "user"); ok && v != "" {
		cfg.user = v
	}
	if cfg.token == "" {
		return cfg, fmt.Errorf("token is required")
	}
	if cfg.user == "" {
		return cfg, fmt.Errorf("user is required")
	}

	if v, ok := metaString(meta, "title"); ok && v != "" {
		cfg.title = v
	}
	if v, ok := metaString(meta, "message"); ok && v != "" {
		cfg.message = v
	}
	if v, ok := metaString(meta, "priority"); ok && v != "" {
		cfg.priority = v
	}
	if v, ok := metaString(meta, "sound"); ok {
		cfg.sound = v
	}
	if v, ok := metaString(meta, "url"); ok {
		cfg.url = v
	}
	if v, ok := metaString(meta, "url_title"); ok {
		cfg.urlTitle = v
	}
	if v, ok := metaString(meta, "api_base"); ok && v != "" {
		cfg.apiBase = v
	}
	if t, ok := parseTimeout(meta["timeout"]); ok {
		cfg.timeout = t
	}
	return cfg, nil
}

// resolvePriority converts the action_form priority knob and the record's
// severity into an integer priority in the range [-2, 2].
//
// When the knob is "auto", the severity is mapped as follows:
//
//	emergency / critical → 2 (emergency — requires retry+expire)
//	error / err          → 1 (high)
//	warning              → 0 (normal)
//	everything else      → -1 (low)
func resolvePriority(knob, severity string) (int, error) {
	if knob != "auto" {
		n, err := strconv.Atoi(strings.TrimSpace(knob))
		if err != nil || n < -2 || n > 2 {
			return 0, fmt.Errorf("invalid priority %q (want auto or integer -2..2)", knob)
		}
		return n, nil
	}
	// Severity-based mapping: normalise to lowercase and strip common aliases.
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "emergency", "critical":
		return 2, nil
	case "error", "err":
		return 1, nil
	case "warning", "warn":
		return 0, nil
	default:
		// info, notice, debug, or any unknown severity → low priority so
		// non-actionable alerts do not overwhelm the user.
		return -1, nil
	}
}

// renderField executes a Go text/template against the record. An empty
// template string returns an empty string without error.
func renderField(name, tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		return "", nil
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

// metaString extracts a string value from the Meta map, tolerating int/float
// values produced by JSON unmarshalling (which uses float64 for numbers).
func metaString(m map[string]any, k string) (string, bool) {
	v, ok := m[k]
	if !ok {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case fmt.Stringer:
		return x.String(), true
	default:
		return fmt.Sprintf("%v", x), true
	}
}

// parseTimeout accepts a duration string, an integer (seconds), or a float64
// (seconds). Anything else yields (0, false).
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
