package otlp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeSnoozeServer stands in for snooze-server: it answers the login endpoint
// and captures records POSTed to /api/v1/alerts. This exercises the real
// snoozeclient.Client wiring inside the daemon, confirming the alert-post path
// and shape.
type fakeSnoozeServer struct {
	mu     sync.Mutex
	srv    *httptest.Server
	posted []snoozetypes.Record
}

func newFakeSnoozeServer(t *testing.T) *fakeSnoozeServer {
	t.Helper()
	f := &fakeSnoozeServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/login/local", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "tok-123", "expires_at": time.Now().Add(time.Hour), "method": "local",
		})
	})
	mux.HandleFunc("/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		// The batch path POSTs a JSON array of records.
		var recs []snoozetypes.Record
		require.NoError(t, json.NewDecoder(r.Body).Decode(&recs))
		f.mu.Lock()
		f.posted = append(f.posted, recs...)
		f.mu.Unlock()
		data := make([]map[string]any, 0, len(recs))
		for _, rec := range recs {
			data = append(data, map[string]any{"uid": "u", "host": rec.Host, "source": rec.Source})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSnoozeServer) records() []snoozetypes.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]snoozetypes.Record, len(f.posted))
	copy(out, f.posted)
	return out
}

func TestDaemon_PostsToSnoozeAlertEndpoint(t *testing.T) {
	fake := newFakeSnoozeServer(t)

	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:        fake.srv.URL,
		Username:       "u",
		Password:       "p",
		InitialBackoff: time.Millisecond,
		MaxRetries:     1,
		TokenCacheFile: filepath.Join(t.TempDir(), "tok"),
		HTTPClient:     fake.srv.Client(),
	})
	require.NoError(t, err)

	// Drive the receiver handler with a real snoozeclient as the poster.
	s := newServer("127.0.0.1:0", client, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(sampleLogsJSON)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	got := fake.records()
	require.Len(t, got, 1)
	require.Equal(t, "otlp", got[0].Source)
	require.Equal(t, "web-01", got[0].Host)
	require.Equal(t, "warning", got[0].Severity)
	require.Equal(t, "disk usage at 92%", got[0].Message)
}

func TestNewDaemonWithPoster_RunAndShutdown(t *testing.T) {
	fp := &fakePoster{}
	cfg, err := Config{Server: "http://x", Listen: "127.0.0.1:0"}.WithDefaults()
	require.NoError(t, err)
	d, err := newDaemonWithPoster(cfg, fp, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	addr := d.Addr() // blocks until the listener is bound
	require.NotEmpty(t, addr)

	// POST a real export through the bound socket.
	resp, err := http.Post("http://"+addr+"/v1/logs", "application/json",
		bytes.NewReader([]byte(sampleLogsJSON)))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not shut down")
	}

	require.Len(t, fp.all(), 1)
}
