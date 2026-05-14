package smtp

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// MessageHandler is invoked for every successfully-received SMTP envelope.
// Returning a non-nil error causes the server to reply with a 451 (try later)
// rather than 250 — useful so upstream backpressure (e.g. snooze-server down)
// propagates back to the sender.
type MessageHandler func(ctx context.Context, msg ReceivedMessage) error

// Server is a tiny SMTP server. It binds to one TCP listener, accepts
// connections, and feeds every accepted DATA blob to a MessageHandler.
//
// The server speaks plain SMTP plus optional STARTTLS and optional AUTH PLAIN.
// It does NOT implement: pipelining, BDAT/CHUNKING, SIZE negotiation beyond
// the advertised cap, SMTPUTF8 declarations, or multi-message-per-connection
// (a successful DATA resets back to the initial state, but most senders
// QUIT after one mail and that's the path we exercise in tests).
type Server struct {
	cfg     Config
	handler MessageHandler
	tlsCfg  *tls.Config
	logger  *slog.Logger

	mu       sync.Mutex
	listener net.Listener
	closed   bool
}

// NewServer builds a Server from cfg. When cfg.TLSCert / cfg.TLSKey are set,
// the corresponding files are loaded eagerly so misconfiguration surfaces
// before the first connection. handler must be non-nil.
func NewServer(cfg Config, handler MessageHandler, logger *slog.Logger) (*Server, error) {
	if handler == nil {
		return nil, errors.New("smtp: server handler is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{cfg: cfg, handler: handler, logger: logger}
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("smtp: load tls: %w", err)
		}
		s.tlsCfg = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}
	return s, nil
}

// LocalAddr returns the bound listener address. Useful for tests that bind to
// ":0" and need to discover the kernel-assigned port. Returns nil before
// Listen has been called.
func (s *Server) LocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Listen binds to s.cfg.Listen. It is split out from Run so callers can verify
// the bind succeeded (and learn the resolved port for ":0") before starting
// the accept loop.
func (s *Server) Listen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return errors.New("smtp: already listening")
	}
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("smtp: listen %q: %w", s.cfg.Listen, err)
	}
	s.listener = ln
	return nil
}

// Run loops on Accept until ctx is cancelled. It always returns a non-nil
// error — context.Canceled / context.DeadlineExceeded for normal shutdown.
// Connections in-flight at cancellation time finish on a best-effort basis
// (no graceful drain — see ServerWaitGroup below if you need one).
func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	if ln == nil {
		if err := s.Listen(); err != nil {
			return err
		}
		s.mu.Lock()
		ln = s.listener
		s.mu.Unlock()
	}

	// Close listener on ctx cancel so Accept() returns.
	go func() {
		<-ctx.Done()
		s.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			wg.Wait()
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			if errors.Is(err, net.ErrClosed) {
				return errors.New("smtp: listener closed")
			}
			return fmt.Errorf("smtp: accept: %w", err)
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			s.handle(ctx, c)
		}(conn)
	}
}

// Close stops the accept loop. It is safe to call concurrently and multiple
// times.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// session bundles the per-connection state. It is short-lived and never
// accessed from more than one goroutine.
type session struct {
	srv  *Server
	conn net.Conn
	br   *bufio.Reader
	bw   *bufio.Writer
	tls  bool

	helo     string
	authUser string

	mailFrom string
	rcptTo   []string
}

// handle drives one connection from greeting to QUIT (or io error).
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	sess := &session{
		srv:  s,
		conn: conn,
		br:   bufio.NewReader(conn),
		bw:   bufio.NewWriter(conn),
	}
	sess.setDeadlines()
	sess.write(220, fmt.Sprintf("%s ESMTP snooze-smtp ready", s.cfg.Hostname))

	for {
		if err := ctx.Err(); err != nil {
			return
		}
		sess.setDeadlines()
		line, err := sess.readLine()
		if err != nil {
			return
		}
		verb, rest := splitVerb(line)
		switch strings.ToUpper(verb) {
		case "HELO":
			sess.helo = rest
			sess.write(250, s.cfg.Hostname)
		case "EHLO":
			sess.helo = rest
			sess.writeEHLO()
		case "STARTTLS":
			if s.tlsCfg == nil {
				sess.write(502, "STARTTLS not supported")
				continue
			}
			if sess.tls {
				sess.write(503, "already over TLS")
				continue
			}
			sess.write(220, "ready to start TLS")
			tlsConn := tls.Server(sess.conn, s.tlsCfg)
			if err := tlsConn.Handshake(); err != nil {
				s.logger.Warn("smtp: TLS handshake failed", slog.Any("err", err))
				return
			}
			// Reset readers/writers on the TLS connection and clear state.
			sess.conn = tlsConn
			sess.br = bufio.NewReader(tlsConn)
			sess.bw = bufio.NewWriter(tlsConn)
			sess.tls = true
			sess.helo = ""
			sess.mailFrom = ""
			sess.rcptTo = nil
			sess.authUser = ""
		case "AUTH":
			if !s.acceptAuth(sess, rest) {
				return
			}
		case "MAIL":
			if !s.handleMAIL(sess, rest) {
				continue
			}
		case "RCPT":
			if !s.handleRCPT(sess, rest) {
				continue
			}
		case "DATA":
			if sess.mailFrom == "" || len(sess.rcptTo) == 0 {
				sess.write(503, "need MAIL/RCPT first")
				continue
			}
			s.handleDATA(ctx, sess)
		case "RSET":
			sess.mailFrom = ""
			sess.rcptTo = nil
			sess.write(250, "OK")
		case "NOOP":
			sess.write(250, "OK")
		case "QUIT":
			sess.write(221, "Bye")
			return
		case "VRFY", "EXPN", "HELP":
			sess.write(502, "command not implemented")
		default:
			sess.write(500, "syntax error, command unrecognised")
		}
	}
}

