package syslog

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"golang.org/x/sync/errgroup"
)

// tcpAcceptDeadline is applied to Accept so the listener can notice ctx
// cancellation. Stdlib's net.Listener has no context-aware Accept.
const tcpAcceptDeadline = 250 * time.Millisecond

// tcpLineMax caps a single syslog line. 64KiB matches the UDP buffer; RFC5424
// recommends supporting at least 8KiB but real-world payloads from Cisco gear
// and structured-data-heavy emitters can push past that.
const tcpLineMax = 64 * 1024

// TCPListener accepts inbound TCP connections, reads newline-delimited syslog
// messages from each, and forwards them. RFC6587 octet-framing is NOT
// implemented in this first cut — most production senders use LF framing.
type TCPListener struct {
	addr      string
	listener  net.Listener
	parser    *MessageParser
	forwarder *Forwarder
	logger    *slog.Logger
}

// NewTCPListener binds to addr and returns a listener ready for Run.
func NewTCPListener(addr string, parser *MessageParser, fwd *Forwarder, logger *slog.Logger) (*TCPListener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("syslog: listen tcp %q: %w", addr, err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TCPListener{
		addr:      addr,
		listener:  ln,
		parser:    parser,
		forwarder: fwd,
		logger:    logger,
	}, nil
}

// LocalAddr returns the bound address. Useful for tests that bind to ":0".
func (l *TCPListener) LocalAddr() net.Addr {
	if l.listener == nil {
		return nil
	}
	return l.listener.Addr()
}

// Run accepts connections until ctx is cancelled. Each accepted connection is
// processed on its own goroutine via the embedded errgroup, and Run waits for
// all in-flight handlers before returning.
func (l *TCPListener) Run(ctx context.Context) error {
	group, gctx := errgroup.WithContext(ctx)

	// Close the listener when ctx fires so any in-flight Accept exits.
	group.Go(func() error {
		<-gctx.Done()
		_ = l.listener.Close()
		return gctx.Err()
	})

	for {
		if err := ctx.Err(); err != nil {
			// Wait for the closer goroutine + active handlers.
			_ = group.Wait()
			return err
		}
		if d, ok := l.listener.(interface{ SetDeadline(time.Time) error }); ok {
			_ = d.SetDeadline(time.Now().Add(tcpAcceptDeadline))
		}
		conn, err := l.listener.Accept()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				_ = group.Wait()
				return ctx.Err()
			}
			l.logger.Warn("syslog: tcp accept", slog.Any("err", err))
			continue
		}
		group.Go(func() error {
			l.handleConn(gctx, conn)
			return nil
		})
	}
}

// handleConn reads newline-delimited messages from one TCP connection until
// EOF, ctx cancellation, or a transport error.
func (l *TCPListener) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	peer := conn.RemoteAddr().String()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), tcpLineMax)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Re-use a slice copy to decouple the scanner buffer from downstream.
		data := make([]byte, len(line))
		copy(data, line)
		msg, err := l.parser.Parse(data)
		if err != nil {
			l.logger.Warn("syslog: parse tcp", slog.String("peer", peer), slog.Any("err", err))
			continue
		}
		if err := l.forwarder.Forward(ctx, msg, peer); err != nil {
			l.logger.Warn("syslog: forward tcp", slog.String("peer", peer), slog.Any("err", err))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		l.logger.Debug("syslog: tcp scan done", slog.String("peer", peer), slog.Any("err", err))
	}
}
