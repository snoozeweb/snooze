package mcp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/snoozeweb/snooze/internal/version"
	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// maxLineBytes bounds a single JSON-RPC message. MCP messages are small
// (tool calls + record payloads), but we give bufio.Scanner a generous 4 MiB
// buffer so a large list_alerts response decoded by an upstream proxy can't
// trip the default 64 KiB token limit on a round-trip.
const maxLineBytes = 4 << 20

// Daemon wires the JSON-RPC Server to stdio and owns the snooze REST client.
// It is the unit cmd/snooze-mcp orchestrates.
type Daemon struct {
	cfg    Config
	logger *slog.Logger
	server *Server

	// in/out are the protocol streams. Defaults to os.Stdin/os.Stdout; tests
	// override them.
	in  io.Reader
	out io.Writer
}

// New builds a Daemon from a validated Config. It constructs the snoozeclient
// but performs no network I/O — Run does the work.
func New(cfg Config, logger *slog.Logger) (*Daemon, error) {
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	sc, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:  cfg.Server,
		Username: cfg.Username,
		Password: cfg.Password,
		Method:   cfg.Method,
		Token:    cfg.Token,
		Insecure: cfg.Insecure,
		Timeout:  cfg.RequestTimeout,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: build snooze client: %w", err)
	}

	return &Daemon{
		cfg:    cfg,
		logger: logger,
		server: NewServer(sc, version.Version, logger),
		in:     os.Stdin,
		out:    os.Stdout,
	}, nil
}

// Run reads newline-delimited JSON-RPC messages from stdin and writes one
// compact JSON response per line to stdout, until EOF or ctx cancellation
// (SIGINT/SIGTERM). It NEVER writes diagnostics to stdout — that stream is
// the protocol channel; logging goes to the logger (stderr).
//
// Because bufio.Scanner has no context-aware Read, cancellation is handled by
// a watchdog goroutine that closes stdin (when it's an *os.File) so the
// blocking Scan returns. EOF is the normal shutdown path: an MCP client
// terminates the server by closing its stdin.
func (d *Daemon) Run(ctx context.Context) error {
	// Best-effort eager login so the first tool call doesn't pay the login
	// latency. A failure is non-fatal: snoozeclient lazily re-logs in on 401.
	if d.cfg.Token == "" && d.cfg.Username != "" {
		if sc, ok := any(d.server.api).(*snoozeclient.Client); ok {
			if err := sc.Login(ctx); err != nil {
				d.logger.Warn("mcp: snooze login failed; will retry lazily", slog.Any("err", err))
			}
		}
	}

	// Close stdin on cancellation so a blocked Scan unblocks. We only do this
	// for the real *os.File; tests pass an in-memory reader and rely on EOF.
	if closer, ok := d.in.(*os.File); ok {
		go func() {
			<-ctx.Done()
			_ = closer.Close()
		}()
	}

	scanner := bufio.NewScanner(d.in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	w := bufio.NewWriter(d.out)
	d.logger.Info("mcp: serving JSON-RPC over stdio", slog.String("version", version.Version))

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(trimSpace(line)) == 0 {
			continue
		}
		// Copy the slice: Handle may outlive the next Scan which reuses the
		// scanner's buffer.
		raw := make([]byte, len(line))
		copy(raw, line)

		resp := d.server.Handle(ctx, raw)
		if resp == nil {
			continue // notification — nothing to write
		}
		if _, err := w.Write(resp); err != nil {
			return fmt.Errorf("mcp: write response: %w", err)
		}
		if err := w.WriteByte('\n'); err != nil {
			return fmt.Errorf("mcp: write newline: %w", err)
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("mcp: flush: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		// A scan error after cancellation is the watchdog closing stdin —
		// report the context error so the caller treats it as a clean stop.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("mcp: read stdin: %w", err)
	}
	return nil
}

// trimSpace reports the byte slice with leading/trailing ASCII whitespace
// removed, without allocating (used only to detect blank lines).
func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && isSpace(b[start]) {
		start++
	}
	end := len(b)
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
