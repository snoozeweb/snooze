package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// TLSConfig is the (minimal) HTTPS configuration honored by Server.
//
// When Enabled is true, CertFile and KeyFile must point at a valid PEM cert
// pair; the Server falls back to plain HTTP otherwise.
type TLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
	// MinVersion lets ops pin TLS 1.2/1.3; zero defaults to tls.VersionTLS12.
	MinVersion uint16
}

// Config bundles every knob the HTTP listener needs. Address fields are
// host:port pairs; UnixSocket, if non-empty, swaps the TCP listener for a
// Unix-domain one (mostly useful for behind-nginx deployments).
type Config struct {
	// Addr is the TCP listen address ("0.0.0.0:5200"). Ignored when
	// UnixSocket is set.
	Addr string
	// UnixSocket overrides Addr; if non-empty the server listens on the
	// given filesystem path.
	UnixSocket string
	// TLS toggles HTTPS termination on the TCP listener.
	TLS TLSConfig
	// GracePeriod is the maximum time Shutdown blocks before forcefully
	// closing in-flight requests. Zero defaults to 30s.
	GracePeriod time.Duration
	// BasePath is reserved for documentation; routes are mounted under
	// /api/v1/* by the router and this field is informational.
	BasePath string
	// ReadTimeout, WriteTimeout, IdleTimeout are forwarded to http.Server.
	// Zero leaves the stdlib defaults in place.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// Server is the HTTP frontend. It composes an *http.Server with a logger and
// the resolved listener (TCP or Unix). Construction never blocks; lifecycle
// is owned by ListenAndServe.
type Server struct {
	*http.Server
	Logger     *slog.Logger
	unixSocket string
	tls        TLSConfig
	grace      time.Duration
}

// NewServer wires handler behind an *http.Server using cfg. The returned
// Server is not started; call ListenAndServe.
func NewServer(cfg Config, handler http.Handler, logger *slog.Logger) *Server {
	if cfg.BasePath == "" {
		cfg.BasePath = "/api/v1"
	}
	grace := cfg.GracePeriod
	if grace <= 0 {
		grace = 30 * time.Second
	}
	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
	if cfg.TLS.Enabled {
		minVer := cfg.TLS.MinVersion
		if minVer == 0 {
			minVer = tls.VersionTLS12
		}
		hs.TLSConfig = &tls.Config{MinVersion: minVer}
	}
	return &Server{
		Server:     hs,
		Logger:     logger,
		unixSocket: cfg.UnixSocket,
		tls:        cfg.TLS,
		grace:      grace,
	}
}

// ListenAndServe starts the listener, runs until ctx is cancelled, then
// gracefully shuts the server down. The TLS path consumes the configured
// CertFile/KeyFile; the Unix path applies 0660 permissions to the socket.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := s.listen()
	if err != nil {
		return fmt.Errorf("api server: listen: %w", err)
	}
	if s.Logger != nil {
		s.Logger.Info("api server listening", slog.String("addr", ln.Addr().String()))
	}

	errCh := make(chan error, 1)
	go func() {
		switch {
		case s.tls.Enabled:
			errCh <- s.ServeTLS(ln, s.tls.CertFile, s.tls.KeyFile)
		default:
			errCh <- s.Serve(ln)
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), s.grace)
		defer cancel()
		if err := s.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("api server: shutdown: %w", err)
		}
		// drain Serve's exit
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("api server: serve: %w", err)
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("api server: serve: %w", err)
		}
		return nil
	}
}

// listen resolves the configured listener (Unix > TCP) and returns it.
func (s *Server) listen() (net.Listener, error) {
	if s.unixSocket != "" {
		// Best-effort: remove a stale socket file before binding.
		_ = removeSocket(s.unixSocket)
		ln, err := net.Listen("unix", s.unixSocket)
		if err != nil {
			return nil, err
		}
		return ln, nil
	}
	return net.Listen("tcp", s.Addr)
}
