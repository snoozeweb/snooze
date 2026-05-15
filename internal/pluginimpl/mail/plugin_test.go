package mail

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/japannext/snooze/internal/config"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/telemetry"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// nullHost is the minimal Host the mail plugin needs. Mail never touches the
// DB or bus during Send, so most accessors are stubbed.
type nullHost struct{}

func (nullHost) DB() db.Driver                { return nil }
func (nullHost) Bus() plugins.Bus             { return nil }
func (nullHost) Logger() *slog.Logger         { return slog.Default() }
func (nullHost) Tracer() trace.Tracer         { return otel.Tracer("mail-test") }
func (nullHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nullHost) Config() *config.Config       { return config.Default() }
func (nullHost) Plugin(string) plugins.Plugin { return nil }

// Compile-time check: Plugin satisfies the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)

// --- stub SMTP server ------------------------------------------------------

// smtpCapture records the conversation a stubSMTP saw during a single
// session. Fields are mutated under the stubSMTP's lock.
type smtpCapture struct {
	mu   sync.Mutex
	helo string
	from string
	to   []string
	data strings.Builder
}

func (c *smtpCapture) snapshot() (helo, from string, to []string, data string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.helo, c.from, slices.Clone(c.to), c.data.String()
}

// stubSMTP is a tiny line-oriented SMTP server that supports just enough of
// the protocol to drive net/smtp through MAIL/RCPT/DATA/QUIT.
type stubSMTP struct {
	ln     net.Listener
	cap    *smtpCapture
	done   chan struct{}
	failOn string // command verb to NAK ("MAIL", "RCPT", "DATA", "")
}

func newStubSMTP(t *testing.T, failOn string) *stubSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &stubSMTP{
		ln:     ln,
		cap:    &smtpCapture{},
		done:   make(chan struct{}),
		failOn: failOn,
	}
	go s.accept()
	t.Cleanup(func() {
		_ = ln.Close()
		<-s.done
	})
	return s
}

func (s *stubSMTP) hostPort() (string, int) {
	addr := s.ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func (s *stubSMTP) accept() {
	defer close(s.done)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *stubSMTP) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	write := func(line string) {
		_, _ = bw.WriteString(line + "\r\n")
		_ = bw.Flush()
	}
	write("220 stub.test ESMTP ready")

	inData := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				inData = false
				write("250 OK queued")
				continue
			}
			s.cap.mu.Lock()
			s.cap.data.WriteString(line)
			s.cap.data.WriteString("\r\n")
			s.cap.mu.Unlock()
			continue
		}

		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "HELO", "EHLO":
			s.cap.mu.Lock()
			s.cap.helo = line
			s.cap.mu.Unlock()
			// Advertise no extensions (AUTH/STARTTLS off) for simplicity;
			// individual tests can opt in by extending this stub.
			write("250-stub.test")
			write("250 SIZE 1048576")
		case "MAIL":
			if s.failOn == "MAIL" {
				write("550 mail refused")
				continue
			}
			s.cap.mu.Lock()
			s.cap.from = extractAngle(line)
			s.cap.mu.Unlock()
			write("250 OK")
		case "RCPT":
			if s.failOn == "RCPT" {
				write("550 rcpt refused")
				continue
			}
			s.cap.mu.Lock()
			s.cap.to = append(s.cap.to, extractAngle(line))
			s.cap.mu.Unlock()
			write("250 OK")
		case "DATA":
			if s.failOn == "DATA" {
				write("554 data refused")
				continue
			}
			write("354 End data with <CR><LF>.<CR><LF>")
			inData = true
		case "QUIT":
			write("221 Bye")
			return
		case "RSET":
			write("250 OK")
		case "NOOP":
			write("250 OK")
		default:
			write("502 unrecognised")
		}
	}
}

// extractAngle pulls the address out of a "MAIL FROM:<x@y>" / "RCPT TO:<x@y>"
// line. It falls back to the whole tail if no angle brackets are present.
func extractAngle(line string) string {
	if i := strings.Index(line, "<"); i >= 0 {
		if j := strings.Index(line[i+1:], ">"); j >= 0 {
			return line[i+1 : i+1+j]
		}
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return line
}

// --- tests -----------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "mail"))
}

func TestNameAndMetadata(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "mail"}}
	require.Equal(t, "mail", p.Name())
	require.Equal(t, "mail", p.Metadata().Name)
}

func TestPostInitAndReload(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.PostInit(context.Background(), nullHost{}))
	require.NoError(t, p.Reload(context.Background()))
}

func TestSplitAddrs(t *testing.T) {
	require.Nil(t, splitAddrs(""))
	require.Equal(t, []string{"a@x"}, splitAddrs("a@x"))
	require.Equal(t, []string{"a@x", "b@y"}, splitAddrs("a@x, b@y"))
	require.Equal(t, []string{"a@x", "b@y"}, splitAddrs("a@x,, b@y , "))
}

