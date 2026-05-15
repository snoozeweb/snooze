// Package webhook implements the "webhook" core Notifier plugin: an
// outbound HTTP dispatcher. Notification entries route matching records to
// a configured webhook action, the notification dispatcher publishes a
// Payload on the bus, and a worker drains the bus and invokes Send on this
// plugin.
//
// Compared to the Python implementation (src/snooze/plugins/core/webhook),
// the Go port keeps the shape (URL + headers + body templated over the
// record) but standardises on:
//
//   - net/http only (no third-party http client),
//   - Go text/template instead of Jinja for url/headers/body,
//   - per-call config carried via plugins.ActionOpts.Form,
//   - explicit timeout / TLS-insecure / auth knobs.
//
// The plugin owns no database collection. It is a pure Notifier: PostInit
// just stores the host, Reload is a no-op.
package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	_ "embed"

	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("webhook", metaYAML, factory)
}

// defaultTimeout matches the Python (10s connect, 13s read) baseline:
// the Go port collapses both into a single request timeout.
const defaultTimeout = 10 * time.Second

// maxResponseBytes caps the body we read for logging/diagnostics. The
// notifier does not inject responses back into the record (Python's
// `inject_response` is not modelled in the Go pipeline yet).
const maxResponseBytes = 64 << 10

// Config captures the per-action knobs the worker passes via the
// NotificationPayload.Meta map (which originates from action_form in the
// notification's `action` document). Values are decoded loosely so the
// caller can pass `int`, `float64`, or `string` for the timeout, etc.
type Config struct {
	URL         string
	Method      string
	Headers     map[string]string
	Body        string
	Timeout     time.Duration
	TLSInsecure bool
	Auth        Auth
}

// Auth carries the optional auth header settings.
type Auth struct {
	Type     string // "" | "bearer" | "basic"
	Token    string // bearer
	Username string // basic
	Password string // basic
}

// Plugin is the webhook notifier.
//
// Concurrency: Send is safe for concurrent calls. The HTTP client is built
// per-call because the per-action TLS / timeout knobs differ between
// invocations; production volume is bounded by the notification worker
// concurrency, not by transport reuse.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. It is overridable from tests
	// (httptest's transport already satisfies the default behaviour).
	newClient func(cfg Config) *http.Client
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "webhook" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host. There is no DB collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin has no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send dispatches a single HTTP request rendered from the payload's Meta
// map. The Meta map carries the action_form fields (url, method, headers,
// body, timeout, tls_insecure, auth) as supplied by the notification
// configuration.
//
// The verdict-style return is "error == nil on 2xx; error otherwise". The
// caller (notification worker) is responsible for retries and dead-letter
// handling.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromPayload(payload)
	if err != nil {
		return fmt.Errorf("webhook: config: %w", err)
	}

	url, err := renderTemplate("url", cfg.URL, rec)
	if err != nil {
		return fmt.Errorf("webhook: render url: %w", err)
	}
	if url == "" {
		return errors.New("webhook: url is empty after rendering")
	}

	body, contentType, err := renderBody(cfg.Body, rec, payload)
	if err != nil {
		return fmt.Errorf("webhook: render body: %w", err)
	}

	method := strings.ToUpper(strings.TrimSpace(cfg.Method))
	if method == "" {
		method = http.MethodPost
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}

	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}

	if err := applyHeaders(req, cfg.Headers, rec); err != nil {
		return fmt.Errorf("webhook: render headers: %w", err)
	}
	if err := applyAuth(req, cfg.Auth); err != nil {
		return fmt.Errorf("webhook: auth: %w", err)
	}

	client := p.newClient(cfg)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Drain a bounded amount so the connection can be reused if the
	// transport is shared. We surface the prefix in the error message for
	// non-2xx responses to make debugging less painful.
	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// factory builds the plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// defaultClient returns an http.Client honouring the per-call config.
func defaultClient(cfg Config) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecure, //nolint:gosec
			MinVersion:         tls.VersionTLS12,
		},
	}
	// We rely on the per-request context for the deadline, but set a
	// belt-and-braces client timeout in case a future caller forgets to
	// pass one.
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

