package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// newTestDaemonWithGraph wires a Daemon whose graphClient targets the
// supplied httptest server, so /alert posts land in the test handler instead
// of Microsoft. The listener address is taken from the Daemon config.
func newTestDaemonWithGraph(t *testing.T, listenAddr string, graphHandler http.HandlerFunc) *Daemon {
	t.Helper()
	graphSrv := httptest.NewServer(graphHandler)
	t.Cleanup(graphSrv.Close)
	u, err := url.Parse(graphSrv.URL)
	require.NoError(t, err)

	cfg := Config{
		Server:       "http://snooze.invalid",
		Method:       "local",
		TenantID:     "tenant",
		ClientID:     "client",
		ClientSecret: "secret",
		TeamID:       "team",
		ChannelID:    "channel",
		GraphBase:    "http://" + u.Host,
		LoginBase:    "http://" + u.Host,
		PollInterval: time.Hour, // keep the poll loop idle for these tests
		PollLookback: time.Minute,
		RequestTimeout: 2 * time.Second,
		BotName:        "SnoozeBot",
		ListenAddr:     listenAddr,
		// Pin to client_credentials for these tests so the mocked Graph
		// server can satisfy the OAuth round-trip without a real token
		// store on disk.
		AuthMode: "client_credentials",
	}
	cfg, err = cfg.WithDefaults()
	require.NoError(t, err)

	d, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	return d
}

// pickFreePort asks the kernel for an ephemeral port the listener can claim.
// Two short windows are inherent to this dance (port released → listener
// binds), but in practice it is reliable enough for in-process tests.
func pickFreePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestDecodeAlertBody(t *testing.T) {
	t.Run("single object", func(t *testing.T) {
		body := []byte(`{"channels": ["teams/t/channels/c"], "alert": {"host": "h"}}`)
		got, err := decodeAlertBody(body)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "h", got[0].Alert.Host)
		require.Equal(t, []string{"teams/t/channels/c"}, got[0].Channels)
	})
	t.Run("array", func(t *testing.T) {
		body := []byte(`[{"channels": ["teams/t/channels/c"], "alert": {"host": "h1"}}, {"channels": ["teams/t/channels/c2"], "alert": {"host": "h2"}}]`)
		got, err := decodeAlertBody(body)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, "h1", got[0].Alert.Host)
		require.Equal(t, "h2", got[1].Alert.Host)
	})
	t.Run("leading whitespace tolerated", func(t *testing.T) {
		_, err := decodeAlertBody([]byte("   \n\t" + `{"channels":[],"alert":{}}`))
		require.NoError(t, err)
	})
	t.Run("empty body", func(t *testing.T) {
		_, err := decodeAlertBody([]byte("   "))
		require.Error(t, err)
	})
	t.Run("garbage", func(t *testing.T) {
		_, err := decodeAlertBody([]byte("not json"))
		require.Error(t, err)
	})
}

func TestParseChannelRef(t *testing.T) {
	tests := []struct {
		in      string
		team    string
		channel string
		err     bool
	}{
		{"teams/abc/channels/19:xyz@thread.tacv2", "abc", "19:xyz@thread.tacv2", false},
		{"teams/abc/channels/short", "abc", "short", false},
		{"channels/abc/teams/x", "", "", true},
		{"teams//channels/c", "", "", true},
		{"teams/t/channels/", "", "", true},
		{"random", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			team, channel, err := parseChannelRef(tc.in)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.team, team)
			require.Equal(t, tc.channel, channel)
		})
	}
}