func TestRenderContent_PayloadWins(t *testing.T) {
	rec := snoozetypes.Record{Host: "h1", Message: "boom"}
	pl := plugins.NotificationPayload{Subject: "S!", Body: "B!"}
	cfg := smtpConfig{subject: "ignored {{.Host}}", message: "ignored", mtype: "plain"}
	subject, body, err := renderContent(rec, pl, cfg)
	require.NoError(t, err)
	require.Equal(t, "S!", subject)
	require.Equal(t, "B!", body)
}

func TestRenderContent_TemplateFromMeta(t *testing.T) {
	rec := snoozetypes.Record{Host: "h1", Message: "boom"}
	cfg := smtpConfig{
		subject: "Alert: {{.Host}}",
		message: "Got: {{.Message}}",
		mtype:   "plain",
	}
	subject, body, err := renderContent(rec, plugins.NotificationPayload{}, cfg)
	require.NoError(t, err)
	require.Equal(t, "Alert: h1", subject)
	require.Equal(t, "Got: boom", body)
}

func TestRenderContent_DefaultBody(t *testing.T) {
	rec := snoozetypes.Record{Host: "h1", Severity: "warn", Message: "boom"}
	_, body, err := renderContent(rec, plugins.NotificationPayload{}, smtpConfig{mtype: "plain"})
	require.NoError(t, err)
	require.Contains(t, body, "Host: h1")
	require.Contains(t, body, "Severity: warn")
	require.Contains(t, body, "Received message:")
	require.Contains(t, body, "boom")
}

func TestRenderContent_HTMLEscapes(t *testing.T) {
	rec := snoozetypes.Record{Message: "<script>x</script>"}
	cfg := smtpConfig{message: "Body: {{.Message}}", mtype: "html"}
	_, body, err := renderContent(rec, plugins.NotificationPayload{}, cfg)
	require.NoError(t, err)
	// html/template must HTML-escape the angle brackets.
	require.NotContains(t, body, "<script>")
	require.Contains(t, body, "&lt;script&gt;")
}

func TestParseConfig_Defaults(t *testing.T) {
	c, err := parseConfig(nil)
	require.NoError(t, err)
	require.Equal(t, "localhost", c.host)
	require.Equal(t, 25, c.port)
	require.Equal(t, "plain", c.mtype)
	require.Equal(t, 3, c.priority)
	require.Equal(t, "none", c.tlsMode)
	require.Equal(t, 10*time.Second, c.timeout)
}

func TestParseConfig_Override(t *testing.T) {
	c, err := parseConfig(map[string]any{
		"host":     "smtp.example.com",
		"port":     float64(2525), // JSON-decoded numbers come back as float64
		"from":     "noc@example.com",
		"to":       "a@x, b@y",
		"type":     "html",
		"priority": 1,
		"tls_mode": "STARTTLS",
		"timeout":  "30",
	})
	require.NoError(t, err)
	require.Equal(t, "smtp.example.com", c.host)
	require.Equal(t, 2525, c.port)
	require.Equal(t, "noc@example.com", c.from)
	require.Equal(t, "a@x, b@y", c.to)
	require.Equal(t, "html", c.mtype)
	require.Equal(t, 1, c.priority)
	require.Equal(t, "starttls", c.tlsMode)
	require.Equal(t, 30*time.Second, c.timeout)
}

func TestParseConfig_InvalidType(t *testing.T) {
	_, err := parseConfig(map[string]any{"type": "weird"})
	require.Error(t, err)
}

func TestSend_HappyPath(t *testing.T) {
	srv := newStubSMTP(t, "")
	host, port := srv.hostPort()

	p := &Plugin{}
	require.NoError(t, p.PostInit(context.Background(), nullHost{}))

	err := p.Send(context.Background(), snoozetypes.Record{Host: "alpha", Message: "boom"}, plugins.NotificationPayload{
		Meta: map[string]any{
			"host":    host,
			"port":    port,
			"from":    "noc@example.com",
			"to":      "ops@example.com, sre@example.com",
			"subject": "Alert: {{.Host}}",
			"message": "Got: {{.Message}}",
		},
	})
	require.NoError(t, err)

	_, from, to, data := srv.cap.snapshot()
	require.Equal(t, "noc@example.com", from)
	require.Equal(t, []string{"ops@example.com", "sre@example.com"}, to)
	require.Contains(t, data, "Subject: Alert: alpha")
	require.Contains(t, data, "From: noc@example.com")
	require.Contains(t, data, "To: ops@example.com, sre@example.com")
	require.Contains(t, data, "Got: boom")
}

