package k8sevents

import (
	"context"
	"encoding/json"
	"io"
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

// fakeSnooze captures POST /api/v1/alerts payloads and serves the login route.
type fakeSnooze struct {
	mu     sync.Mutex
	srv    *httptest.Server
	posted []snoozetypes.Record
}

func newFakeSnooze(t *testing.T) *fakeSnooze {
	t.Helper()
	f := &fakeSnooze{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/login/local", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "tok-123", "expires_at": time.Now().Add(time.Hour), "method": "local",
		})
	})
	mux.HandleFunc("/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		var rec snoozetypes.Record
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rec))
		f.mu.Lock()
		f.posted = append(f.posted, rec)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"uid": "abc", "host": rec.Host, "source": rec.Source}},
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSnooze) records() []snoozetypes.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]snoozetypes.Record, len(f.posted))
	copy(out, f.posted)
	return out
}

// TestDaemon_EndToEnd wires a canned-stream apiserver to a fake snooze target
// and asserts the mapped records land, with the Normal event filtered out.
func TestDaemon_EndToEnd(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, cannedStream)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // hold the stream open so Run doesn't busy-reconnect
	}))
	defer apiSrv.Close()

	snooze := newFakeSnooze(t)

	cfg := Config{
		Server:    snooze.srv.URL,
		Username:  "u",
		Password:  "p",
		APIServer: apiSrv.URL,
		K8sToken:  "k8s-token",
		// Trust the apiserver via its httptest CA below; insecure keeps the
		// config valid and we override the client anyway.
		K8sInsecure: true,
	}

	d, err := newDaemonForTest(cfg, &snoozeClientPoster{t: t, snooze: snooze}, nil)
	require.NoError(t, err)
	// Point the watcher's HTTP client at the apiserver's TLS-less httptest.
	d.watcher.client = apiSrv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for len(snooze.records()) < 2 {
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatalf("expected 2 alerts, got %d", len(snooze.records()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	recs := snooze.records()
	require.Len(t, recs, 2)
	require.Equal(t, "kubernetes", recs[0].Source)
	require.Equal(t, "web-0", recs[0].Host)
	require.Equal(t, "Pod/BackOff", recs[0].Process)
	require.Equal(t, "error", recs[0].Severity)
	require.Equal(t, "db-0", recs[1].Host)
	require.Equal(t, "critical", recs[1].Severity) // OOMKilling elevated
}

// snoozeClientPoster adapts a real snoozeclient.Client (pointed at the fake)
// into the snoozePoster interface so the end-to-end path exercises the actual
// PostAlert wire format rather than a hand-rolled fake.
type snoozeClientPoster struct {
	t      *testing.T
	snooze *fakeSnooze
	once   sync.Once
	client *snoozeclient.Client
}

func (s *snoozeClientPoster) PostAlert(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, error) {
	s.once.Do(func() {
		c, err := snoozeclient.New(snoozeclient.Options{
			BaseURL:        s.snooze.srv.URL,
			Token:          "tok-123",
			InitialBackoff: time.Millisecond,
			MaxRetries:     1,
			TokenCacheFile: filepath.Join(s.t.TempDir(), "tok"),
			HTTPClient:     s.snooze.srv.Client(),
		})
		require.NoError(s.t, err)
		s.client = c
	})
	return s.client.PostAlert(ctx, rec)
}

// TestDaemon_ForwardMapsRecord exercises the forward callback directly with a
// fake poster (no HTTP), confirming the Record handed to Snooze is correct.
func TestDaemon_ForwardMapsRecord(t *testing.T) {
	fp := &capturePoster{}
	d, err := newDaemonForTest(Config{
		Server: "http://snooze", APIServer: "https://api", K8sToken: "t", K8sInsecure: true,
	}, fp, nil)
	require.NoError(t, err)

	ev := Event{
		InvolvedObject: objectReference{Kind: "Node", Name: "node-9"},
		Reason:         "NodeNotReady",
		Message:        "kubelet stopped posting status",
		Type:           "Warning",
	}
	require.NoError(t, d.forward(context.Background(), ev))
	require.Len(t, fp.recs, 1)
	require.Equal(t, "node-9", fp.recs[0].Host)
	require.Equal(t, "critical", fp.recs[0].Severity)
	require.Equal(t, "Node/NodeNotReady", fp.recs[0].Process)
}

type capturePoster struct {
	mu   sync.Mutex
	recs []snoozetypes.Record
}

func (p *capturePoster) PostAlert(_ context.Context, rec snoozetypes.Record) (snoozetypes.Record, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recs = append(p.recs, rec)
	return rec, nil
}
