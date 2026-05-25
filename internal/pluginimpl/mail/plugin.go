// Package mail implements the "mail" Notifier plugin: it renders an alert
// into an SMTP message and delivers it via the configured server.
//
// The plugin is a port of the Python `snooze.plugins.core.mail` plugin. The
// Python implementation lives in src/snooze/plugins/core/mail/plugin.py.
//
// # Config surface
//
// Per-invocation configuration is read from plugins.NotificationPayload.Meta
// (the action_form contents the notification dispatcher would attach). The
// recognised keys mirror the Python action_form, with three Go-side additions
// that have no Python equivalent — see the package README in metadata.yaml:
//
//   - host      string         SMTP server hostname (default "localhost")
//   - port      int            SMTP server port (default 25)
//   - from      string         envelope sender / From header
//   - to        string         comma-separated To recipients
//   - cc        string         comma-separated Cc recipients (Go-only)
//   - bcc       string         comma-separated Bcc recipients (Go-only)
//   - subject   string         text/template rendered against the record
//   - message   string         text/template (plain) or html/template (html)
//   - type      string         "plain" | "html" (default "plain")
//   - priority  int            X-Priority header (1..5, default 3)
//   - tls_mode  string         "none" | "starttls" | "tls" (Go-only, default "none")
//   - username  string         optional PLAIN auth username (Go-only)
//   - password  string         optional PLAIN auth password (Go-only)
//   - timeout   int            seconds; SMTP dial+overall deadline (default 10)
//
// If both NotificationPayload.Subject and NotificationPayload.Body are
// non-empty, the Meta-driven templates are bypassed and the caller's strings
// are used verbatim.
//
// # SMTP transport
//
// Delivery uses the stdlib net/smtp client driven from a tlsDialer so the
// implementation supports the three common transport modes (plaintext,
// STARTTLS, and implicit TLS) without taking a third-party dependency.
package mail

import (
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"fmt"
	htmltmpl "html/template"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	texttmpl "text/template"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("mail", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the mail Notifier implementation.
//
// Batching: actions with `batch: true` accumulate rendered subject+body pairs
// in per-action buckets in `buckets`, flushing on the first of batch_maxsize
// or batch_timer. See batch.go.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	bMu     sync.Mutex
	buckets map[string]*batchBucket
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "mail" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls. Mail keeps no DB-backed
// state; the action config lives on each NotificationPayload.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for mail (no in-process cache to refresh).
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send dispatches one rendered email — or queues the rendered content for a
// later batch flush when the action has batch enabled.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := parseConfig(payload.Meta)
	if err != nil {
		return fmt.Errorf("mail: parse config: %w", err)
	}

	to := splitAddrs(cfg.to)
	cc := splitAddrs(cfg.cc)
	bcc := splitAddrs(cfg.bcc)
	if len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		return errors.New("mail: no recipients (to/cc/bcc are all empty)")
	}

	subject, body, err := renderContent(rec, payload, cfg)
	if err != nil {
		return fmt.Errorf("mail: render: %w", err)
	}

	// Batched dispatch: queue the rendered subject+body for later. The
	// flusher sends one SMTP message containing all queued bodies, using
	// the first record's subject as the message subject.
	if cfg.batch {
		p.queueForBatch(cfg, subject, body)
		return nil
	}

	msg := buildMessage(cfg, to, cc, subject, body)
	rcpts := append(append(append([]string{}, to...), cc...), bcc...)

	return p.deliver(ctx, cfg, rcpts, msg)
}

// --- config -----------------------------------------------------------------

type smtpConfig struct {
	host     string
	port     int
	from     string
	to       string
	cc       string
	bcc      string
	subject  string
	message  string
	mtype    string // "plain" or "html"
	priority int
	tlsMode  string // "none", "starttls", "tls"
	username string
	password string
	timeout  time.Duration

	// Batch knobs (mirror the webhook plugin). Batch coalesces multiple
	// records into one SMTP message; the subject is taken from the first
	// record and bodies are joined with a separator. Degenerate config
	// (batch true but bounds <= 0) silently falls back to immediate send.
	batch        bool
	batchMaxsize int
	batchTimer   time.Duration
	batchKey     string // action_name; only set when batch is true
}

func parseConfig(meta map[string]any) (smtpConfig, error) {
	c := smtpConfig{
		host:     "localhost",
		port:     25,
		mtype:    "plain",
		priority: 3,
		tlsMode:  "none",
		timeout:  10 * time.Second,
	}
	if meta == nil {
		return c, nil
	}
	if v, ok := metaString(meta, "host"); ok && v != "" {
		c.host = v
	}
	if v, ok := metaInt(meta, "port"); ok && v > 0 {
		c.port = v
	}
	c.from, _ = metaString(meta, "from")
	c.to, _ = metaString(meta, "to")
	c.cc, _ = metaString(meta, "cc")
	c.bcc, _ = metaString(meta, "bcc")
	c.subject, _ = metaString(meta, "subject")
	c.message, _ = metaString(meta, "message")
	if v, ok := metaString(meta, "type"); ok {
		switch strings.ToLower(v) {
		case "html":
			c.mtype = "html"
		case "plain", "":
			c.mtype = "plain"
		default:
			return c, fmt.Errorf("invalid type %q (want plain|html)", v)
		}
	}
	if v, ok := metaInt(meta, "priority"); ok && v >= 1 && v <= 5 {
		c.priority = v
	}
	if v, ok := metaString(meta, "tls_mode"); ok {
		switch strings.ToLower(v) {
		case "none", "starttls", "tls", "":
			c.tlsMode = strings.ToLower(v)
			if c.tlsMode == "" {
				c.tlsMode = "none"
			}
		default:
			return c, fmt.Errorf("invalid tls_mode %q (want none|starttls|tls)", v)
		}
	}
	c.username, _ = metaString(meta, "username")
	c.password, _ = metaString(meta, "password")
	if v, ok := metaInt(meta, "timeout"); ok && v > 0 {
		c.timeout = time.Duration(v) * time.Second
	}

	if v, ok := meta["batch"].(bool); ok {
		c.batch = v
	}
	if v, ok := metaInt(meta, "batch_maxsize"); ok && v > 0 {
		c.batchMaxsize = v
	}
	if v, ok := metaInt(meta, "batch_timer"); ok && v > 0 {
		c.batchTimer = time.Duration(v) * time.Second
	}
	// Degenerate config: silently disable batching rather than buffer forever.
	if c.batch && (c.batchMaxsize <= 1 || c.batchTimer <= 0) {
		c.batch = false
	}
	if c.batch {
		c.batchKey, _ = metaString(meta, "action_name")
	}
	return c, nil
}

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

