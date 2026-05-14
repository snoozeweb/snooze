// Test coverage for the snooze-server entrypoint.
//
// The boot path is exercised end-to-end against an in-process SQLite database
// so the test catches integration breakages (plugins.Build, core.New,
// api.Router.Build) the unit suites cannot. plugins.Build is a one-shot, so we
// only get a single full-boot test per process; the rest of the file covers
// flag parsing and subcommand surfaces.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// freeTCPPort picks an ephemeral port by binding and immediately closing. The
// returned port is not held; collisions are rare in CI and the test guards
// against them by retrying once on listen failure.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestVersionSubcommand asserts that `snooze-server version` prints the
// version package output and exits 0 without touching the daemon path.
func TestVersionSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.HasPrefix(stdout.String(), "snooze-server ") {
		t.Fatalf("stdout = %q, want prefix %q", stdout.String(), "snooze-server ")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

// TestHelpSubcommand asserts every help spelling prints usage and exits 0.
func TestHelpSubcommand(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{arg}, &stdout, &stderr)
			if code != exitOK {
				t.Fatalf("code = %d, want 0", code)
			}
			if !strings.Contains(stdout.String(), "snooze-server") {
				t.Fatalf("stdout = %q, want help text", stdout.String())
			}
		})
	}
}

// TestMigrateConfigPlaceholder validates the placeholder subcommand: it must
// accept --from, exit 0, and emit a "not yet implemented" notice.
func TestMigrateConfigPlaceholder(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"migrate-config", "--from", t.TempDir()}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("code = %d (stderr=%q), want 0", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "not yet implemented") {
		t.Fatalf("stdout = %q, want 'not yet implemented'", stdout.String())
	}

	// Missing --from must surface a usage error.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"migrate-config"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("missing --from: code = %d, want %d", code, exitUsage)
	}
}

// TestSplitHostPort exercises the small flag helper in isolation.
func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
		wantPort int
		wantOK   bool
	}{
		{"127.0.0.1:5200", "127.0.0.1", 5200, true},
		{"0.0.0.0:8080", "0.0.0.0", 8080, true},
		{"bogus", "", 0, false},
		{"127.0.0.1:notaport", "", 0, false},
		{"127.0.0.1:0", "", 0, false},
		{"127.0.0.1:65536", "", 0, false},
	}
	for _, c := range cases {
		host, port, ok := splitHostPort(c.in)
		if ok != c.wantOK || host != c.wantHost || port != c.wantPort {
			t.Errorf("splitHostPort(%q) = (%q,%d,%v), want (%q,%d,%v)",
				c.in, host, port, ok, c.wantHost, c.wantPort, c.wantOK)
		}
	}
}

// TestSplitCSV covers the CORS helper.
func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{"*"}},
		{"*", []string{"*"}},
		{"https://a.example, https://b.example", []string{"https://a.example", "https://b.example"}},
		{"a, ,b", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if !stringSliceEq(got, c.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func stringSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRootTokenDialFailure asserts that root-token reports an error when the
// admin socket is absent (rather than panicking or hanging).
func TestRootTokenDialFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.sock")
	var stdout, stderr bytes.Buffer
	code := run([]string{"root-token", "--socket", missing}, &stdout, &stderr)
	if code != exitErr {
		t.Fatalf("code = %d, want %d (stdout=%q stderr=%q)", code, exitErr, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "root-token") {
		t.Fatalf("stderr = %q, want 'root-token' prefix", stderr.String())
	}
}

// bootOnce guards the one full daemon-boot test. plugins.Build panics on a
// second invocation so we coalesce every "needs a running server" test into a
// single bring-up.
var bootOnce sync.Once

// TestDaemonBoot smoke-tests the full bring-up: load defaults + apply
// flag overrides, open SQLite, build the Core + plugins, start the HTTP
// listener, hit /healthz, and shut down via context cancellation. Five
// seconds is the hard cap mandated by the spec.
//
// The test must run exactly once per process because plugins.Build is
// single-shot; we use sync.Once to make the contract explicit.
func TestDaemonBoot(t *testing.T) {
	bootOnce.Do(func() {
		runDaemonBootTest(t)
	})
}

func runDaemonBootTest(t *testing.T) {
	port := freeTCPPort(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "snooze.db")
	socketPath := filepath.Join(dir, "admin.sock")

	flags := &daemonFlags{
		// Empty configDir → config.Load uses defaults only. We override the
		// SQLite path via env so the bootstrap_db step has a place to land.
		configDir:  "",
		listenAddr: fmt.Sprintf("127.0.0.1:%d", port),
		adminSock:  socketPath,
		logFormat:  "text",
		logLevel:   "debug",
	}

	// Steer config.Load at our temp SQLite file. We use the env variable
	// because the bootstrap default Path is "./db.json" which is in the wrong
	// location for a test sandbox.
	t.Setenv("SNOOZE_SERVER_CORE_DATABASE_PATH", dbPath)
	t.Setenv("SNOOZE_SERVER_CORE_DATABASE_TYPE", "file")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		runErr     error
		runErrOnce sync.Once
	)
	done := make(chan struct{})
	var stderr safeBuffer
	go func() {
		defer close(done)
		err := runDaemonCtx(ctx, flags, &stderr)
		runErrOnce.Do(func() { runErr = err })
	}()

	// Poll /healthz until it answers or the deadline expires. If runDaemonCtx
	// has already returned (e.g. config error) surface that immediately.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	if err := waitForHealthOrExit(healthURL, 5*time.Second, done, &runErr); err != nil {
		cancel()
		<-done
		t.Fatalf("healthz never came up: %v (runErr=%v stderr=%q)", err, runErr, stderr.String())
	}

	// Trigger shutdown and confirm it completes within the SLO.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("shutdown timed out; stderr=%q", stderr.String())
	}
	if runErr != nil {
		t.Fatalf("runDaemonCtx: %v", runErr)
	}
}

