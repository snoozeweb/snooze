package k8sevents

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeFile is a tiny os.WriteFile shim with 0600 perms, shared across the
// package's tests.
func writeFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o600)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// canned watch stream: two Warning events, one Normal, and a BOOKMARK.
const cannedStream = `{"type":"ADDED","object":{"metadata":{"namespace":"prod","name":"web.1","resourceVersion":"100"},"involvedObject":{"kind":"Pod","name":"web-0","namespace":"prod"},"reason":"BackOff","message":"crash loop","type":"Warning","lastTimestamp":"2026-05-27T09:00:00Z","count":4,"source":{"component":"kubelet","host":"node-1"}}}
{"type":"ADDED","object":{"metadata":{"namespace":"prod","name":"web.2","resourceVersion":"101"},"involvedObject":{"kind":"Pod","name":"db-0","namespace":"prod"},"reason":"OOMKilling","message":"oom","type":"Warning","lastTimestamp":"2026-05-27T09:01:00Z","count":1,"source":{"component":"kubelet","host":"node-2"}}}
{"type":"ADDED","object":{"metadata":{"namespace":"prod","name":"web.3","resourceVersion":"102"},"involvedObject":{"kind":"Pod","name":"web-0","namespace":"prod"},"reason":"Scheduled","message":"scheduled","type":"Normal","lastTimestamp":"2026-05-27T09:02:00Z","count":1,"source":{"component":"scheduler"}}}
{"type":"BOOKMARK","object":{"metadata":{"resourceVersion":"103"}}}
`

// collectingWatcher builds a watcher pointed at srv that records the events it
// would forward into events. The mu guards events for -race.
type collected struct {
	mu     sync.Mutex
	events []Event
}

func (c *collected) emit(_ context.Context, e Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

func (c *collected) snapshot() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	copy(out, c.events)
	return out
}

// newTestWatcher builds a watcher whose apiserver is srv. We bypass
// newWatcher's CA/token file reads by setting fields directly with a client
// that trusts srv.
func newTestWatcher(t *testing.T, srv *httptest.Server, cfg Config, emit emitFunc) *watcher {
	t.Helper()
	cfg.APIServer = srv.URL
	cfg, err := cfg.WithDefaults()
	require.NoError(t, err)
	cfg.APIServer = srv.URL // WithDefaults trims trailing slash; httptest has none
	return &watcher{
		cfg:    cfg,
		client: srv.Client(),
		logger: quietLogger(),
		emit:   emit,
		token:  "test-token",
		dedup:  make(map[string]time.Time),
	}
}

func TestWatcher_StreamMapping_WarningOnly(t *testing.T) {
	var gotAuth, gotSelector string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSelector = r.URL.Query().Get("fieldSelector")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, cannedStream)
	}))
	defer srv.Close()

	col := &collected{}
	w := newTestWatcher(t, srv, Config{
		Server: "http://snooze", K8sToken: "t", K8sInsecure: true,
	}, col.emit)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.watchOnce(ctx))

	require.Equal(t, "Bearer test-token", gotAuth)
	require.Equal(t, "type=Warning", gotSelector) // server-side warning filter

	evs := col.snapshot()
	// Normal "Scheduled" is filtered out; BOOKMARK is not an event.
	require.Len(t, evs, 2)
	require.Equal(t, "BackOff", evs[0].Reason)
	require.Equal(t, "OOMKilling", evs[1].Reason)
	// resourceVersion advanced to the bookmark.
	require.Equal(t, "103", w.getResourceVersion())
}

func TestWatcher_IncludeNormal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When IncludeNormal is on, no fieldSelector should be sent.
		require.Empty(t, r.URL.Query().Get("fieldSelector"))
		_, _ = io.WriteString(w, cannedStream)
	}))
	defer srv.Close()

	col := &collected{}
	w := newTestWatcher(t, srv, Config{
		Server: "http://snooze", K8sToken: "t", K8sInsecure: true, IncludeNormal: true,
	}, col.emit)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.watchOnce(ctx))
	require.Len(t, col.snapshot(), 3) // Normal included now
}

func TestWatcher_Dedup(t *testing.T) {
	// Two identical BackOff events for the same pod within the window → 1 emit.
	const dupStream = `{"type":"ADDED","object":{"metadata":{"resourceVersion":"1"},"involvedObject":{"kind":"Pod","name":"web-0","namespace":"prod"},"reason":"BackOff","type":"Warning"}}
{"type":"MODIFIED","object":{"metadata":{"resourceVersion":"2"},"involvedObject":{"kind":"Pod","name":"web-0","namespace":"prod"},"reason":"BackOff","type":"Warning"}}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, dupStream)
	}))
	defer srv.Close()

	col := &collected{}
	w := newTestWatcher(t, srv, Config{
		Server: "http://snooze", K8sToken: "t", K8sInsecure: true, DedupWindow: time.Hour,
	}, col.emit)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.watchOnce(ctx))
	require.Len(t, col.snapshot(), 1)
}

func TestWatcher_DedupDisabled(t *testing.T) {
	const dupStream = `{"type":"ADDED","object":{"metadata":{"resourceVersion":"1"},"involvedObject":{"kind":"Pod","name":"web-0","namespace":"prod"},"reason":"BackOff","type":"Warning"}}
{"type":"MODIFIED","object":{"metadata":{"resourceVersion":"2"},"involvedObject":{"kind":"Pod","name":"web-0","namespace":"prod"},"reason":"BackOff","type":"Warning"}}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, dupStream)
	}))
	defer srv.Close()

	col := &collected{}
	cfg := Config{Server: "http://snooze", K8sToken: "t", K8sInsecure: true}
	cfg.DedupWindow = -1 // disable
	w := newTestWatcher(t, srv, cfg, col.emit)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.watchOnce(ctx))
	require.Len(t, col.snapshot(), 2)
}