func metaInt(m map[string]any, k string) (int, bool) {
	v, ok := m[k]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float32:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// --- rendering --------------------------------------------------------------

// renderContent returns the final subject and body. The caller's payload
// values win when non-empty; otherwise the Meta templates render against rec.
func renderContent(rec snoozetypes.Record, payload plugins.NotificationPayload, cfg smtpConfig) (string, string, error) {
	subject := payload.Subject
	if subject == "" && cfg.subject != "" {
		s, err := renderText(cfg.subject, rec)
		if err != nil {
			return "", "", fmt.Errorf("subject: %w", err)
		}
		subject = s
	}

	body := payload.Body
	if body == "" {
		tmpl := cfg.message
		if tmpl == "" {
			tmpl = defaultBodyTemplate
		}
		var (
			rendered string
			err      error
		)
		switch cfg.mtype {
		case "html":
			rendered, err = renderHTML(tmpl, rec)
		default:
			rendered, err = renderText(tmpl, rec)
		}
		if err != nil {
			return "", "", fmt.Errorf("body: %w", err)
		}
		body = rendered
	}
	return subject, body, nil
}

// defaultBodyTemplate mirrors the Python DEFAULT_MESSAGE_TEMPLATE but uses
// Go templates and the typed Record fields. It is intentionally trivial and
// is replaced when the action_form supplies a custom message template.
const defaultBodyTemplate = `{{if .Host}}Host: {{.Host}}
{{end}}{{if .Source}}Source: {{.Source}}
{{end}}{{if .Process}}Process: {{.Process}}
{{end}}{{if .Severity}}Severity: {{.Severity}}
{{end}}Received message:
{{.Message}}
`

func renderText(tmpl string, rec snoozetypes.Record) (string, error) {
	t, err := texttmpl.New("mail").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, rec); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func renderHTML(tmpl string, rec snoozetypes.Record) (string, error) {
	t, err := htmltmpl.New("mail").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, rec); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// --- message construction --------------------------------------------------

// splitAddrs splits a comma-separated address list and trims whitespace,
// dropping empty entries. It does not perform RFC 5322 validation; the SMTP
// server is the source of truth for "is this address acceptable".
func splitAddrs(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// buildMessage produces an RFC-5322-ish wire message with the standard
// headers. We avoid MIME multipart (the Python code only sets a single body
// part on the multipart container, so the on-the-wire result is equivalent).
func buildMessage(cfg smtpConfig, to, cc []string, subject, body string) []byte {
	var sb strings.Builder
	if cfg.from != "" {
		sb.WriteString("From: ")
		sb.WriteString(cfg.from)
		sb.WriteString("\r\n")
	}
	if len(to) > 0 {
		sb.WriteString("To: ")
		sb.WriteString(strings.Join(to, ", "))
		sb.WriteString("\r\n")
	}
	if len(cc) > 0 {
		sb.WriteString("Cc: ")
		sb.WriteString(strings.Join(cc, ", "))
		sb.WriteString("\r\n")
	}
	sb.WriteString("Subject: ")
	sb.WriteString(subject)
	sb.WriteString("\r\n")
	sb.WriteString("X-Priority: ")
	sb.WriteString(strconv.Itoa(cfg.priority))
	sb.WriteString("\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	if cfg.mtype == "html" {
		sb.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	} else {
		sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	}
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return []byte(sb.String())
}

// --- delivery ---------------------------------------------------------------

// deliver opens the SMTP connection (respecting tls_mode), authenticates if
// credentials are supplied, and runs MAIL/RCPT/DATA/QUIT. It honours the
// overall ctx deadline by setting a write deadline on the underlying conn.
func (p *Plugin) deliver(ctx context.Context, cfg smtpConfig, rcpts []string, msg []byte) error {
	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	deadline := time.Now().Add(cfg.timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}

	var (
		conn net.Conn
		err  error
	)
	dialer := &net.Dialer{Timeout: cfg.timeout}
	switch cfg.tlsMode {
	case "tls":
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.host, MinVersion: tls.VersionTLS12})
	default:
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, cfg.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer func() {
		// Best-effort QUIT — ignore the error (Close is idempotent).
		_ = c.Quit()
	}()

	if cfg.tlsMode == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("server does not advertise STARTTLS")
		}
		if err := c.StartTLS(&tls.Config{ServerName: cfg.host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if cfg.username != "" || cfg.password != "" {
		if ok, _ := c.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}
	}

	if err := c.Mail(cfg.from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}
	return nil
}
