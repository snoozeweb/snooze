package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// httpServer wraps net/http to serve POST /alert. The shape mirrors the
// other "listener" daemons (snmptrap, smtp): a small struct, a Run method
// that blocks until ctx is cancelled, and an Addr accessor for tests.
type httpServer struct {
	addr      string
	logger    *slog.Logger
	forwarder *forwarder
	ready     chan struct{}

	listener net.Listener
	server   *http.Server
}

// newHTTPServer constructs an httpServer. The TCP socket is bound lazily by
// Run so callers can construct without side effects.
func newHTTPServer(addr string, fwd *forwarder, logger *slog.Logger) *httpServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &httpServer{
		addr:      addr,
		logger:    logger,
		forwarder: fwd,
		ready:     make(chan struct{}),
	}
}

// Addr returns the bound listener address once Run has started. Tests that
// bind to :0 use this to discover the kernel-assigned port.
func (s *httpServer) Addr() string {
	<-s.ready
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Run binds the listener and serves /alert until ctx is cancelled. Returns
// nil for clean shutdown.
func (s *httpServer) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		close(s.ready)
		return fmt.Errorf("jira: listen %q: %w", s.addr, err)
	}
	s.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/alert", s.handleAlert)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	close(s.ready)

	s.logger.Info("jira: webhook listening", slog.String("addr", ln.Addr().String()))

	// Cancellation: graceful shutdown when ctx is cancelled.
	shutdownDone := make(chan error, 1)
	go func() { //nolint:gosec
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownDone <- s.server.Shutdown(shutdownCtx)
	}()

	if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("jira: serve: %w", err)
	}
	return <-shutdownDone
}

// handleAlert is the only mutating route on the webhook surface. It accepts
// a single envelope or an array — both are normal Snooze webhook payloads
// (the "Batch" toggle picks one over the other).
func (s *httpServer) handleAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close() //nolint:errcheck

	envs, err := decodeEnvelopes(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	action := r.URL.Query().Get("snooze_action_name")
	if action == "" {
		action = "unknown_action"
	}

	out := s.forwarder.handleEnvelopes(r.Context(), envs, action)
	w.Header().Set("Content-Type", "application/json")
	if len(out) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.logger.Warn("jira: encode response failed", slog.Any("err", err))
	}
}

// decodeEnvelopes accepts either a single object or a JSON array of objects.
// We read the body fully (capped at 8 MiB) and try the array path first; on
// a type mismatch we fall back to a single-object decode. Unknown keys are
// tolerated — json.Unmarshal ignores them by default.
func decodeEnvelopes(r *http.Request) ([]envelope, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("decode body: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("decode body: empty body")
	}
	var arr []envelope
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var one envelope
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, fmt.Errorf("decode body: %w", err)
	}
	return []envelope{one}, nil
}
