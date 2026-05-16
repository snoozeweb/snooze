package smtp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestConfig_WithDefaults(t *testing.T) {
	t.Run("requires server", func(t *testing.T) {
		_, err := Config{Listen: "127.0.0.1:25"}.WithDefaults()
		require.Error(t, err)
	})
	t.Run("fills defaults", func(t *testing.T) {
		c, err := Config{Server: "http://x"}.WithDefaults()
		require.NoError(t, err)
		require.Equal(t, "0.0.0.0:25", c.Listen)
		require.Equal(t, []string{"*"}, c.AllowedSenders)
		require.Equal(t, int64(10*1024*1024), c.MaxMessageBytes)
		require.NotZero(t, c.Hostname)
	})
	t.Run("tls cert+key paired", func(t *testing.T) {
		_, err := Config{Server: "http://x", TLSCert: "/tmp/c"}.WithDefaults()
		require.Error(t, err)
	})
	t.Run("auth_required needs user", func(t *testing.T) {
		_, err := Config{Server: "http://x", AuthRequired: true}.WithDefaults()
		require.Error(t, err)
	})
}

func TestSenderAllowed(t *testing.T) {
	cfg := Config{AllowedSenders: []string{"alerts@example.com", "*@monitoring.example.com", "*@*.partner.io"}}
	require.True(t, cfg.senderAllowed("alerts@example.com"))
	require.True(t, cfg.senderAllowed("ALERTS@EXAMPLE.COM"))
	require.True(t, cfg.senderAllowed("anyone@monitoring.example.com"))
	require.True(t, cfg.senderAllowed("nagios@host1.partner.io"))
	require.False(t, cfg.senderAllowed("evil@elsewhere.org"))
	require.False(t, cfg.senderAllowed("plain@partner.io"))

	wild := Config{AllowedSenders: []string{"*"}}
	require.True(t, wild.senderAllowed("anything@anywhere.io"))
}

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "smtp.yaml")
	body := []byte(`
server: https://snooze.example/
username: ingest
password: hunter2
listen: 127.0.0.1:2525
allowed_senders:
  - "*@example.com"
local_domains:
  - "example.com"
`)
	require.NoError(t, writeFile(path, body))
	c, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example/", c.Server)
	require.Equal(t, "ingest", c.Username)
	require.Equal(t, "127.0.0.1:2525", c.Listen)
	require.Equal(t, []string{"*@example.com"}, c.AllowedSenders)
}

func TestParseDate(t *testing.T) {
	got := parseDate("Tue, 19 Aug 2025 10:30:00 +0000")
	require.False(t, got.IsZero())
	require.Equal(t, 2025, got.UTC().Year())

	now := parseDate("")
	require.WithinDuration(t, time.Now(), now, 2*time.Second)
}

func TestGuessSeverity(t *testing.T) {
	require.Equal(t, "crit", guessSeverity("[CRITICAL] disk full"))
	require.Equal(t, "warning", guessSeverity("WARNING: cpu hot"))
	require.Equal(t, "err", guessSeverity("Error occurred"))
	require.Equal(t, "info", guessSeverity("[INFO] all good"))
	require.Equal(t, "info", guessSeverity("Backup OK"))
	require.Equal(t, "warning", guessSeverity("Hello world")) // default
}

func TestStripHTML(t *testing.T) {
	s := stripHTML("<p>hello <b>world</b></p>")
	require.Equal(t, "hello world", s)
	require.Equal(t, "a < b > c", stripHTML("a &lt; b &gt; c"))
}

func TestParseReceivedFrom(t *testing.T) {
	require.Equal(t,
		"alerthost.example.com",
		parseReceivedFrom("from alerthost.example.com (alerthost [10.0.0.5]) by snoozesmtp.example.com with ESMTP id ABC; Mon, 1 Jan 2024 00:00:00 +0000"))
	require.Empty(t, parseReceivedFrom(""))
}

