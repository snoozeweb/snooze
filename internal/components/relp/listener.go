package relp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// Handler is the callback the listener invokes for every syslog frame after
// successful decoding. Returning a non-nil error causes the listener to emit
// a NACK; nil produces a 200 OK ACK.
type Handler func(ctx context.Context, payload []byte, peer string) error

// Listener accepts RELP sessions on a TCP socket and dispatches every syslog
// frame to a Handler. One Listener manages many concurrent sessions; each
// session is single-threaded internally because RELP requires ordered
// per-connection ACKs.
type Listener struct {
	addr        string
	handler     Handler
	logger      *slog.Logger
	maxFrameLen int
	readTimeout time.Duration

	listenerMu sync.Mutex
	tcp        *net.TCPListener
	wg         sync.WaitGroup
}

// ListenerOptions bundles the knobs for NewListener.
type ListenerOptions struct {
	Addr        string
	Handler     Handler
	Logger      *slog.Logger
	MaxFrameLen int
	ReadTimeout time.Duration
}

// NewListener builds a Listener but does not bind yet — call Serve to bind
// and start accepting connections.
func NewListener(opts ListenerOptions) (*Listener, error) {
	if opts.Addr == "" {
		return nil, fmt.Errorf("relp: listener addr is required")
	}
	if opts.Handler == nil {
		return nil, fmt.Errorf("relp: listener handler is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.MaxFrameLen <= 0 {
		opts.MaxFrameLen = 1 << 20
	}
	return &Listener{
		addr:        opts.Addr,
		handler:     opts.Handler,
		logger:      opts.Logger,
		maxFrameLen: opts.MaxFrameLen,
		readTimeout: opts.ReadTimeout,
	}, nil
}

// Addr returns the bound address once Serve has started. Returns empty
// string before bind.
func (l *Listener) Addr() string {
	l.listenerMu.Lock()
	defer l.listenerMu.Unlock()
	if l.tcp == nil {
		return ""
	}
	return l.tcp.Addr().String()
}

// Serve binds the TCP socket and runs the accept loop until ctx is cancelled
// or a fatal Accept error occurs. Serve blocks; run it in a goroutine.
func (l *Listener) Serve(ctx context.Context) error {
	tcpAddr, err := net.ResolveTCPAddr("tcp", l.addr)
	if err != nil {
		return fmt.Errorf("relp: resolve %q: %w", l.addr, err)
	}
	ln, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return fmt.Errorf("relp: listen %q: %w", l.addr, err)
	}
	l.listenerMu.Lock()
	l.tcp = ln
	l.listenerMu.Unlock()

	l.logger.Info("relp: listening", slog.String("addr", ln.Addr().String()))

	// Cancellation: close the listener when the context is done so the
	// blocking Accept call below returns.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = ln.Close()
		case <-done:
		}
	}()
	defer close(done)

	for {
		conn, err := ln.AcceptTCP()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				// Clean shutdown — wait for in-flight sessions to drain.
				l.wg.Wait()
				return nil
			}
			return fmt.Errorf("relp: accept: %w", err)
		}
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			l.handleSession(ctx, conn)
		}()
	}
}

// handleSession is the per-connection driver. It reads frames serially, ACKs
// each one once the Handler returns, and writes a serverclose frame before
// closing the socket on graceful shutdown.
func (l *Listener) handleSession(ctx context.Context, conn *net.TCPConn) {
	peer := conn.RemoteAddr().String()
	logger := l.logger.With(slog.String("peer", peer))
	defer func() {
		_ = conn.Close()
		logger.Debug("relp: session closed")
	}()

	reader := NewFrameReader(conn, l.maxFrameLen)

	for {
		if l.readTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(l.readTimeout))
		}
		f, err := reader.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.Debug("relp: peer closed connection")
				return
			}
			logger.Warn("relp: read frame failed", slog.Any("err", err))
			return
		}
		if err := l.dispatch(ctx, conn, f, peer, logger); err != nil {
			logger.Warn("relp: dispatch failed", slog.Any("err", err))
			return
		}
		if f.Command == CmdClose {
			// Peer asked to close — ACK already sent inside dispatch; reply
			// with serverclose and end the session.
			_ = WriteFrame(conn, ServerCloseFrame())
			return
		}
	}
}

// dispatch handles a single frame. Unknown commands receive a NACK so the
// peer can decide whether to retry or give up; we never crash the session
// on a single bad frame.
func (l *Listener) dispatch(ctx context.Context, w io.Writer, f Frame, peer string, logger *slog.Logger) error {
	switch f.Command {
	case CmdOpen:
		// Send back the negotiated offers. We support a minimal set:
		// relp_version=0 and commands=syslog.
		offers := buildOpenResponse(f.Data)
		return WriteFrame(w, Frame{TxnR: f.TxnR, Command: CmdRsp, Data: offers})

	case CmdSyslog:
		if err := l.handler(ctx, f.Data, peer); err != nil {
			logger.Warn("relp: handler rejected frame",
				slog.Uint64("txnr", f.TxnR), slog.Any("err", err))
			return WriteFrame(w, NackFrame(f.TxnR, err.Error()))
		}
		return WriteFrame(w, AckFrame(f.TxnR))

	case CmdClose:
		// ACK the close request; the caller will follow up with a serverclose
		// frame.
		return WriteFrame(w, AckFrame(f.TxnR))

	default:
		// Unrecognised command: emit a NACK so the peer knows we saw it but
		// can't honour it. We log but don't terminate the session.
		logger.Info("relp: unsupported command",
			slog.String("command", f.Command), slog.Uint64("txnr", f.TxnR))
		return WriteFrame(w, NackFrame(f.TxnR, "command not supported: "+f.Command))
	}
}

// buildOpenResponse formats the offer block returned in response to an
// `open` command. The client's offers arrive as `key=value` pairs separated
// by LF; we echo back the keys we accept.
//
// We currently advertise:
//
//	200 OK\nrelp_version=0\nrelp_software=snooze-relp\ncommands=syslog
//
// Unsupported offers like `tls`, `compression`, or `relp_tls` are silently
// ignored — see the package doc for the deferred list.
func buildOpenResponse(clientOffers []byte) []byte {
	// We don't currently negotiate based on the client offers; the response
	// is constant. We do log unsupported offers at the call site (the
	// listener) but here we just emit the canonical reply.
	_ = clientOffers
	var buf bytes.Buffer
	buf.WriteString("200 OK\n")
	buf.WriteString("relp_version=0\n")
	buf.WriteString("relp_software=snooze-relp\n")
	buf.WriteString("commands=syslog")
	return buf.Bytes()
}

// parseOpenOffers is a small helper exposed for tests: it parses the
// key=value pair list a client sends in an `open` frame into a map. We don't
// actually use the result on the hot path, but keeping the helper here keeps
// the wire format documented in one place.
func parseOpenOffers(data []byte) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 0 {
			out[line] = ""
			continue
		}
		out[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
	}
	return out
}