func TestWatcher_Gone_HTTP410(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()
	w := newTestWatcher(t, srv, Config{Server: "http://snooze", K8sToken: "t", K8sInsecure: true}, func(context.Context, Event) error { return nil })
	w.setResourceVersion("12345")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := w.watchOnce(ctx)
	require.ErrorIs(t, err, errGone)
}

func TestWatcher_Gone_StreamedERROR(t *testing.T) {
	const errStream = `{"type":"ERROR","object":{"kind":"Status","apiVersion":"v1","status":"Failure","message":"too old resource version","reason":"Expired","code":410}}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, errStream)
	}))
	defer srv.Close()
	w := newTestWatcher(t, srv, Config{Server: "http://snooze", K8sToken: "t", K8sInsecure: true}, func(context.Context, Event) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.ErrorIs(t, w.watchOnce(ctx), errGone)
}

// TestWatcher_Run_410ResetsResourceVersion drives the full Run loop: the first
// connection returns 410 (with a stale RV set), Run must clear the RV and the
// second connection must be issued with no resourceVersion query param.
func TestWatcher_Run_410ResetsResourceVersion(t *testing.T) {
	var (
		mu        sync.Mutex
		callRVs   []string
		callCount int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		n := callCount
		callRVs = append(callRVs, r.URL.Query().Get("resourceVersion"))
		mu.Unlock()
		switch n {
		case 1:
			w.WriteHeader(http.StatusGone) // stale RV → 410
		case 2:
			// Serve one event then stream stays open until ctx cancel.
			_, _ = io.WriteString(w, `{"type":"ADDED","object":{"metadata":{"resourceVersion":"200"},"involvedObject":{"kind":"Pod","name":"p","namespace":"d"},"reason":"BackOff","type":"Warning"}}`+"\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		default:
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	col := &collected{}
	cfg := Config{Server: "http://snooze", K8sToken: "t", K8sInsecure: true}
	w := newTestWatcher(t, srv, cfg, col.emit)
	w.setResourceVersion("999") // pretend we were resuming from a stale RV

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Wait for the event from connection #2 to arrive.
	deadline := time.Now().Add(3 * time.Second)
	for len(col.snapshot()) < 1 {
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("expected an event after the 410 reset within 3s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(callRVs), 2)
	require.Equal(t, "999", callRVs[0], "first call resumes from stale RV")
	require.Equal(t, "", callRVs[1], "after 410 the RV is reset, so no resourceVersion param")
}

func TestWatcher_NamespacedURL(t *testing.T) {
	w := &watcher{cfg: Config{APIServer: "https://api", Namespace: "kube-system"}}
	require.Equal(t, "https://api/api/v1/namespaces/kube-system/events", w.eventsURL())

	wAll := &watcher{cfg: Config{APIServer: "https://api"}}
	require.Equal(t, "https://api/api/v1/events", wAll.eventsURL())
}

func TestBackoff(t *testing.T) {
	b := newBackoff()
	require.Equal(t, backoffInitial, b.next())
	require.Equal(t, 2*time.Second, b.next())
	require.Equal(t, 4*time.Second, b.next())
	// Cap.
	for i := 0; i < 10; i++ {
		b.next()
	}
	require.Equal(t, backoffMax, b.next())
	b.reset()
	require.Equal(t, backoffInitial, b.next())
}

// TestWatcher_Non200 confirms a non-200/410 status surfaces as an error
// (which Run would back off on).
func TestWatcher_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "forbidden")
	}))
	defer srv.Close()
	w := newTestWatcher(t, srv, Config{Server: "http://snooze", K8sToken: "t", K8sInsecure: true}, func(context.Context, Event) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := w.watchOnce(ctx)
	require.Error(t, err)
	require.NotErrorIs(t, err, errGone)
}

// sanity: make sure the query encoder produces the watch=true param.
func TestNewWatchRequest_Params(t *testing.T) {
	w := &watcher{cfg: Config{APIServer: "https://api"}, token: "tk"}
	req, err := w.newWatchRequest(context.Background())
	require.NoError(t, err)
	q, err := url.ParseQuery(req.URL.RawQuery)
	require.NoError(t, err)
	require.Equal(t, "true", q.Get("watch"))
	require.Equal(t, "true", q.Get("allowWatchBookmarks"))
	require.Equal(t, "type=Warning", q.Get("fieldSelector"))
	require.Equal(t, "Bearer tk", req.Header.Get("Authorization"))
}