func TestPickMultipart(t *testing.T) {
	boundary := "B"
	raw := "--B\r\nContent-Type: text/html\r\n\r\n<p>hi</p>\r\n--B\r\nContent-Type: text/plain\r\n\r\nplain body\r\n--B--\r\n"
	got := pickMultipart([]byte(raw), boundary)
	require.Equal(t, "plain body", strings.TrimSpace(got))

	htmlOnly := "--B\r\nContent-Type: text/html\r\n\r\n<b>only html</b>\r\n--B--\r\n"
	got = pickMultipart([]byte(htmlOnly), boundary)
	require.Contains(t, got, "only html")
}

func TestToRecord_BasicMapping(t *testing.T) {
	cfg, err := Config{Server: "http://x", LocalDomains: []string{"example.com"}}.WithDefaults()
	require.NoError(t, err)
	mailBody := []byte("From: alice@host1.example.com\r\n" +
		"To: alerts@snooze.local\r\n" +
		"Subject: [CRITICAL] disk full on host1\r\n" +
		"Date: Tue, 19 Aug 2025 10:30:00 +0000\r\n" +
		"Content-Type: text/plain; charset=us-ascii\r\n" +
		"\r\n" +
		"/var is at 99%.\r\n",
	)
	msg := ReceivedMessage{
		MailFrom: "alice@host1.example.com",
		RcptTo:   []string{"alerts@snooze.local"},
		Peer:     "10.0.0.5:54321",
		Helo:     "host1",
		Data:     mailBody,
	}
	rec, err := ToRecord(msg, cfg)
	require.NoError(t, err)
	require.Equal(t, "host1", rec.Host) // stripped from host1.example.com
	require.Equal(t, "smtp", rec.Source)
	require.Equal(t, "[CRITICAL] disk full on host1", rec.Process)
	require.Equal(t, "crit", rec.Severity)
	require.Contains(t, rec.Message, "/var is at 99%")
	require.Equal(t, 2025, rec.Timestamp.UTC().Year())
	require.Equal(t, "alice@host1.example.com", rec.Raw["from"])
	require.Equal(t, "host1.example.com", rec.Raw["fqdn"])
	headers, ok := rec.Raw["headers"].(map[string]string)
	require.True(t, ok)
	require.Contains(t, headers, "Subject")
}

func TestToRecord_FallbackToReceivedHeader(t *testing.T) {
	cfg, err := Config{Server: "http://x"}.WithDefaults()
	require.NoError(t, err)
	mailBody := []byte("From: monitor\r\n" +
		"Subject: ping\r\n" +
		"Received: from sender.example.com (sender [10.0.0.5]) by smtp.example.com\r\n" +
		"\r\n" +
		"body\r\n",
	)
	rec, err := ToRecord(ReceivedMessage{MailFrom: "no-at-sign", Data: mailBody}, cfg)
	require.NoError(t, err)
	// No '@' in MAIL FROM → fall back to the "from <host>" in Received.
	require.Equal(t, "sender.example.com", rec.Host)
}

func TestParsePathArg(t *testing.T) {
	addr, ok := parsePathArg("FROM:<alice@example.com>", "FROM")
	require.True(t, ok)
	require.Equal(t, "alice@example.com", addr)

	addr, ok = parsePathArg("FROM:<a@b> SIZE=12345 BODY=8BITMIME", "FROM")
	require.True(t, ok)
	require.Equal(t, "a@b", addr)

	addr, ok = parsePathArg("TO:<bob@example.com>", "TO")
	require.True(t, ok)
	require.Equal(t, "bob@example.com", addr)

	_, ok = parsePathArg("FROM:alice@example.com", "FROM")
	require.False(t, ok)
}

func TestReadDataBlob_DotStuffing(t *testing.T) {
	in := "line one\r\n..dotted\r\n.\r\n"
	br := bufio.NewReader(strings.NewReader(in))
	got, err := readDataBlob(br, 1024)
	require.NoError(t, err)
	require.Equal(t, "line one\r\n.dotted\r\n", string(got))
}

