package syslog_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

	syslogcomp "github.com/japannext/snooze/internal/components/syslog"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// fakeBackend is an httptest server that captures every alert payload posted
// to /api/v1/alerts. It also exposes a synchronous "wait for N" helper so the
// concurrent listener tests don't race against the forwarder goroutine.
type fakeBackend struct {
	srv  *httptest.Server
	mu   sync.Mutex
	recs []snoozetypes.Record
	got  int32
}

func newFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	b := &fakeBackend{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/login/local", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"ok","expires_at":"2099-01-01T00:00:00Z","method":"local"}`))
	})
	mux.HandleFunc("/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var rec snoozetypes.Record
		if err := json.Unmarshal(body, &rec); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		b.mu.Lock()
		b.recs = append(b.recs, rec)
		b.mu.Unlock()
		atomic.AddInt32(&b.got, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"0001"}]}`))
	})
	b.srv = httptest.NewServer(mux)
	t.Cleanup(b.srv.Close)
	return b
}

// waitFor polls until at least n records have been ingested or the timeout
// elapses; it returns a snapshot of what was captured.
func (b *fakeBackend) waitFor(t *testing.T, n int, timeout time.Duration) []snoozetypes.Record {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if int(atomic.LoadInt32(&b.got)) >= n {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]snoozetypes.Record, len(b.recs))
	copy(out, b.recs)
	return out
}

// quietLogger discards listener output so test runs aren't noisy.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// startDaemon writes a temp YAML config pointing at backend and brings up a
// Daemon listening on ephemeral ports. It returns the running daemon plus a
// cancel func; the daemon's Run goroutine is joined in t.Cleanup.
func startDaemon(t *testing.T, backend *fakeBackend, parser string) (*syslogcomp.Daemon, context.CancelFunc) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "syslog.yaml")
	yaml := fmt.Sprintf(`
