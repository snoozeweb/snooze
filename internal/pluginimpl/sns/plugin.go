// Package sns implements the "sns" Notifier plugin: it publishes a Snooze
// alert to an Amazon SNS topic (fan-out output) via the SNS Query API
// (Action=Publish). It runs in-process inside snooze-server; no daemon.
//
// SigV4 is hand-rolled on the standard library (see sigv4.go) — there is no
// AWS SDK dependency. Only the Publish action is supported.
//
// For each matching record the plugin builds an
// application/x-www-form-urlencoded POST to the regional SNS endpoint
// (https://sns.<region>.amazonaws.com/ by default, overridable for LocalStack),
// signs it with AWS Signature Version 4 for service "sns", and treats an HTTP
// 200 with a <PublishResponse> body as success. On a non-2xx response the SNS
// error XML (<Error><Code>…</Code><Message>…</Message></Error>) is parsed and
// surfaced in the returned error.
//
// The plugin owns no database collection. PostInit stores the host; Reload is
// a no-op.
package sns

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/xml"
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
	plugins.Register("sns", metaYAML, factory)
}

// defaultTimeout matches the Go port baseline used by the other notifiers.
const defaultTimeout = 10 * time.Second

// maxResponseBytes caps how many bytes we read from SNS for diagnostics.
const maxResponseBytes = 16 << 10

// SNS Query API constants.
const (
	snsAPIVersion = "2010-03-31"
	snsAction     = "Publish"
	snsService    = "sns"
)

// Default templates, mirrored in metadata.yaml so the UI and the code agree
// when an action_form value is left blank.
const (
	defaultSubjectTmpl = "{{ .Severity }} on {{ .Host }}"
	defaultMessageTmpl = "{{ .Message }}"
)

// Plugin is the Amazon SNS notifier.
//
// Concurrency: Send is safe for concurrent calls. A fresh http.Client is built
// per call because per-action timeout knobs may differ between invocations.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. Tests replace it with a function
	// that returns a client aimed at an httptest server.
	newClient func(timeout time.Duration) *http.Client
}

// factory builds the plugin instance with the production http.Client builder.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "sns" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit stores the host. There is no DB collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin has no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send publishes one message to the configured SNS topic. The per-action knobs
// are read from payload.Meta (populated from the action_form values by the
// notification dispatcher).
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("sns: config: %w", err)
	}

	subject, err := renderTemplate("subject", cfg.Subject, rec)
	if err != nil {
		return fmt.Errorf("sns: render subject: %w", err)
	}
	message, err := renderTemplate("message", cfg.Message, rec)
	if err != nil {
		return fmt.Errorf("sns: render message: %w", err)
	}

	// Build the application/x-www-form-urlencoded body for the Publish action.
	form := url.Values{}
	form.Set("Action", snsAction)
	form.Set("Version", snsAPIVersion)
	form.Set("TopicArn", cfg.TopicArn)
	form.Set("Message", message)
	if s := snsSubject(subject); s != "" {
		form.Set("Subject", s)
	}
	body := []byte(form.Encode())

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://sns.%s.amazonaws.com/", cfg.Region)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("sns: parse endpoint %q: %w", endpoint, err)
	}
	path := u.Path
	if path == "" {
		path = "/"
	}

	const contentType = "application/x-www-form-urlencoded"
	sig := sign(signParams{
		Method:  http.MethodPost,
		Host:    u.Host,
		Path:    path,
		Query:   "",
		Headers: map[string]string{"content-type": contentType},
		Body:    body,
		Region:  cfg.Region,
		Service: snsService,
		Creds: credentials{
			AccessKeyID:     cfg.AccessKeyID,
			SecretAccessKey: cfg.SecretAccessKey,
			SessionToken:    cfg.SessionToken,
		},
	})

	reqCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sns: build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Amz-Date", sig.AmzDate)
	req.Header.Set("Authorization", sig.Authorization)
	if sig.SecurityToken != "" {
		req.Header.Set("X-Amz-Security-Token", sig.SecurityToken)
	}

	client := p.newClient(cfg.Timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sns: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if code, msg := parseSNSError(preview); code != "" {
			return fmt.Errorf("sns: HTTP %d: %s: %s", resp.StatusCode, code, msg)
		}
		return fmt.Errorf("sns: HTTP %d: %s", resp.StatusCode, truncate(preview, 400))
	}
	return nil
}

