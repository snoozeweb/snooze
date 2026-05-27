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
	"net/url"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	_ "embed"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

	// Proxy is an optional HTTP/HTTPS/SOCKS proxy URL. When non-empty the
	// http.Transport routes the outbound request through it. Mirrors the
	// Python `proxy` action_form field.
	Proxy string

	// InjectResponse, when true, parses the HTTP response body (as JSON if
	// it parses, otherwise as a string) and stamps it onto the originating
	// record under `response_<action_name>` via payload.Inject. Mirrors the
	// Python `inject_response` action_form field.
	InjectResponse bool

	// Batch accumulates rendered request bodies and flushes them to the
	// configured URL as a single `[obj1, obj2, ...]` JSON array. Mirrors
	// the Python webhook plugin's `batch` knob. When false, every call to
	// Send fires its own HTTP request.
	Batch        bool
	BatchMaxsize int           // flush when bucket reaches this many records
	BatchTimer   time.Duration // flush when bucket is at least this old
	BatchKey     string        // dispatch-bucket key (URL + action_name); only set when Batch is true
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
//
// Batching: actions with `batch: true` accumulate rendered bodies in
// per-(URL, action) buckets in `buckets`, flushing on the first of
// batch_maxsize or batch_timer. See batch.go.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. It is overridable from tests
	// (httptest's transport already satisfies the default behaviour).
	newClient func(cfg Config) *http.Client

	bMu     sync.Mutex
	buckets map[string]*batchBucket
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

	urlRendered, err := renderTemplate("url", cfg.URL, rec)
	if err != nil {
		return fmt.Errorf("webhook: render url: %w", err)
	}
	if urlRendered == "" {
		return errors.New("webhook: url is empty after rendering")
	}
	cfg.URL = urlRendered // store the rendered URL so deliver / batch flush reuse it

	body, contentType, err := renderBody(cfg.Body, rec, payload)
	if err != nil {
		return fmt.Errorf("webhook: render body: %w", err)
	}

	// Batched dispatch: queue the rendered body and return immediately. The
	// flusher posts a `[body1, body2, ...]` JSON array when the bucket
	// reaches batch_maxsize or batch_timer expires (whichever first).
	// Falls back to the immediate path when the rendered body cannot be
	// safely concatenated as a JSON array element (non-JSON template).
	//
	// Batched dispatch and inject_response are mutually exclusive: by the
	// time the bucket flushes the originating record map has been forgotten,
	// so there is no sensible field to stamp the shared response onto.
	// Inject_response forces the immediate path.
	if cfg.Batch && !cfg.InjectResponse && bodyIsJSON(body) {
		p.queueForBatch(cfg, body)
		return nil
	}

	return p.deliver(ctx, cfg, body, contentType, rec, payload)
}

// deliver POSTs body to cfg.URL. Used by both the immediate Send path and the
// batch flush. The record is only consulted for templated headers — pass a
// zero-value Record when calling from the flush path.
//
// The payload argument is the per-record NotificationPayload from Send (or
// a zero value when called from the batch flush, where no single record
// "owns" the response). It is only consulted for the Inject closure that
// implements `inject_response`.
func (p *Plugin) deliver(ctx context.Context, cfg Config, body []byte, contentType string, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
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

	req, err := http.NewRequestWithContext(reqCtx, method, cfg.URL, bodyReader)
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

	// inject_response: stamp the response onto the originating record under
	// `response_<action_name>`. We try to JSON-decode first so downstream
	// consumers see structured data; on failure we fall back to the raw
	// string. The action_name lives in Meta — same place the notification
	// dispatcher placed it via metaFromSubcontent.
	if cfg.InjectResponse {
		actionName, _ := payload.Meta["action_name"].(string)
		if actionName == "" {
			actionName = "webhook"
		}
		field := "response_" + actionName
		var parsed any
		if json.Valid(bytes.TrimSpace(preview)) {
			if err := json.Unmarshal(preview, &parsed); err != nil {
				parsed = string(preview)
			}
		} else {
			parsed = string(preview)
		}
		plugins.InjectField(payload.Inject, field, parsed)
	}
	return nil
}

