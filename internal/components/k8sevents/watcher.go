package k8sevents

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// errGone is returned by the stream reader when the apiserver reports the
// resourceVersion is too old (HTTP 410, or a streamed ERROR/Expired status).
// The watch loop reacts by resetting the resourceVersion and reconnecting.
var errGone = errors.New("k8sevents: watch resourceVersion expired (410 Gone)")

// emitFunc receives every event the watcher decides to forward. The watcher
// owns no Snooze knowledge — the daemon supplies this callback.
type emitFunc func(ctx context.Context, e Event) error

// watcher streams Kubernetes watch events and applies the type filter and
// de-duplication before handing survivors to emit. It tracks the last seen
// resourceVersion so a reconnect resumes where it left off.
type watcher struct {
	cfg    Config
	client *http.Client
	logger *slog.Logger
	emit   emitFunc

	// token is the resolved Kubernetes bearer token (read once at New).
	token string

	mu              sync.Mutex
	resourceVersion string
	dedup           map[string]time.Time
}

// newWatcher builds a watcher from cfg: it resolves the Kubernetes token and CA
// and constructs a CA-trusting HTTP client. emit is the per-event sink.
func newWatcher(cfg Config, logger *slog.Logger, emit emitFunc) (*watcher, error) {
	token, err := resolveToken(cfg)
	if err != nil {
		return nil, err
	}
	client, err := buildAPIClient(cfg)
	if err != nil {
		return nil, err
	}
	return &watcher{
		cfg:    cfg,
		client: client,
		logger: logger,
		emit:   emit,
		token:  token,
		dedup:  make(map[string]time.Time),
	}, nil
}