server: %q
username: ingest
password: secret
listen_udp: 127.0.0.1:0
listen_tcp: 127.0.0.1:0
parser: %s
request_timeout: 2s
`, backend.srv.URL, parser)
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0o600))

	cfg, err := syslogcomp.LoadConfig(cfgPath)
	require.NoError(t, err)
	d, err := syslogcomp.New(cfg, quietLogger())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case <-runErr:
		case <-time.After(3 * time.Second):
			t.Errorf("daemon did not shut down")
		}
	})
	return d, cancel
}

func TestParserRFC5424RoundTrip(t *testing.T) {
	p, err := syslogcomp.NewParser("auto")
	require.NoError(t, err)
	line := []byte(`<165>1 2003-10-11T22:14:15.003Z mymachine.example.com evntslog - ID47 [exampleSDID@32473 iut="3" eventSource="Application"] An application event log entry`)
	msg, err := p.Parse(line)
	require.NoError(t, err)
	require.Equal(t, "rfc5424", msg.Format)
	require.Equal(t, "mymachine.example.com", msg.Hostname)
	require.Equal(t, "evntslog", msg.AppName)
	require.Equal(t, "notice", msg.Severity) // pri=165: facility=20 local4, severity=5
	require.True(t, msg.HasTime)
	require.Equal(t, 2003, msg.Timestamp.Year())
	require.Equal(t, "An application event log entry", msg.Message)
	require.Contains(t, msg.Structured, "exampleSDID@32473")

	rec := syslogcomp.ToRecord(msg, "10.0.0.1:5000")
	require.Equal(t, "syslog", rec.Source)
	require.Equal(t, "evntslog", rec.Process)
	require.Equal(t, "notice", rec.Severity)
	require.Equal(t, "10.0.0.1:5000", rec.Raw["peer"])
	require.Equal(t, "rfc5424", rec.Raw["format"])
}

func TestParserRFC3164RoundTrip(t *testing.T) {
	p, err := syslogcomp.NewParser("auto")
	require.NoError(t, err)
	line := []byte(`<34>Oct 11 22:14:15 mymachine su[230]: 'su root' failed for lonvick on /dev/pts/8`)
	msg, err := p.Parse(line)
	require.NoError(t, err)
	require.Equal(t, "rfc3164", msg.Format)
	require.Equal(t, "mymachine", msg.Hostname)
	require.Equal(t, "su", msg.AppName)
	require.Equal(t, "230", msg.ProcID)
	require.Equal(t, "crit", msg.Severity) // pri=34: facility=4 auth, severity=2
	require.Contains(t, msg.Message, "'su root' failed for lonvick")
	rec := syslogcomp.ToRecord(msg, "")
	require.Equal(t, "su", rec.Process)
}

func TestParserAutoDetect(t *testing.T) {
	p, err := syslogcomp.NewParser("auto")
	require.NoError(t, err)
	// 5424 (has VERSION "1" after the priority)
	m5, err := p.Parse([]byte(`<13>1 2024-01-02T03:04:05Z host app - - - hello`))
	require.NoError(t, err)
	require.Equal(t, "rfc5424", m5.Format)
	// 3164 (no version digit)
	m3, err := p.Parse([]byte(`<13>Jan  2 03:04:05 host app: hello`))
	require.NoError(t, err)
	require.Equal(t, "rfc3164", m3.Format)
}

func TestUDPListenerForwards(t *testing.T) {
	backend := newFakeBackend(t)
	d, _ := startDaemon(t, backend, "auto")

	udpAddr, err := net.ResolveUDPAddr("udp", d.UDPAddr())
	require.NoError(t, err)
	conn, err := net.DialUDP("udp", nil, udpAddr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = conn.Write([]byte(`<165>1 2023-04-05T06:07:08Z myhost myapp 42 EVT1 - hello over udp`))
	require.NoError(t, err)

	recs := backend.waitFor(t, 1, 2*time.Second)
	require.Len(t, recs, 1)
	require.Equal(t, "myhost", recs[0].Host)
	require.Equal(t, "myapp", recs[0].Process)
	require.Equal(t, "syslog", recs[0].Source)
	require.Equal(t, "hello over udp", recs[0].Message)
}

func TestTCPListenerForwards(t *testing.T) {
	backend := newFakeBackend(t)
	d, _ := startDaemon(t, backend, "auto")

	conn, err := net.Dial("tcp", d.TCPAddr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	// Send two messages in one connection — TCP framing is LF-delimited.
	w := bufio.NewWriter(conn)
	_, err = w.WriteString("<14>Jan  2 03:04:05 srv-a daemon: tcp message one\n")
	require.NoError(t, err)
	_, err = w.WriteString("<14>1 2024-05-06T07:08:09Z srv-b app2 - - - tcp message two\n")
	require.NoError(t, err)
	require.NoError(t, w.Flush())

	recs := backend.waitFor(t, 2, 2*time.Second)
	require.Len(t, recs, 2)
	// recs[0] is 3164, recs[1] is 5424 (deterministic since both come over the
	// same connection and the handler processes them serially).
	require.Equal(t, "srv-a", recs[0].Host)
	require.Equal(t, "daemon", recs[0].Process)
	require.Equal(t, "info", recs[0].Severity)
	require.Equal(t, "srv-b", recs[1].Host)
	require.Equal(t, "app2", recs[1].Process)
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "syslog.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
server: https://snooze.example/
username: u
password: p
listen_udp: 0.0.0.0:0
`), 0o600))
	cfg, err := syslogcomp.LoadConfig(cfgPath)
	require.NoError(t, err)
	require.Equal(t, "auto", cfg.Parser)
	require.Equal(t, 10*time.Second, cfg.RequestTimeout)
	require.True(t, strings.HasSuffix(cfg.Server, "/"))
}

func TestLoadConfigRejectsMissingListener(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "syslog.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
server: https://snooze.example/
`), 0o600))
	_, err := syslogcomp.LoadConfig(cfgPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "listen_udp")
}