// writeEHLO advertises the extensions we actually support for this session.
func (s *session) writeEHLO() {
	lines := []string{s.srv.cfg.Hostname}
	lines = append(lines, fmt.Sprintf("SIZE %d", s.srv.cfg.MaxMessageBytes))
	if s.srv.tlsCfg != nil && !s.tls {
		lines = append(lines, "STARTTLS")
	}
	// AUTH is only meaningful over TLS (or when no TLS is configured at all)
	// — advertising plaintext AUTH on a cleartext channel is widely discouraged.
	if s.srv.cfg.Username != "" && (s.tls || s.srv.tlsCfg == nil) {
		lines = append(lines, "AUTH PLAIN")
	}
	lines = append(lines, "8BITMIME")
	lines = append(lines, "ENHANCEDSTATUSCODES")
	for i, l := range lines {
		sep := "-"
		if i == len(lines)-1 {
			sep = " "
		}
		_, _ = fmt.Fprintf(s.bw, "250%s%s\r\n", sep, l)
	}
	_ = s.bw.Flush()
}

// acceptAuth handles "AUTH PLAIN [<base64>]". Returns false when the
// connection should be terminated (auth was attempted but failed and the
// session is unsalvageable).
func (s *Server) acceptAuth(sess *session, rest string) bool {
	if s.cfg.Username == "" {
		sess.write(502, "AUTH not advertised")
		return true
	}
	parts := strings.SplitN(rest, " ", 2)
	mechanism := strings.ToUpper(strings.TrimSpace(parts[0]))
	if mechanism != "PLAIN" {
		sess.write(504, "unsupported AUTH mechanism")
		return true
	}
	var enc string
	if len(parts) == 2 {
		enc = strings.TrimSpace(parts[1])
	} else {
		sess.write(334, "")
		line, err := sess.readLine()
		if err != nil {
			return false
		}
		enc = strings.TrimSpace(line)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		sess.write(535, "authentication failed: bad base64")
		return true
	}
	// PLAIN: authzid \0 authcid \0 passwd
	fields := strings.Split(string(raw), "\x00")
	if len(fields) != 3 {
		sess.write(535, "authentication failed: malformed PLAIN")
		return true
	}
	user, pass := fields[1], fields[2]
	if user != s.cfg.Username || pass != s.cfg.Password {
		sess.write(535, "authentication failed")
		return true
	}
	sess.authUser = user
	sess.write(235, "authenticated")
	return true
}

// handleMAIL parses "FROM:<addr>". Returns false if the session should skip
// state mutation (already responded with the right reply).
func (s *Server) handleMAIL(sess *session, rest string) bool {
	if s.cfg.AuthRequired && sess.authUser == "" {
		sess.write(530, "authentication required")
		return false
	}
	addr, ok := parsePathArg(rest, "FROM")
	if !ok {
		sess.write(501, "syntax: MAIL FROM:<addr>")
		return false
	}
	if !s.cfg.senderAllowed(addr) {
		sess.write(550, "sender not allowed")
		return false
	}
	sess.mailFrom = addr
	sess.rcptTo = nil
	sess.write(250, "OK")
	return true
}

// handleRCPT parses "TO:<addr>".
func (s *Server) handleRCPT(sess *session, rest string) bool {
	if sess.mailFrom == "" {
		sess.write(503, "need MAIL FROM first")
		return false
	}
	addr, ok := parsePathArg(rest, "TO")
	if !ok {
		sess.write(501, "syntax: RCPT TO:<addr>")
		return false
	}
	sess.rcptTo = append(sess.rcptTo, addr)
	sess.write(250, "OK")
	return true
}