func TestSend_PayloadOverridesTemplates(t *testing.T) {
	srv := newStubSMTP(t, "")
	host, port := srv.hostPort()
	p := &Plugin{}

	err := p.Send(context.Background(), snoozetypes.Record{Host: "alpha"}, plugins.NotificationPayload{
		Subject: "Explicit subject",
		Body:    "Explicit body",
		Meta: map[string]any{
			"host":    host,
			"port":    port,
			"from":    "noc@example.com",
			"to":      "ops@example.com",
			"subject": "would-be-rendered",
			"message": "would-be-rendered",
		},
	})
	require.NoError(t, err)

	_, _, _, data := srv.cap.snapshot()
	require.Contains(t, data, "Subject: Explicit subject")
	require.Contains(t, data, "Explicit body")
	require.NotContains(t, data, "would-be-rendered")
}

func TestSend_CcAndBccRoutingAndHeaders(t *testing.T) {
	srv := newStubSMTP(t, "")
	host, port := srv.hostPort()
	p := &Plugin{}

	err := p.Send(context.Background(), snoozetypes.Record{Host: "alpha"}, plugins.NotificationPayload{
		Subject: "subj",
		Body:    "body",
		Meta: map[string]any{
			"host": host,
			"port": port,
			"from": "noc@example.com",
			"to":   "ops@example.com",
			"cc":   "team@example.com",
			"bcc":  "audit@example.com",
		},
	})
	require.NoError(t, err)

	_, _, to, data := srv.cap.snapshot()
	require.ElementsMatch(t,
		[]string{"ops@example.com", "team@example.com", "audit@example.com"},
		to)
	require.Contains(t, data, "To: ops@example.com")
	require.Contains(t, data, "Cc: team@example.com")
	// BCC must not appear in headers, only in the SMTP envelope.
	require.NotContains(t, data, "audit@example.com")
}

func TestSend_NoRecipients(t *testing.T) {
	p := &Plugin{}
	err := p.Send(context.Background(), snoozetypes.Record{}, plugins.NotificationPayload{
		Meta: map[string]any{"from": "noc@example.com"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no recipients")
}

func TestSend_ServerRejectsMail(t *testing.T) {
	srv := newStubSMTP(t, "MAIL")
	host, port := srv.hostPort()
	p := &Plugin{}

	err := p.Send(context.Background(), snoozetypes.Record{Host: "alpha"}, plugins.NotificationPayload{
		Subject: "s", Body: "b",
		Meta: map[string]any{
			"host": host, "port": port,
			"from": "noc@example.com",
			"to":   "ops@example.com",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "MAIL FROM")
}

func TestSend_ConnectionRefused(t *testing.T) {
	// Bind a port, immediately close — the OS will refuse connections.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	p := &Plugin{}
	err = p.Send(context.Background(), snoozetypes.Record{}, plugins.NotificationPayload{
		Subject: "s", Body: "b",
		Meta: map[string]any{
			"host":    "127.0.0.1",
			"port":    port,
			"from":    "noc@example.com",
			"to":      "ops@example.com",
			"timeout": 2,
		},
	})
	require.Error(t, err)
	var opErr *net.OpError
	// We don't pin the exact error class — just that dial failed.
	if errors.As(err, &opErr) {
		require.True(t, strings.Contains(err.Error(), "dial"))
	} else {
		require.Contains(t, err.Error(), "dial")
	}
}

func TestBuildMessage_Headers(t *testing.T) {
	cfg := smtpConfig{from: "noc@example.com", priority: 2, mtype: "html"}
	msg := buildMessage(cfg, []string{"a@x"}, []string{"b@y"}, "Subj", "<p>hi</p>")
	s := string(msg)
	require.Contains(t, s, "From: noc@example.com\r\n")
	require.Contains(t, s, "To: a@x\r\n")
	require.Contains(t, s, "Cc: b@y\r\n")
	require.Contains(t, s, "Subject: Subj\r\n")
	require.Contains(t, s, "X-Priority: 2\r\n")
	require.Contains(t, s, "Content-Type: text/html;")
}

// Sanity: stringer-style fmt fallback for non-string meta values.
type stringy struct{ v string }

func (s stringy) String() string { return s.v }

func TestMetaString_Stringer(t *testing.T) {
	v, ok := metaString(map[string]any{"k": stringy{v: "hello"}}, "k")
	require.True(t, ok)
	require.Equal(t, "hello", v)
}

func TestMetaInt_Floats(t *testing.T) {
	for _, in := range []any{int(5), int32(5), int64(5), float32(5), float64(5), "5"} {
		v, ok := metaInt(map[string]any{"k": in}, "k")
		require.True(t, ok, fmt.Sprintf("%T", in))
		require.Equal(t, 5, v)
	}
	_, ok := metaInt(map[string]any{"k": "nope"}, "k")
	require.False(t, ok)
}

// portAvailable is a tiny helper used to keep the connection-refused test
// hermetic on systems where TCP_TW_REUSE policies might recycle ports.
//
//nolint:unused // retained for future tests
func portAvailable(port int) bool {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err != nil {
		return true
	}
	_ = c.Close()
	return false
}