// bodyIsJSON returns true when body is a valid JSON value. Used to gate
// batched dispatch — concatenating non-JSON bodies into a `[...]` array
// would corrupt the wire shape.
func bodyIsJSON(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	return json.Valid(trimmed)
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
	if cfg.Proxy != "" {
		// url.Parse is permissive (it accepts scheme-less hosts); the resulting
		// proxy is what http.Transport ends up dialling. Parse failure falls
		// back to direct dial and surfaces in the logs only via the proxy
		// being silently absent — operators get an obvious "request went
		// direct" symptom rather than an opaque transport error.
		if u, err := url.Parse(cfg.Proxy); err == nil && u.Scheme != "" {
			transport.Proxy = http.ProxyURL(u)
		}
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
	// Python's webhook plugin spelled the body template `payload`. Accept it
	// as a fallback so action records ported from 1.x keep dispatching
	// without an operator-side migration.
	if cfg.Body == "" {
		if v, ok := p.Meta["payload"].(string); ok && v != "" {
			cfg.Body = v
		}
	}
	if v, ok := p.Meta["tls_insecure"].(bool); ok {
		cfg.TLSInsecure = v
	}
	if v, ok := p.Meta["proxy"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.Proxy = strings.TrimSpace(v)
	}
	if v, ok := p.Meta["inject_response"].(bool); ok {
		cfg.InjectResponse = v
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

	// Batch knobs (mirror the Python plugin's `batch`/`batch_maxsize`/
	// `batch_timer` action fields). Only honoured when batch is explicitly
	// true and both bounds are positive; otherwise we send immediately.
	if v, ok := p.Meta["batch"].(bool); ok {
		cfg.Batch = v
	}
	if v, ok := intField(p.Meta["batch_maxsize"]); ok {
		cfg.BatchMaxsize = v
	}
	if v, ok := intField(p.Meta["batch_timer"]); ok {
		cfg.BatchTimer = time.Duration(v) * time.Second
	}
	if cfg.Batch && (cfg.BatchMaxsize <= 1 || cfg.BatchTimer <= 0) {
		// Degenerate config — fall back to per-record dispatch rather than
		// silently buffering forever.
		cfg.Batch = false
	}
	if cfg.Batch {
		cfg.BatchKey = cfg.URL + "|" + stringField(p.Meta, "action_name")
	}

	return cfg, nil
}

// intField extracts an int from a map[string]any value that may be int,
// int64, float64, or a decimal string. Returns (0, false) on anything else.
func intField(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		n, err := time.ParseDuration(x + "s") // accept "10" → 10s
		if err == nil {
			return int(n / time.Second), true
		}
	}
	return 0, false
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
// yields an empty string with no error. Python-era Jinja2 idioms commonly
// used in legacy webhook templates are normalised before parsing — see
// translatePythonIdioms.
func renderTemplate(name, tmpl string, rec snoozetypes.Record) (string, error) {
	return renderTemplateData(name, tmpl, templateData(rec))
}

// renderTemplateData executes a Go text/template over an arbitrary data value.
// renderTemplate wraps it with the default {Record, Now} data; renderBody adds
// the computed .ReplyToIDs sibling.
func renderTemplateData(name, tmpl string, data any) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	if !strings.Contains(tmpl, "{{") {
		// Fast-path: the value contains no template directives, return it
		// verbatim. Avoids surprising errors on URLs with brace-heavy
		// path segments (none today, but cheap insurance).
		return tmpl, nil
	}
	tmpl = translatePythonIdioms(tmpl)
	t, err := template.New(name).Option("missingkey=zero").Funcs(templateFuncs()).Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// pythonSelfTojson matches the most common Python idiom found in legacy
// action records: `{{ __self__ | tojson() }}`. We tolerate optional
// whitespace and the `()` Jinja filter-call form.
var pythonSelfTojson = regexp.MustCompile(`\{\{\s*__self__\s*\|\s*tojson(?:\(\))?\s*\}\}`)

// pythonSelfBare matches a standalone `{{ __self__ }}` reference.
var pythonSelfBare = regexp.MustCompile(`\{\{\s*__self__\s*\}\}`)

// translatePythonIdioms rewrites the Jinja2 expressions that the Python 1.x
// webhook plugin accepted into the Go text/template equivalents this plugin
// understands. Only the idioms our deployed action records use are handled —
// anything else flows through unchanged and fails loudly at parse time so the
// gap is visible rather than silently producing the wrong body.
func translatePythonIdioms(tmpl string) string {
	tmpl = pythonSelfTojson.ReplaceAllString(tmpl, `{{ tojson .Record }}`)
	tmpl = pythonSelfBare.ReplaceAllString(tmpl, `{{ tojson .Record }}`)
	return tmpl
}

// templateFuncs returns the function map exposed to webhook templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"tojson": func(v any) (string, error) {
			raw, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	}
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

// replyToIDs returns the per-channel message ids a previous inject_response
// stamped onto the record under `response_<actionName>.message_ids`, or nil
// when absent. It is exposed to the body template as `.ReplyToIDs` so a Teams
// action can emit `"reply_to_ids": {{ .ReplyToIDs | tojson }}` to thread the
// follow-up under the recorded message — computing the (possibly
// space-containing) `response_<actionName>` key in Go means the template never
// has to name the action, sidestepping text/template's identifier rules.
// `response_*` survives onto the record because aggregaterule carries it
// forward on a duplicate match (see internal/pluginimpl/aggregaterule).
//
// The lookup tolerates the absence of any layer: a first fire, an action with
// no recorded response yet, or a malformed response all yield nil → the
// template renders JSON null and the bridge posts a fresh root message.
func replyToIDs(rec snoozetypes.Record, actionName string) any {
	if actionName == "" || rec.Extra == nil {
		return nil
	}
	resp, ok := rec.Extra["response_"+actionName].(map[string]any)
	if !ok {
		return nil
	}
	return resp["message_ids"]
}

// renderBody returns (body, contentType). When the config body is empty,
// it falls back to a JSON-encoded record and sets Content-Type. Otherwise
// it renders the body template and lets the caller's headers govern the
// content type (defaulting to application/json when the body looks like
// JSON).
func renderBody(tmpl string, rec snoozetypes.Record, payload plugins.NotificationPayload) ([]byte, string, error) {
	if tmpl == "" {
		data, err := json.Marshal(rec)
		if err != nil {
			return nil, "", err
		}
		return data, "application/json", nil
	}
	data := templateData(rec)
	actionName, _ := payload.Meta["action_name"].(string)
	data["ReplyToIDs"] = replyToIDs(rec, actionName)
	rendered, err := renderTemplateData("body", tmpl, data)
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