func TestReadDataBlob_TooLarge(t *testing.T) {
	in := strings.Repeat("a", 50) + "\r\n.\r\n"
	br := bufio.NewReader(strings.NewReader(in))
	_, err := readDataBlob(br, 10)
	require.ErrorIs(t, err, errMessageTooLarge)
}

// --- end-to-end: real TCP dialog ------------------------------------------

// fakeAlertServer captures POST /api/v1/alerts payloads.
type fakeAlertServer struct {
	mu      sync.Mutex
	srv     *httptest.Server
	posted  []snoozetypes.Record
	loginOK atomic.Bool
}

func newFakeAlertServer(t *testing.T) *fakeAlertServer {
	t.Helper()
	f := &fakeAlertServer{}
	f.loginOK.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/login/local", func(w http.ResponseWriter, _ *http.Request) {
		if !f.loginOK.Load() {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "tok-123",
			"expires_at": time.Now().Add(time.Hour),
			"method":     "local",
		})
	})
	mux.HandleFunc("/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		var rec snoozetypes.Record
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rec))
		f.mu.Lock()
		f.posted = append(f.posted, rec)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"uid":     "abc",
				"host":    rec.Host,
				"source":  rec.Source,
				"process": rec.Process,
			}},
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAlertServer) records() []snoozetypes.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]snoozetypes.Record, len(f.posted))
	copy(out, f.posted)
	return out
}

// dialAndSend speaks plain SMTP through a net.Conn and pushes one mail.
// Returns the final reply for the QUIT verb.
func dialAndSend(t *testing.T, addr, from, to, data string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	br := bufio.NewReader(conn)
	write := func(line string) {
		_, err := fmt.Fprintf(conn, "%s\r\n", line)
		require.NoError(t, err)
	}
	expect := func(want int) string {
		line, err := br.ReadString('\n')
		require.NoError(t, err)
		require.Truef(t, strings.HasPrefix(line, fmt.Sprintf("%d", want)), "expected %d, got %q", want, line)
		// Consume any continuation lines (250-...).
		for strings.HasPrefix(line, fmt.Sprintf("%d-", want)) {
			line, err = br.ReadString('\n')
			require.NoError(t, err)
		}
		return line
	}

	expect(220)
	write("EHLO test.local")
	expect(250)
	write("MAIL FROM:<" + from + ">")
	expect(250)
	write("RCPT TO:<" + to + ">")
	expect(250)
	write("DATA")
	expect(354)
	_, err = fmt.Fprint(conn, data)
	require.NoError(t, err)
	_, err = fmt.Fprint(conn, "\r\n.\r\n")
	require.NoError(t, err)
	expect(250)
	write("QUIT")
	expect(221)
}

func TestDaemon_EndToEnd(t *testing.T) {
	fake := newFakeAlertServer(t)

	cfg, err := Config{
		Server:         fake.srv.URL,
		Username:       "u",
		Password:       "p",
		Listen:         "127.0.0.1:0",
		LocalDomains:   []string{"example.com"},
		AllowedSenders: []string{"*"},
	}.WithDefaults()
	require.NoError(t, err)

	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:        fake.srv.URL,
		Username:       "u",
		Password:       "p",
		InitialBackoff: time.Millisecond,
		MaxRetries:     1,
		TokenCacheFile: filepath.Join(t.TempDir(), "tok"),
		HTTPClient:     fake.srv.Client(),
	})
	require.NoError(t, err)

	d, err := NewDaemonWithClient(cfg, client, nil)
	require.NoError(t, err)
	require.NoError(t, d.Listen())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	mail := "From: nagios@host1.example.com\r\n" +
		"To: alerts@snooze.local\r\n" +
		"Subject: [WARN] high cpu on host1\r\n" +
		"Date: Tue, 19 Aug 2025 10:30:00 +0000\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"cpu 95%"
	dialAndSend(t, d.LocalAddr(), "nagios@host1.example.com", "alerts@snooze.local", mail)

	// Wait briefly for the goroutine that handles DATA to finish posting.
	deadline := time.Now().Add(2 * time.Second)
	for len(fake.records()) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("expected alert posted within 2s")
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	<-done

	recs := fake.records()
	require.Len(t, recs, 1)
	rec := recs[0]
	require.Equal(t, "host1", rec.Host)
	require.Equal(t, "smtp", rec.Source)
	require.Equal(t, "warning", rec.Severity)
	require.Contains(t, rec.Process, "high cpu on host1")
}

