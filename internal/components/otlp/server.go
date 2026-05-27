package otlp

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// maxBodyBytes caps a single OTLP export body (post-decompression read is
// additionally bounded by maxDecompressedBytes). 16 MiB comfortably covers a
// large batch of log records while protecting the daemon from a runaway sender.
const maxBodyBytes = 16 << 20

// maxDecompressedBytes bounds a gzip-expanded body to guard against a zip-bomb.
const maxDecompressedBytes = 64 << 20

// recordPoster is the slice of pkg/snoozeclient.Client this server needs. A
// real *snoozeclient.Client satisfies it; tests inject a fake. Keeping it a
// local interface avoids dragging the concrete client into the handler tests.
type recordPoster interface {
	PostAlerts(ctx context.Context, recs []snoozetypes.Record) ([]snoozetypes.Record, []error, error)
}

// server is the OTLP/HTTP receiver. It binds a listener lazily in Run so the
// struct can be constructed without side effects (mirrors the jira httpServer).
type server struct {
	addr   string
	poster recordPoster
	logger *slog.Logger
	now    func() time.Time

	ready    chan struct{}
	listener net.Listener
	httpSrv  *http.Server
}

// newServer constructs an OTLP receiver bound (lazily) to addr, forwarding
// mapped records through poster.
func newServer(addr string, poster recordPoster, logger *slog.Logger) *server {
	if logger == nil {
		logger = slog.Default()
	}
	return &server{
		addr:   addr,
		poster: poster,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
		ready:  make(chan struct{}),
	}
}

// Addr returns the bound listener address once Run has started. Tests binding
// to :0 use this to discover the kernel-assigned port.
func (s *server) Addr() string {
	<-s.ready
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// handler builds the OTLP route mux. Exposed so tests can drive it through
// httptest.NewRecorder without binding a socket.
func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs", s.handleLogs)
	mux.HandleFunc("/v1/metrics", s.handleMetrics)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// Run binds the listener and serves the OTLP routes until ctx is cancelled.
func (s *server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		close(s.ready)
		return fmt.Errorf("otlp: listen %q: %w", s.addr, err)
	}
	s.listener = ln

	s.httpSrv = &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	close(s.ready)

	s.logger.Info("otlp: receiver listening",
		slog.String("addr", ln.Addr().String()),
		slog.String("path", "/v1/logs"),
	)

	shutdownDone := make(chan error, 1)
	go func() { //nolint:gosec
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownDone <- s.httpSrv.Shutdown(shutdownCtx)
	}()

	if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("otlp: serve: %w", err)
	}
	return <-shutdownDone
}

// handleLogs implements POST /v1/logs per the OTLP/HTTP spec:
//   - 405 for non-POST.
//   - 415 when the Content-Type is not application/json (e.g. binary Protobuf,
//     which this receiver does not support).
//   - 400 when the body cannot be decoded as an ExportLogsServiceRequest.
//   - 200 with an empty ExportLogsServiceResponse ("{}") on success.
func (s *server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		// The receiver is JSON-only: protobuf (the OTLP default) is rejected.
		http.Error(w,
			"unsupported content-type: snooze-otlp accepts application/json only (gRPC/protobuf OTLP is not supported)",
			http.StatusUnsupportedMediaType)
		return
	}

	body, err := readBody(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}

	var req exportLogsServiceRequest
	dec := json.NewDecoder(strings.NewReader(string(body)))
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid OTLP-JSON logs payload: %v", err), http.StatusBadRequest)
		return
	}

	records := recordsFromRequest(req, s.now())
	s.forward(r.Context(), records)

	// ExportLogsServiceResponse with partial_success unset → empty JSON object.
	writeJSONOK(w)
}

// handleMetrics is a documented no-op stub for POST /v1/metrics. It validates
// the method/content-type the same way as logs and returns an empty
// ExportMetricsServiceResponse, but maps nothing: metrics are not yet
// translated into Snooze alerts in this version.
func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		http.Error(w,
			"unsupported content-type: snooze-otlp accepts application/json only (gRPC/protobuf OTLP is not supported)",
			http.StatusUnsupportedMediaType)
		return
	}
	// Drain so the connection can be reused; the body is intentionally ignored.
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxBodyBytes))
	s.logger.Debug("otlp: /v1/metrics accepted but not mapped (metrics not supported)")
	writeJSONOK(w)
}

// forward POSTs mapped records to Snooze. Errors are logged, not fatal — the
// OTLP spec wants a 200 once the request was accepted; a transient Snooze
// outage should not make exporters retry-storm against the receiver. A nil
// poster (no client wired) degrades to a logged no-op.
func (s *server) forward(ctx context.Context, records []snoozetypes.Record) {
	if len(records) == 0 {
		return
	}
	if s.poster == nil {
		s.logger.Warn("otlp: no snooze client configured; dropping records",
			slog.Int("count", len(records)))
		return
	}
	_, perRec, err := s.poster.PostAlerts(ctx, records)
	if err != nil {
		s.logger.Warn("otlp: forwarding records to snooze failed",
			slog.Int("count", len(records)), slog.Any("err", err))
		return
	}
	for _, e := range perRec {
		if e != nil {
			s.logger.Warn("otlp: snooze rejected a record", slog.Any("err", e))
		}
	}
	s.logger.Debug("otlp: forwarded records", slog.Int("count", len(records)))
}

// isJSONContentType reports whether ct designates JSON. An empty Content-Type
// is treated as JSON for tolerance with minimal curl invocations; everything
// else (notably application/x-protobuf) is rejected with 415.
func isJSONContentType(ct string) bool {
	ct = strings.TrimSpace(ct)
	if ct == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}

// readBody reads the request body, transparently decompressing gzip when the
// Content-Encoding header asks for it. The compressed read is capped at
// maxBodyBytes and the decompressed stream at maxDecompressedBytes.
func readBody(r *http.Request) ([]byte, error) {
	reader := io.LimitReader(r.Body, maxBodyBytes)
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "gzip") {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close() //nolint:errcheck
		reader = io.LimitReader(gz, maxDecompressedBytes)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// writeJSONOK writes the canonical OTLP success response: HTTP 200 with an
// empty JSON object (an ExportLogsServiceResponse / ExportMetricsServiceResponse
// whose partial_success field is unset).
func writeJSONOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}
