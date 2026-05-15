package syslog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"
)

// udpReadDeadline is the periodic deadline applied to ReadFromUDP so the
// listener can notice ctx cancellation. UDP sockets have no native context
// integration in stdlib, so we poll.
const udpReadDeadline = 250 * time.Millisecond

// udpBufferSize is the maximum datagram length we accept. 64KiB is the
// theoretical IPv4 UDP MTU; in practice senders cap well below 8KiB but we
// pay nothing for the larger buffer.
const udpBufferSize = 64 * 1024

// UDPListener reads RFC3164/5424 datagrams from a UDP socket, parses each one
// and forwards it via the configured Forwarder.
type UDPListener struct {
	addr      string
	conn      *net.UDPConn
	parser    *MessageParser
	forwarder *Forwarder
	logger    *slog.Logger
}

// NewUDPListener binds to addr (e.g. "0.0.0.0:514") and returns a listener
// ready for Run. The socket is opened eagerly so binding errors surface before
// the daemon transitions into the run loop.
func NewUDPListener(addr string, parser *MessageParser, fwd *Forwarder, logger *slog.Logger) (*UDPListener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("syslog: resolve udp %q: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("syslog: listen udp %q: %w", addr, err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &UDPListener{
		addr:      addr,
		conn:      conn,
		parser:    parser,
		forwarder: fwd,
		logger:    logger,
	}, nil
}

// LocalAddr returns the bound address. Useful for tests that bind to ":0" and
// need to discover the kernel-assigned port.
func (l *UDPListener) LocalAddr() net.Addr {
	if l.conn == nil {
		return nil
	}
	return l.conn.LocalAddr()
}

// Run reads datagrams until ctx is cancelled. It always returns a non-nil
// error — context.Canceled / context.DeadlineExceeded for normal shutdown,
// or a wrapped network error otherwise. The socket is closed before Run
// returns.
func (l *UDPListener) Run(ctx context.Context) error {
	defer l.conn.Close() //nolint:errcheck
	buf := make([]byte, udpBufferSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = l.conn.SetReadDeadline(time.Now().Add(udpReadDeadline))
		n, peer, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				return ctx.Err()
			}
			l.logger.Warn("syslog: udp read error", slog.Any("err", err))
			continue
		}
		if n == 0 {
			continue
		}
		// Copy out of buf so the parser/forwarder don't see the next datagram.
		data := make([]byte, n)
		copy(data, buf[:n])
		l.handle(ctx, data, peer.String())
	}
}

// handle parses one datagram and forwards it. Failures are logged but never
// propagated — UDP has no flow-control feedback channel anyway.
func (l *UDPListener) handle(ctx context.Context, data []byte, peer string) {
	msg, err := l.parser.Parse(data)
	if err != nil {
		l.logger.Warn("syslog: parse udp", slog.String("peer", peer), slog.Any("err", err))
		return
	}
	if err := l.forwarder.Forward(ctx, msg, peer); err != nil {
		l.logger.Warn("syslog: forward udp", slog.String("peer", peer), slog.Any("err", err))
	}
}