// resolveToken returns the Kubernetes bearer token, reading the token file when
// an inline token was not supplied.
func resolveToken(cfg Config) (string, error) {
	if cfg.K8sToken != "" {
		return strings.TrimSpace(cfg.K8sToken), nil
	}
	if cfg.K8sTokenFile == "" {
		return "", fmt.Errorf("k8sevents: no Kubernetes token configured")
	}
	raw, err := os.ReadFile(cfg.K8sTokenFile) //nolint:gosec // operator-supplied path
	if err != nil {
		return "", fmt.Errorf("k8sevents: read token_file %q: %w", cfg.K8sTokenFile, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// buildAPIClient constructs an *http.Client whose TLS config trusts the
// configured CA (or skips verification). It deliberately leaves Timeout at zero
// so the long-lived watch stream is not torn down mid-flight; per-request
// timeouts are applied with a context instead.
func buildAPIClient(cfg Config) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.K8sInsecure {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // opt-in via insecure_skip_verify
	} else if cfg.CACert != "" {
		pem, err := os.ReadFile(cfg.CACert) //nolint:gosec // operator-supplied path
		if err != nil {
			return nil, fmt.Errorf("k8sevents: read ca_cert %q: %w", cfg.CACert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("k8sevents: ca_cert %q contained no valid certificates", cfg.CACert)
		}
		tlsCfg.RootCAs = pool
	}
	tr := &http.Transport{
		TLSClientConfig:     tlsCfg,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{Transport: tr}, nil
}

// eventsURL builds the events endpoint, namespaced when configured.
func (w *watcher) eventsURL() string {
	if ns := strings.TrimSpace(w.cfg.Namespace); ns != "" {
		return fmt.Sprintf("%s/api/v1/namespaces/%s/events", w.cfg.APIServer, url.PathEscape(ns))
	}
	return w.cfg.APIServer + "/api/v1/events"
}

// Run drives the watch loop until ctx is cancelled. Each iteration opens one
// streaming watch; on a clean close it reconnects with a short delay, on an
// error it backs off, and on a 410 Gone it resets the resourceVersion first.
func (w *watcher) Run(ctx context.Context) error {
	backoff := newBackoff()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := w.watchOnce(ctx)
		switch {
		case err == nil:
			// The stream ended cleanly (server-side timeout / resync). Resume
			// immediately from the tracked resourceVersion; reset the backoff.
			backoff.reset()
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Our own resync deadline fired — reconnect promptly.
			backoff.reset()
		case errors.Is(err, errGone):
			w.logger.Warn("k8sevents: resourceVersion expired, restarting watch from current state")
			w.setResourceVersion("")
			backoff.reset()
		default:
			d := backoff.next()
			w.logger.Warn("k8sevents: watch error, backing off",
				slog.Any("err", err), slog.Duration("backoff", d))
			if !sleep(ctx, d) {
				return ctx.Err()
			}
		}
	}
}

// watchOnce opens a single watch connection and streams envelopes until the
// connection closes, the resync deadline fires, or an error occurs.
func (w *watcher) watchOnce(parent context.Context) error {
	// Bound this connection by the resync interval so a wedged-open stream is
	// eventually recycled even if the apiserver never closes it.
	ctx, cancel := context.WithTimeout(parent, w.cfg.ResyncInterval)
	defer cancel()

	req, err := w.newWatchRequest(ctx)
	if err != nil {
		return err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("k8sevents: watch request: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusGone {
		return errGone
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("k8sevents: apiserver returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return w.consume(ctx, resp.Body)
}

// newWatchRequest builds the GET request for a streaming watch, resuming from
// the tracked resourceVersion and applying the Warning fieldSelector when the
// configuration forwards Warning events only.
func (w *watcher) newWatchRequest(ctx context.Context) (*http.Request, error) {
	q := url.Values{}
	q.Set("watch", "true")
	q.Set("allowWatchBookmarks", "true")
	if rv := w.getResourceVersion(); rv != "" {
		q.Set("resourceVersion", rv)
	}
	// Server-side filtering for the common Warning-only case spares both the
	// apiserver and this process from streaming Normal noise.
	if !w.cfg.IncludeNormal && len(w.cfg.EventTypes) == 0 {
		q.Set("fieldSelector", "type=Warning")
	}
	u := w.eventsURL() + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("k8sevents: build watch request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// consume streams newline-delimited watch envelopes from r, updates the tracked
// resourceVersion, filters by type and dedup, and forwards survivors to emit.
func (w *watcher) consume(ctx context.Context, r io.Reader) error {
	// Wrap in a bufio.Reader so a json.Decoder can read frame-by-frame without
	// loading the entire (unbounded) stream into memory.
	dec := json.NewDecoder(bufio.NewReader(r))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var raw struct {
			Type   string          `json:"type"`
			Object json.RawMessage `json:"object"`
		}
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return nil // clean end of stream → reconnect
			}
			// A context cancellation surfaces here as a read error; let Run
			// classify it via ctx.Err().
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("k8sevents: decode watch stream: %w", err)
		}

		switch raw.Type {
		case "ERROR":
			var st status
			_ = json.Unmarshal(raw.Object, &st)
			if st.Code == http.StatusGone || strings.EqualFold(st.Reason, "Expired") || strings.EqualFold(st.Reason, "Gone") {
				return errGone
			}
			return fmt.Errorf("k8sevents: watch ERROR: %s (reason=%s code=%d)", st.Message, st.Reason, st.Code)
		case "BOOKMARK":
			// Bookmarks carry only metadata.resourceVersion — advance the
			// cursor so a reconnect resumes efficiently, then move on.
			var ev Event
			if err := json.Unmarshal(raw.Object, &ev); err == nil {
				w.bumpResourceVersion(ev.Metadata.ResourceVersion)
			}
			continue
		}

		var ev Event
		if err := json.Unmarshal(raw.Object, &ev); err != nil {
			w.logger.Debug("k8sevents: skipping undecodable object", slog.Any("err", err))
			continue
		}
		w.bumpResourceVersion(ev.Metadata.ResourceVersion)

		// DELETED events are tombstones, not new conditions; never forward.
		if raw.Type == "DELETED" {
			continue
		}
		if !w.cfg.wantsType(ev.Type) {
			continue
		}
		if w.suppressed(ev) {
			w.logger.Debug("k8sevents: de-duplicated event",
				slog.String("key", ev.dedupKey()))
			continue
		}
		if err := w.emit(ctx, ev); err != nil {
			// Forwarding failures are logged but don't kill the watch — the
			// event will be retried only if it recurs.
			w.logger.Warn("k8sevents: forward failed",
				slog.String("reason", ev.Reason),
				slog.String("object", ev.InvolvedObject.Kind+"/"+ev.InvolvedObject.Name),
				slog.Any("err", err))
		}
	}
}

// suppressed reports whether ev should be dropped as a duplicate seen inside
// the dedup window. It records the timestamp for accepted events. A zero
// DedupWindow disables suppression entirely.
func (w *watcher) suppressed(ev Event) bool {
	if w.cfg.DedupWindow <= 0 {
		return false
	}
	now := time.Now()
	key := ev.dedupKey()
	w.mu.Lock()
	defer w.mu.Unlock()
	if last, ok := w.dedup[key]; ok && now.Sub(last) < w.cfg.DedupWindow {
		return true
	}
	w.dedup[key] = now
	// Opportunistically evict stale keys so the map doesn't grow without bound
	// in a long-running daemon watching a busy cluster.
	if len(w.dedup) > 1024 {
		for k, t := range w.dedup {
			if now.Sub(t) >= w.cfg.DedupWindow {
				delete(w.dedup, k)
			}
		}
	}
	return false
}

func (w *watcher) getResourceVersion() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.resourceVersion
}

func (w *watcher) setResourceVersion(rv string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.resourceVersion = rv
}

// bumpResourceVersion advances the cursor to rv when non-empty. Kubernetes
// resourceVersions are opaque but monotonically increasing per-watch, so we
// simply take the most recently observed value.
func (w *watcher) bumpResourceVersion(rv string) {
	if rv == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.resourceVersion = rv
}

// --- backoff ---------------------------------------------------------------

// backoff is a small capped exponential backoff used between failed watch
// attempts. It is not safe for concurrent use; Run owns the only instance.
type backoff struct {
	cur time.Duration
}

const (
	backoffInitial = 1 * time.Second
	backoffMax     = 30 * time.Second
)

func newBackoff() *backoff { return &backoff{cur: 0} }

func (b *backoff) reset() { b.cur = 0 }

// next returns the next delay, doubling each call up to backoffMax.
func (b *backoff) next() time.Duration {
	if b.cur == 0 {
		b.cur = backoffInitial
		return b.cur
	}
	b.cur *= 2
	if b.cur > backoffMax {
		b.cur = backoffMax
	}
	return b.cur
}

// sleep waits for d or until ctx is cancelled. Returns false if cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