func TestDaemon_RejectsDisallowedSender(t *testing.T) {
	fake := newFakeAlertServer(t)
	cfg, err := Config{
		Server:         fake.srv.URL,
		Listen:         "127.0.0.1:0",
		AllowedSenders: []string{"alerts@example.com"},
	}.WithDefaults()
	require.NoError(t, err)
	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:        fake.srv.URL,
		Token:          "tok-123",
		InitialBackoff: time.Millisecond,
		MaxRetries:     1,
		TokenCacheFile: filepath.Join(t.TempDir(), "tok"),
		HTTPClient:     fake.srv.Client(),
	})
	require.NoError(t, err)
	d, err := NewDaemonWithClient(cfg, client, nil)
	require.NoError(t, err)
	require.NoError(t, d.Listen())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	conn, err := net.DialTimeout("tcp", d.LocalAddr(), 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	_, err = br.ReadString('\n') // 220
	require.NoError(t, err)
	_, _ = fmt.Fprint(conn, "EHLO t\r\n")
	for {
		line, err := br.ReadString('\n')
		require.NoError(t, err)
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}
	_, _ = fmt.Fprint(conn, "MAIL FROM:<rogue@nope.org>\r\n")
	line, err := br.ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(line, "550"), "expected 550, got %q", line)

	require.Empty(t, fake.records())
}

func TestDaemon_AuthRequired(t *testing.T) {
	fake := newFakeAlertServer(t)
	cfg, err := Config{
		Server:       fake.srv.URL,
		Username:     "alerts",
		Password:     "shh",
		Listen:       "127.0.0.1:0",
		AuthRequired: true,
	}.WithDefaults()
	require.NoError(t, err)
	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:        fake.srv.URL,
		Token:          "tok-123",
		InitialBackoff: time.Millisecond,
		MaxRetries:     1,
		TokenCacheFile: filepath.Join(t.TempDir(), "tok"),
		HTTPClient:     fake.srv.Client(),
	})
	require.NoError(t, err)
	d, err := NewDaemonWithClient(cfg, client, nil)
	require.NoError(t, err)
	require.NoError(t, d.Listen())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	conn, err := net.DialTimeout("tcp", d.LocalAddr(), 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	_, _ = br.ReadString('\n') // 220
	_, _ = fmt.Fprint(conn, "EHLO t\r\n")
	for {
		line, _ := br.ReadString('\n')
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}
	// Without AUTH, MAIL FROM is refused with 530.
	_, _ = fmt.Fprint(conn, "MAIL FROM:<alice@example.com>\r\n")
	line, err := br.ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(line, "530"), "expected 530, got %q", line)

	// Authenticate then retry — should succeed.
	plain := base64.StdEncoding.EncodeToString([]byte("\x00alerts\x00shh"))
	_, _ = fmt.Fprintf(conn, "AUTH PLAIN %s\r\n", plain)
	line, _ = br.ReadString('\n')
	require.True(t, strings.HasPrefix(line, "235"), "expected 235, got %q", line)
	_, _ = fmt.Fprint(conn, "MAIL FROM:<alice@example.com>\r\n")
	line, _ = br.ReadString('\n')
	require.True(t, strings.HasPrefix(line, "250"), "expected 250 after auth, got %q", line)
}

// writeFile is a tiny os.WriteFile shim with 0600 perms.
func writeFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o600)
}
