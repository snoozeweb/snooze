package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newMattermostStub returns an httptest.Server that emulates the small slice
// of Mattermost v4 REST + WS endpoints the daemon talks to. The returned
// `posts` channel receives each POST /api/v4/posts payload so tests can
// assert on the reply emitted by the daemon.
func newMattermostStub(t *testing.T) (*httptest.Server, *stubServer) {
	t.Helper()
	st := &stubServer{
		posts:    make(chan Post, 4),
		wsEvents: make(chan any, 4),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/users/me", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(User{ID: "bot-id", Username: "snoozebot"})
	})
	mux.HandleFunc("/api/v4/teams/name/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/v4/teams/name/")
		_ = json.NewEncoder(w).Encode(Team{ID: "team-id", Name: name})
	})
	mux.HandleFunc("/api/v4/teams/", func(w http.ResponseWriter, r *http.Request) {
		// /api/v4/teams/{teamID}/channels/name/{ch}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v4/teams/"), "/")
		if len(parts) >= 4 && parts[1] == "channels" && parts[2] == "name" {
			_ = json.NewEncoder(w).Encode(Channel{ID: "ch-" + parts[3], TeamID: parts[0], Name: parts[3]})
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v4/posts", func(w http.ResponseWriter, r *http.Request) {
		var p Post
		_ = json.NewDecoder(r.Body).Decode(&p)
		p.ID = "post-" + fmt.Sprint(atomic.AddInt64(&st.postSeq, 1))
		st.posts <- p
		_ = json.NewEncoder(w).Encode(p)
	})
	mux.HandleFunc("/api/v4/websocket", func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("ws upgrade: %v", err)
			return
		}
		// Read the auth challenge so the test mimics real Mattermost behaviour.
		var challenge map[string]any
		if err := conn.ReadJSON(&challenge); err != nil {
			t.Logf("ws read challenge: %v", err)
			_ = conn.Close()
			return
		}
		st.connections.Add(1)
		go func() {
			defer conn.Close()
			for {
				select {
				case <-r.Context().Done():
					return
				case msg, ok := <-st.wsEvents:
					if !ok {
						return
					}
					if msg == nil {
						// Sentinel: close the socket.
						return
					}
					if err := conn.WriteJSON(msg); err != nil {
						return
					}
				}
			}
		}()
		// Block the handler so the upgraded conn stays alive until the test
		// signals it should close (by closing wsEvents or sending nil).
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	st.srv = srv
	return srv, st
}

// stubServer collects observations from a fake Mattermost API for assertions.
type stubServer struct {
	srv         *httptest.Server
	posts       chan Post
	wsEvents    chan any
	connections atomic.Int32
	postSeq     int64
}