// snsSubject trims the rendered subject and clamps it to the SNS limit. SNS
// rejects subjects longer than 100 ASCII characters, and also rejects subjects
// containing control characters; we only enforce the length here and let SNS
// validate the rest, mirroring the other notifiers' light-touch approach.
func snsSubject(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

// defaultClient returns an http.Client with the given timeout. It uses the
// default transport (TLS-verified, standard dial).
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// Compile-time proof we satisfy the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)

// ---------------------------------------------------------------------------
// SNS response parsing
// ---------------------------------------------------------------------------

// snsErrorResponse models the XML returned by SNS on a failed request:
//
//	<ErrorResponse><Error><Code>…</Code><Message>…</Message></Error></ErrorResponse>
type snsErrorResponse struct {
	XMLName xml.Name `xml:"ErrorResponse"`
	Error   struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	} `xml:"Error"`
}

// parseSNSError extracts the SNS error code and message from a response body.
// Returns ("", "") when the body does not parse as an SNS error document.
func parseSNSError(body []byte) (code, message string) {
	var e snsErrorResponse
	if err := xml.Unmarshal(body, &e); err != nil {
		return "", ""
	}
	return strings.TrimSpace(e.Error.Code), strings.TrimSpace(e.Error.Message)
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// config captures the per-action knobs from NotificationPayload.Meta.
type config struct {
	Region          string
	TopicArn        string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Subject         string
	Message         string
	Endpoint        string
	Timeout         time.Duration
}

// configFromMeta decodes config from a NotificationPayload.Meta map. The map
// contains action_form values: strings, floats, bools. Missing optional fields
// fall back to defaults; region/topic_arn/access_key_id/secret_access_key are
// required and yield an error when absent.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		Subject: defaultSubjectTmpl,
		Message: defaultMessageTmpl,
		Timeout: defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("region, topic_arn, access_key_id and secret_access_key are required")
	}

	cfg.Region = metaString(meta, "region")
	cfg.TopicArn = metaString(meta, "topic_arn")
	cfg.AccessKeyID = metaString(meta, "access_key_id")
	cfg.SecretAccessKey = metaString(meta, "secret_access_key")
	cfg.SessionToken = metaString(meta, "session_token")
	cfg.Endpoint = metaString(meta, "endpoint")

	if v := metaString(meta, "subject"); v != "" {
		cfg.Subject = v
	}
	if v := metaString(meta, "message"); v != "" {
		cfg.Message = v
	}
	if t, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = t
	}

	var missing []string
	if cfg.Region == "" {
		missing = append(missing, "region")
	}
	if cfg.TopicArn == "" {
		missing = append(missing, "topic_arn")
	}
	if cfg.AccessKeyID == "" {
		missing = append(missing, "access_key_id")
	}
	if cfg.SecretAccessKey == "" {
		missing = append(missing, "secret_access_key")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required field(s): %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

// metaString reads a string action_form value, returning "" when absent or of
// the wrong type.
func metaString(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return strings.TrimSpace(v)
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

// ---------------------------------------------------------------------------
// Templating
// ---------------------------------------------------------------------------

// renderTemplate executes a Go text/template over the record. The record is
// the dot, so templates read fields directly (e.g. "{{ .Host }}"). An empty
// template yields an empty string with no error; a template with no directives
// is returned verbatim.
func renderTemplate(name, tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	if !strings.Contains(tmpl, "{{") {
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

// truncate returns at most n bytes of b as a string, appending "..." when the
// source was longer. Used for embedding error bodies in error messages.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