func TestHandleAlert_PostsOneMessagePerChannel(t *testing.T) {
	var posts atomic.Int32
	type capturedPost struct {
		Path string
		Body map[string]any
	}
	var captured []capturedPost
	graphHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"AT","expires_in":3600}`))
			return
		case strings.Contains(r.URL.Path, "/teams/") && strings.HasSuffix(r.URL.Path, "/messages"):
			posts.Add(1)
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			captured = append(captured, capturedPost{Path: r.URL.Path, Body: body})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg1"}`))
			return
		default:
			http.NotFound(w, r)
		}
	}
	addr := pickFreePort(t)
	d := newTestDaemonWithGraph(t, addr, graphHandler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listenErr := make(chan error, 1)
	go func() { listenErr <- d.runListener(ctx) }()

	// wait briefly for the server to be ready
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, time.Second, 10*time.Millisecond, "listener not ready")

	body := `{
		"channels": [
			"teams/team-A/channels/19:aaa@thread.tacv2",
			"teams/team-B/channels/19:bbb@thread.tacv2"
		],
		"alert": {"host": "myhost", "severity": "warning", "message": "disk full"}
	}`
	resp, err := http.Post("http://"+addr+"/alert", "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var decoded alertResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&decoded))
	require.ElementsMatch(t, []string{
		"teams/team-A/channels/19:aaa@thread.tacv2",
		"teams/team-B/channels/19:bbb@thread.tacv2",
	}, decoded.Delivered)
	require.Empty(t, decoded.Failed)

	require.Eventually(t, func() bool {
		return posts.Load() == 2
	}, time.Second, 10*time.Millisecond)

	require.Len(t, captured, 2)
	for _, c := range captured {
		bodyMap, _ := c.Body["body"].(map[string]any)
		require.Equal(t, "html", bodyMap["contentType"])
		content, _ := bodyMap["content"].(string)
		require.Contains(t, content, "<attachment id=")
		// The alert fields live inside the AdaptiveCard JSON attached to the
		// message; assert against that rather than the (placeholder-only)
		// body.content.
		atts, _ := c.Body["attachments"].([]any)
		require.Len(t, atts, 1, "expected one attachment per message")
		att, _ := atts[0].(map[string]any)
		require.Equal(t, "application/vnd.microsoft.card.adaptive", att["contentType"])
		cardJSON, _ := att["content"].(string)
		require.Contains(t, cardJSON, "myhost")
		require.Contains(t, cardJSON, "warning")
	}

	cancel()
	require.NoError(t, <-listenErr)
}

func TestHandleAlert_PartialFailure(t *testing.T) {
	graphHandler := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"AT","expires_in":3600}`))
			return
		}
		// One channel succeeds, the other 500s.
		if strings.Contains(r.URL.Path, "team-bad") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}
	addr := pickFreePort(t)
	d := newTestDaemonWithGraph(t, addr, graphHandler)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.runListener(ctx) }()
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, time.Second, 10*time.Millisecond)

	body := `{
		"channels": ["teams/team-good/channels/c1", "teams/team-bad/channels/c2"],
		"alert": {"host": "h"}
	}`
	resp, err := http.Post("http://"+addr+"/alert", "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	// One succeeded → 200 overall (partial success).
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var decoded alertResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&decoded))
	require.Equal(t, []string{"teams/team-good/channels/c1"}, decoded.Delivered)
	require.Contains(t, decoded.Failed, "teams/team-bad/channels/c2")

	cancel()
	<-done
}

func TestHandleAlert_RejectsNonPOST(t *testing.T) {
	addr := pickFreePort(t)
	d := newTestDaemonWithGraph(t, addr, func(http.ResponseWriter, *http.Request) {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.runListener(ctx) }()
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, time.Second, 10*time.Millisecond)

	resp, err := http.Get("http://" + addr + "/alert")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	cancel()
	<-done
}

// TestRunListener_DisabledNoOp ensures an empty ListenAddr returns
// immediately without binding any port.
func TestRunListener_DisabledNoOp(t *testing.T) {
	d := newTestDaemonWithGraph(t, "", func(http.ResponseWriter, *http.Request) {})
	require.NoError(t, d.runListener(context.Background()))
}

// quiet the linter about unused imports if the test file is later edited.
var _ snoozetypes.Record