// emitPosted pushes a `posted` event into the WS so the daemon's loop sees it.
func (s *stubServer) emitPosted(post postedPayload, senderName, channelName string) {
	postRaw, _ := json.Marshal(post)
	data := map[string]json.RawMessage{
		"post":         mustMarshal(string(postRaw)),
		"sender_name":  mustMarshal(senderName),
		"channel_name": mustMarshal(channelName),
	}
	s.wsEvents <- wsEvent{Event: "posted", Data: data, Seq: 1}
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func tmpCfg(t *testing.T, mmURL string) *Config {
	t.Helper()
	c := &Config{
		Server:                  "http://snooze.invalid",
		Username:                "u",
		Password:                "p",
		MattermostURL:           mmURL,
		MattermostToken:         "tok",
		MattermostTeam:          "ops",
		BotName:                 "snooze",
		PingInterval:            100 * time.Millisecond,
		ReconnectInitialBackoff: 10 * time.Millisecond,
		ReconnectMaxBackoff:     50 * time.Millisecond,
	}
	c.applyDefaults()
	return c
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	body := []byte("" +
		"server: https://snooze.example/\n" +
		"username: bot\n" +
		"password: pw\n" +
		"mattermost_url: https://mm.example/\n" +
		"mattermost_token: tok\n" +
		"mattermost_team: ops\n" +
		"channels: [alerts, oncall]\n",
	)
	if err := writeFile(path, body); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Server != "https://snooze.example" {
		t.Fatalf("trim trailing slash: %q", cfg.Server)
	}
	if cfg.MattermostURL != "https://mm.example" {
		t.Fatalf("trim mm url: %q", cfg.MattermostURL)
	}
	if len(cfg.Channels) != 2 || cfg.Channels[0] != "alerts" {
		t.Fatalf("channels: %+v", cfg.Channels)
	}
	if cfg.PingInterval == 0 || cfg.BotName != "snooze" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadConfigMissingRequired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	// Missing mattermost_url.
	if err := writeFile(path, []byte("server: https://x\nmattermost_token: t\nmattermost_team: ops\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestNewDaemon(t *testing.T) {
	d, err := NewDaemon(tmpCfg(t, "http://example"))
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if d.cfg == nil || d.mm == nil || d.snooze == nil || d.dialer == nil {
		t.Fatalf("daemon not fully wired: %+v", d)
	}
}

func TestNewDaemonRejectsBadConfig(t *testing.T) {
	if _, err := NewDaemon(nil); err == nil {
		t.Fatal("expected nil-config error")
	}
	if _, err := NewDaemon(&Config{}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDaemonHandshakeAndForward(t *testing.T) {
	srv, stub := newMattermostStub(t)
	defer srv.Close()

	cfg := tmpCfg(t, srv.URL)
	cfg.Channels = []string{"alerts"}

	sc := &fakeSnooze{}
	logger := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d, err := NewDaemon(cfg,
		WithLogger(logger),
		WithMattermostAPI(newAPI(srv.URL, cfg.MattermostToken, false)),
		WithSnoozeAPI(sc),
	)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if d.botUser == nil || d.botUser.Username != "snoozebot" {
		t.Fatalf("bot user: %+v", d.botUser)
	}
	if d.team == nil || d.team.Name != "ops" {
		t.Fatalf("team: %+v", d.team)
	}
	if d.channelIDs["ch-alerts"] != "alerts" {
		t.Fatalf("channel lookup: %+v", d.channelIDs)
	}

	// Drive a synthetic `posted` event through handleEvent directly so we
	// keep the test deterministic and avoid the goroutine timing of runOnce.
	post := postedPayload{
		ID:        "post-1",
		UserID:    "user-id",
		ChannelID: "ch-alerts",
		Message:   "/snooze ack abc123 looking",
	}
	ev := &wsEvent{
		Event: "posted",
		Data: map[string]json.RawMessage{
			"post":         mustMarshal(mustString(post)),
			"sender_name":  mustMarshal("alice"),
			"channel_name": mustMarshal("alerts"),
		},
	}
	if err := d.handleEvent(context.Background(), ev); err != nil {
		t.Fatalf("handleEvent: %v", err)
	}

	if sc.method != "POST" || sc.path != "/api/v1/comments" {
		t.Fatalf("expected snooze POST /api/v1/comments, got %s %s", sc.method, sc.path)
	}

	select {
	case p := <-stub.posts:
		if p.ChannelID != "ch-alerts" || !strings.Contains(p.Message, "acknowledged") {
			t.Fatalf("unexpected reply post: %+v", p)
		}
		if p.RootID != "post-1" {
			t.Fatalf("expected reply rooted at post-1, got %q", p.RootID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not post a reply")
	}
}

func TestDaemonIgnoresOwnMessages(t *testing.T) {
	srv, stub := newMattermostStub(t)
	defer srv.Close()
	cfg := tmpCfg(t, srv.URL)
	sc := &fakeSnooze{}
	d, err := NewDaemon(cfg, WithSnoozeAPI(sc), WithMattermostAPI(newAPI(srv.URL, cfg.MattermostToken, false)))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.handshake(context.Background()); err != nil {
		t.Fatal(err)
	}
	post := postedPayload{ID: "p1", UserID: "bot-id", ChannelID: "ch-alerts", Message: "/snooze ack abc"}
	ev := &wsEvent{
		Event: "posted",
		Data: map[string]json.RawMessage{
			"post":        mustMarshal(mustString(post)),
			"sender_name": mustMarshal("snoozebot"),
		},
	}
	if err := d.handleEvent(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-stub.posts:
		t.Fatalf("bot should not reply to its own messages; saw %+v", p)
	case <-time.After(150 * time.Millisecond):
	}
	if sc.path != "" {
		t.Fatalf("bot should not hit snooze for its own messages; saw %s %s", sc.method, sc.path)
	}
}

func TestDaemonReconnectsOnClose(t *testing.T) {
	srv, stub := newMattermostStub(t)
	defer srv.Close()
	cfg := tmpCfg(t, srv.URL)

	var dialCount atomic.Int32
	var firstClosed sync.Once
	d, err := NewDaemon(cfg,
		WithSnoozeAPI(&fakeSnooze{}),
		WithMattermostAPI(newAPI(srv.URL, cfg.MattermostToken, false)),
		WithDialer(func(ctx context.Context, siteURL, token string, insecure bool, logger *slog.Logger) (*wsClient, error) {
			n := dialCount.Add(1)
			ws, err := dialWS(ctx, siteURL, token, insecure, logger)
			if err != nil {
				return nil, err
			}
			if n == 1 {
				// Force the first connection to drop almost immediately so the
				// daemon exercises its backoff/reconnect path.
				go firstClosed.Do(func() {
					time.Sleep(50 * time.Millisecond)
					stub.wsEvents <- nil // sentinel: server-side close
				})
			}
			return ws, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.handshake(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// Wait for the second dial to happen before tearing the test down. This
	// proves the reconnect loop fired.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if dialCount.Load() >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if got := dialCount.Load(); got < 2 {
		t.Fatalf("expected at least 2 dial attempts, got %d", got)
	}
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// --- helpers ---

func writeFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o600)
}

func mustString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// testWriter routes slog output to t.Logf so test failures show the daemon
// log without spamming stderr on a clean run.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