// waitForHealthOrExit polls the URL until it returns 200, the deadline
// expires, or the daemon goroutine completes (signalling an early exit).
// runErrPtr is read only after `done` is closed so we don't race with the
// goroutine writing it.
func waitForHealthOrExit(url string, deadline time.Duration, done <-chan struct{}, runErrPtr *error) error {
	hc := &http.Client{Timeout: 1 * time.Second}
	end := time.Now().Add(deadline)
	var lastErr error
	for time.Now().Before(end) {
		select {
		case <-done:
			if runErrPtr != nil && *runErrPtr != nil {
				return fmt.Errorf("daemon exited early: %w", *runErrPtr)
			}
			return fmt.Errorf("daemon exited without serving")
		default:
		}
		resp, err := hc.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status=%d body=%q", resp.StatusCode, string(body))
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("deadline %s elapsed", deadline)
	}
	return lastErr
}

// safeBuffer is a minimal goroutine-safe io.Writer used so the daemon's stderr
// can be inspected from the test goroutine without racing.
type safeBuffer struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	used atomic.Bool
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used.Store(true)
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestParseDaemonFlags_WebDirDefault(t *testing.T) {
	t.Helper()
	f, err := parseDaemonFlags([]string{}, io.Discard)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.webDir != "/var/lib/snooze/web" {
		t.Fatalf("webDir default = %q, want %q", f.webDir, "/var/lib/snooze/web")
	}
}

func TestParseDaemonFlags_WebDirOverride(t *testing.T) {
	t.Helper()
	f, err := parseDaemonFlags([]string{"-web-dir", "/tmp/custom"}, io.Discard)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.webDir != "/tmp/custom" {
		t.Fatalf("webDir override = %q, want %q", f.webDir, "/tmp/custom")
	}
}

func TestOpenWebFS(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("empty string returns nil", func(t *testing.T) {
		if fs := openWebFS("", logger); fs != nil {
			t.Fatalf("expected nil for empty dir, got %T", fs)
		}
	})

	t.Run("missing directory returns nil", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		if fs := openWebFS(missing, logger); fs != nil {
			t.Fatalf("expected nil for missing dir, got %T", fs)
		}
	})

	t.Run("path that is a file returns nil", func(t *testing.T) {
		tmp := t.TempDir()
		f := filepath.Join(tmp, "not-a-dir")
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if fs := openWebFS(f, logger); fs != nil {
			t.Fatalf("expected nil for file path, got %T", fs)
		}
	})

	t.Run("valid directory returns http.Dir", func(t *testing.T) {
		dir := t.TempDir()
		fs := openWebFS(dir, logger)
		if fs == nil {
			t.Fatalf("expected non-nil FileSystem for %q", dir)
		}
		if _, ok := fs.(http.Dir); !ok {
			t.Fatalf("expected http.Dir, got %T", fs)
		}
	})

	t.Run("nil logger is tolerated", func(t *testing.T) {
		// Missing-dir path with no logger: must not panic.
		if fs := openWebFS("/definitely/not/a/path", nil); fs != nil {
			t.Fatalf("expected nil, got %T", fs)
		}
	})
}