// handleDATA reads the DATA blob until the dot-line, dedots transparency, and
// invokes the handler.
func (s *Server) handleDATA(ctx context.Context, sess *session) {
	sess.write(354, "end data with <CR><LF>.<CR><LF>")
	sess.setDeadlines()
	data, err := readDataBlob(sess.br, s.cfg.MaxMessageBytes)
	if err != nil {
		if errors.Is(err, errMessageTooLarge) {
			sess.write(552, "message too large")
		} else {
			s.logger.Warn("smtp: DATA read error", slog.Any("err", err))
		}
		return
	}
	msg := ReceivedMessage{
		MailFrom: sess.mailFrom,
		RcptTo:   append([]string(nil), sess.rcptTo...),
		Peer:     sess.conn.RemoteAddr().String(),
		Helo:     sess.helo,
		Auth:     sess.authUser,
		Data:     data,
	}
	if err := s.handler(ctx, msg); err != nil {
		s.logger.Warn("smtp: handler returned error", slog.Any("err", err))
		sess.write(451, "local error, try again later")
	} else {
		sess.write(250, "OK message accepted")
	}
	// Reset envelope state — the connection may send another mail.
	sess.mailFrom = ""
	sess.rcptTo = nil
}

// errMessageTooLarge is returned by readDataBlob when the body exceeds the
// configured cap. The caller maps it to a 552 reply.
var errMessageTooLarge = errors.New("smtp: message exceeds max_message_bytes")

// readDataBlob consumes lines until ".\r\n" and returns the concatenated body
// with dot-stuffing reversed. It enforces max to keep adversarial senders
// from exhausting memory.
func readDataBlob(br *bufio.Reader, max int64) ([]byte, error) {
	var out []byte
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		// End-of-message marker.
		if isDotLine(line) {
			break
		}
		// Dot-stuffing: a line starting with two dots loses one.
		if len(line) >= 2 && line[0] == '.' {
			line = line[1:]
		}
		out = append(out, line...)
		if int64(len(out)) > max {
			// Drain the rest of the message to keep the protocol sane.
			drainUntilDot(br)
			return nil, errMessageTooLarge
		}
	}
	return out, nil
}

// isDotLine returns true when line is exactly ".\r\n" or ".\n" (the SMTP
// end-of-data marker).
func isDotLine(line []byte) bool {
	switch len(line) {
	case 2:
		return line[0] == '.' && line[1] == '\n'
	case 3:
		return line[0] == '.' && line[1] == '\r' && line[2] == '\n'
	default:
		return false
	}
}

// drainUntilDot consumes (and discards) the rest of a DATA blob after a
// too-large detection so the sender doesn't get confused mid-stream.
func drainUntilDot(br *bufio.Reader) {
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return
		}
		if isDotLine(line) {
			return
		}
	}
}

// parsePathArg parses "FROM:<addr>" / "TO:<addr>" (verb is matched
// case-insensitively). It tolerates extra SP and SMTP extension parameters
// after the address ("SIZE=123", "BODY=8BITMIME"), which are ignored.
func parsePathArg(s, verb string) (string, bool) {
	s = strings.TrimSpace(s)
	verb = strings.ToUpper(verb) + ":"
	if !strings.HasPrefix(strings.ToUpper(s), verb) {
		return "", false
	}
	s = strings.TrimSpace(s[len(verb):])
	i := strings.IndexByte(s, '<')
	if i < 0 {
		return "", false
	}
	j := strings.IndexByte(s[i+1:], '>')
	if j < 0 {
		return "", false
	}
	return s[i+1 : i+1+j], true
}

// splitVerb returns the first word ("MAIL", "RCPT", ...) and the rest of the
// line. Verbs are returned as-typed; callers upper-case them when comparing.
func splitVerb(line string) (string, string) {
	line = strings.TrimSpace(line)
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimLeft(line[i+1:], " \t")
}

// setDeadlines refreshes both read and write deadlines so a slow client times
// out instead of pinning a goroutine forever.
func (s *session) setDeadlines() {
	now := time.Now()
	_ = s.conn.SetReadDeadline(now.Add(s.srv.cfg.ReadTimeout))
	_ = s.conn.SetWriteDeadline(now.Add(s.srv.cfg.WriteTimeout))
}

// readLine reads one CRLF-terminated SMTP command line.
func (s *session) readLine() (string, error) {
	line, err := s.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// write sends one status-line reply.
func (s *session) write(code int, msg string) {
	_, _ = fmt.Fprintf(s.bw, "%d %s\r\n", code, msg)
	_ = s.bw.Flush()
}