// configFromPayload decodes Config from NotificationPayload.Meta. Missing
// fields fall back to defaults; unknown fields are ignored.
//
//nolint:gocognit // straight-line key extraction; the linear shape is the point.
func configFromPayload(p plugins.NotificationPayload) (Config, error) {
	cfg := Config{
		Method:  http.MethodPost,
		Body:    p.Body,
		Timeout: defaultTimeout,
	}
	if p.Meta == nil {
		return cfg, nil
	}

	if v, ok := p.Meta["url"].(string); ok {
		cfg.URL = v
	}
	if v, ok := p.Meta["method"].(string); ok && v != "" {
		cfg.Method = v
	}
	if v, ok := p.Meta["body"].(string); ok && v != "" {
		cfg.Body = v
	}
	if v, ok := p.Meta["tls_insecure"].(bool); ok {
		cfg.TLSInsecure = v
	}

	switch h := p.Meta["headers"].(type) {
	case map[string]string:
		cfg.Headers = h
	case map[string]any:
		cfg.Headers = make(map[string]string, len(h))
		for k, vv := range h {
			if s, ok := vv.(string); ok {
				cfg.Headers[k] = s
			}
		}
	}

	if t, ok := parseTimeout(p.Meta["timeout"]); ok {
		cfg.Timeout = t
	}

	if a, ok := p.Meta["auth"].(map[string]any); ok {
		cfg.Auth = Auth{
			Type:     strings.ToLower(stringField(a, "type")),
			Token:    stringField(a, "token"),
			Username: stringField(a, "username"),
			Password: stringField(a, "password"),
		}
	}

	return cfg, nil
}

// parseTimeout accepts a duration string, a number of seconds (int or
// float64), or a time.Duration. Anything else yields (0, false).
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

func stringField(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

// renderTemplate executes a Go text/template over the record. Empty input
// yields an empty string with no error.
func renderTemplate(name, tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	if !strings.Contains(tmpl, "{{") {
		// Fast-path: the value contains no template directives, return it
		// verbatim. Avoids surprising errors on URLs with brace-heavy
		// path segments (none today, but cheap insurance).
		return tmpl, nil
	}
	t, err := template.New(name).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, templateData(rec)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// templateData wraps the record so callers can write `{{.Record.Host}}`
// in templates while keeping room to add sibling fields later (e.g.
// `.Now`) without breaking back-compat.
func templateData(rec snoozetypes.Record) map[string]any {
	return map[string]any{
		"Record": rec,
		"Now":    time.Now().UTC(),
	}
}

// renderBody returns (body, contentType). When the config body is empty,
// it falls back to a JSON-encoded record and sets Content-Type. Otherwise
// it renders the body template and lets the caller's headers govern the
// content type (defaulting to application/json when the body looks like
// JSON).
func renderBody(tmpl string, rec snoozetypes.Record, _ plugins.NotificationPayload) ([]byte, string, error) {
	if tmpl == "" {
		data, err := json.Marshal(rec)
		if err != nil {
			return nil, "", err
		}
		return data, "application/json", nil
	}
	rendered, err := renderTemplate("body", tmpl, rec)
	if err != nil {
		return nil, "", err
	}
	ct := ""
	trimmed := strings.TrimSpace(rendered)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		ct = "application/json"
	}
	return []byte(rendered), ct, nil
}

// applyHeaders renders and sets each configured header on the request.
func applyHeaders(req *http.Request, headers map[string]string, rec snoozetypes.Record) error {
	for k, v := range headers {
		rendered, err := renderTemplate("header:"+k, v, rec)
		if err != nil {
			return err
		}
		req.Header.Set(k, rendered)
	}
	return nil
}

// applyAuth installs the requested authentication header. Unknown types
// fail closed: silently letting a typo strip auth would be a security
// footgun.
func applyAuth(req *http.Request, a Auth) error {
	switch a.Type {
	case "":
		return nil
	case "bearer":
		if a.Token == "" {
			return errors.New("bearer auth requires token")
		}
		req.Header.Set("Authorization", "Bearer "+a.Token)
		return nil
	case "basic":
		if a.Username == "" {
			return errors.New("basic auth requires username")
		}
		creds := a.Username + ":" + a.Password
		enc := base64.StdEncoding.EncodeToString([]byte(creds))
		req.Header.Set("Authorization", "Basic "+enc)
		return nil
	default:
		return fmt.Errorf("unsupported auth type %q", a.Type)
	}
}

// truncate returns at most n bytes of b as a string, with an ellipsis if
// the input was longer.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
