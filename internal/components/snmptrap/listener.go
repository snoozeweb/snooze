package snmptrap

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// TrapHandler is invoked for every successfully-unmarshalled trap. Handlers
// must not block the listener for long; the daemon shells off heavy work
// (HTTP forwarding) onto a worker goroutine.
type TrapHandler func(parsed ParsedTrap)

// Listener wraps gosnmp.TrapListener with our config and decoupled handler.
// It is single-use: call Start, then Close.
type Listener struct {
	cfg    Config
	logger *slog.Logger
	tl     *gosnmp.TrapListener
	on     TrapHandler
}

// NewListener wires a Listener but does not bind a socket. Call Start to
// actually listen. logger may be nil (defaults to slog.Default()).
func NewListener(cfg Config, logger *slog.Logger, handler TrapHandler) *Listener {
	if logger == nil {
		logger = slog.Default()
	}
	if handler == nil {
		handler = func(ParsedTrap) {}
	}
	return &Listener{cfg: cfg, logger: logger, on: handler}
}

// Start binds the UDP socket and blocks until the listener errors out or the
// supplied context is cancelled. The actual goroutine that runs gosnmp.Listen
// is detached so Start can return its error synchronously.
//
// Start is safe to call exactly once per Listener instance.
func (l *Listener) Start(ctx context.Context) error {
	tl := gosnmp.NewTrapListener()
	tl.OnNewTrap = l.handle
	tl.Params = buildParams(l.cfg, l.logger)
	l.tl = tl

	errCh := make(chan error, 1)
	go func() {
		// gosnmp.TrapListener.Listen blocks until Close is called or the
		// socket errors out. A clean Close returns a closed-network error
		// which we translate to nil.
		err := tl.Listen(l.cfg.Listen)
		if err != nil && !isClosedErr(err) {
			errCh <- fmt.Errorf("snmptrap: listener: %w", err)
			return
		}
		errCh <- nil
	}()

	// Wait for either the context to be cancelled (graceful shutdown) or the
	// listener goroutine to exit (typically a bind error).
	select {
	case <-ctx.Done():
		tl.Close()
		// Drain the listener goroutine so we don't leak it.
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		return err
	case <-tl.Listening():
		// Listener is up — fall through to wait for shutdown or error.
		l.logger.Info("snmptrap: listening", slog.String("addr", l.cfg.Listen))
	}

	select {
	case <-ctx.Done():
		tl.Close()
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Close terminates the underlying gosnmp listener. Safe to call after Start
// has returned; safe to call multiple times.
func (l *Listener) Close() {
	if l.tl != nil {
		l.tl.Close()
	}
}

// handle is the gosnmp callback. It enforces the configured community for
// v1/v2c traps then dispatches the parsed trap to the registered handler.
func (l *Listener) handle(pkt *gosnmp.SnmpPacket, remote *net.UDPAddr) {
	if pkt == nil {
		return
	}
	// Community check (v1/v2c only). The sentinel "*" disables the check
	// (useful when accepting traps from a mixed fleet).
	if pkt.Version != gosnmp.Version3 && l.cfg.Community != "*" && pkt.Community != l.cfg.Community {
		l.logger.Warn("snmptrap: rejecting trap with unexpected community",
			slog.String("got", pkt.Community),
			slog.String("from", safeAddr(remote)))
		return
	}
	parsed := ParseTrap(pkt, remote)
	l.logger.Debug("snmptrap: received",
		slog.String("from", parsed.Host),
		slog.String("version", parsed.Version),
		slog.String("process", parsed.Process),
		slog.String("severity", parsed.Severity),
	)
	l.on(parsed)
}

// buildParams assembles the gosnmp.GoSNMP parameter object that the trap
// listener consults during decoding. v3 USM is configured when cfg.V3 is set.
func buildParams(cfg Config, logger *slog.Logger) *gosnmp.GoSNMP {
	p := &gosnmp.GoSNMP{
		Version:   gosnmp.Version2c,
		Community: cfg.Community,
		Logger:    gosnmp.NewLogger(&slogShim{l: logger}),
	}
	if cfg.V3 != nil {
		p.Version = gosnmp.Version3
		p.SecurityModel = gosnmp.UserSecurityModel
		p.MsgFlags = v3MsgFlags(cfg.V3)
		p.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 cfg.V3.User,
			AuthenticationProtocol:   parseAuthProto(cfg.V3.AuthProto),
			AuthenticationPassphrase: cfg.V3.AuthPassphrase,
			PrivacyProtocol:          parsePrivProto(cfg.V3.PrivProto),
			PrivacyPassphrase:        cfg.V3.PrivPassphrase,
			Logger:                   gosnmp.NewLogger(&slogShim{l: logger}),
		}
	}
	return p
}

// v3MsgFlags converts the configured auth/priv protocols into the appropriate
// gosnmp message-flag bitmask. Defaults to NoAuthNoPriv if neither is set.
func v3MsgFlags(c *V3Config) gosnmp.SnmpV3MsgFlags {
	auth := parseAuthProto(c.AuthProto)
	priv := parsePrivProto(c.PrivProto)
	switch {
	case auth != gosnmp.NoAuth && priv != gosnmp.NoPriv:
		return gosnmp.AuthPriv
	case auth != gosnmp.NoAuth:
		return gosnmp.AuthNoPriv
	default:
		return gosnmp.NoAuthNoPriv
	}
}

// parseAuthProto turns the operator-facing shorthand into the gosnmp enum.
// Unknown values map to NoAuth, matching the conservative default of the
// Python plugin.
func parseAuthProto(s string) gosnmp.SnmpV3AuthProtocol {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "md5":
		return gosnmp.MD5
	case "sha", "sha1":
		return gosnmp.SHA
	case "sha224":
		return gosnmp.SHA224
	case "sha256":
		return gosnmp.SHA256
	case "sha384":
		return gosnmp.SHA384
	case "sha512":
		return gosnmp.SHA512
	default:
		return gosnmp.NoAuth
	}
}

// parsePrivProto mirrors parseAuthProto for the privacy protocol field.
func parsePrivProto(s string) gosnmp.SnmpV3PrivProtocol {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "des":
		return gosnmp.DES
	case "aes", "aes128":
		return gosnmp.AES
	case "aes192":
		return gosnmp.AES192
	case "aes256":
		return gosnmp.AES256
	default:
		return gosnmp.NoPriv
	}
}

// safeAddr renders a UDPAddr for logging without panicking on nil.
func safeAddr(a *net.UDPAddr) string {
	if a == nil {
		return ""
	}
	return a.String()
}

// isClosedErr reports whether err is the benign "use of closed connection"
// returned by net.Listen* after we call Close.
func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "closed connection")
}

// slogShim adapts our slog.Logger to gosnmp's lightweight logger interface
// (which expects a Printf method). Everything goes out at debug level.
type slogShim struct {
	l *slog.Logger
}

// Printf satisfies gosnmp.LoggerInterface.
func (s *slogShim) Printf(format string, args ...any) {
	s.l.Debug(fmt.Sprintf(format, args...))
}

// Print satisfies gosnmp.LoggerInterface (Print path used by some helpers).
func (s *slogShim) Print(args ...any) {
	s.l.Debug(fmt.Sprint(args...))
}
