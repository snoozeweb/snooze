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
	"sync"
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
		Server:         "http://snooze.invalid",
		Method:         "local",
		TenantID:       "tenant",
		ClientID:       "client",
		ClientSecret:   "secret",
		TeamID:         "team",
		ChannelID:      "channel",
		GraphBase:      "http://" + u.Host,
		LoginBase:      "http://" + u.Host,
		PollInterval:   time.Hour, // keep the poll loop idle for these tests
		PollLookback:   time.Minute,
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

// TestHandleAlert_FanInPerChannel exercises the batched-array branch of
// decodeAlertBody. With 3 alerts in the request — two of them targeting
// channel A, one of them targeting channels A *and* B — the listener
// must emit exactly two Graph POSTs:
//
//   - one to channel A containing all 3 alerts in a multi-alert card
//   - one to channel B containing only the single alert that targeted it
//
// This proves the per-channel splitting works: no alert leaks into a
// channel it wasn't addressed to.
func TestHandleAlert_FanInPerChannel(t *testing.T) {
	type capturedPost struct {
		path string
		body map[string]any
	}
	var mu sync.Mutex
	var captured []capturedPost
	graphHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"AT","expires_in":3600}`))
			return
		case strings.Contains(r.URL.Path, "/teams/") && strings.HasSuffix(r.URL.Path, "/messages"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			captured = append(captured, capturedPost{path: r.URL.Path, body: body})
			mu.Unlock()
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
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, time.Second, 10*time.Millisecond, "listener not ready")

	// Two records target channel A, one targets both A and B.
	body := `[
	  {"channels":["teams/team-A/channels/19:aaa@thread.tacv2"],
	   "alert":{"host":"host-1","severity":"critical","message":"first"}},
	  {"channels":["teams/team-A/channels/19:aaa@thread.tacv2"],
	   "alert":{"host":"host-2","severity":"critical","message":"second"}},
	  {"channels":["teams/team-A/channels/19:aaa@thread.tacv2",
	               "teams/team-B/channels/19:bbb@thread.tacv2"],
	   "alert":{"host":"host-3","severity":"emergency","message":"third"}}
	]`
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
		mu.Lock()
		defer mu.Unlock()
		return len(captured) == 2
	}, time.Second, 10*time.Millisecond)

	mu.Lock()
	snap := append([]capturedPost(nil), captured...)
	mu.Unlock()

	// Pick out the post for each channel and assert which alerts each saw.
	var aCard, bCard map[string]any
	for _, c := range snap {
		switch {
		case strings.Contains(c.path, "team-A"):
			aCard = extractCardJSON(t, c.body)
		case strings.Contains(c.path, "team-B"):
			bCard = extractCardJSON(t, c.body)
		default:
			t.Fatalf("unexpected captured path: %s", c.path)
		}
	}
	require.NotNil(t, aCard, "expected a post to team-A")
	require.NotNil(t, bCard, "expected a post to team-B")

	// Channel A: three alerts → multi-alert card with "Received 3 alerts" header.
	aSerialized, _ := json.Marshal(aCard)
	require.Contains(t, string(aSerialized), "Received 3 alerts")
	require.Contains(t, string(aSerialized), "host-1")
	require.Contains(t, string(aSerialized), "host-2")
	require.Contains(t, string(aSerialized), "host-3")

	// Channel B: one alert → single-alert card, only host-3 present.
	bSerialized, _ := json.Marshal(bCard)
	require.Contains(t, string(bSerialized), "Received alert")
	require.Contains(t, string(bSerialized), "host-3")
	require.NotContains(t, string(bSerialized), "host-1")
	require.NotContains(t, string(bSerialized), "host-2")

	cancel()
	require.NoError(t, <-listenErr)
}

// extractCardJSON pulls the AdaptiveCard JSON out of a captured Graph
// chatMessage POST body. Returns nil if the message has no attachment
// (the test caller asserts on that).
func extractCardJSON(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	atts, _ := body["attachments"].([]any)
	require.Len(t, atts, 1)
	att, _ := atts[0].(map[string]any)
	require.Equal(t, "application/vnd.microsoft.card.adaptive", att["contentType"])
	content, _ := att["content"].(string)
	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(content), &card))
	return card
}

// quiet the linter about unused imports if the test file is later edited.
var _ snoozetypes.Record

// TestFormatEscalationReply_Succinct verifies a follow-up reply is a short HTML
// message — not a repeat of the full Adaptive Card. The root message already
// carries host/source/severity/message in its card; a reply only flags the
// re-escalation and restates the (possibly updated) message, mirroring Snooze
// 1.x's threaded "New escalation" reply.
func TestFormatEscalationReply_Succinct(t *testing.T) {
	rec := snoozetypes.Record{
		Host:      "srv-x",
		Source:    "syslog",
		Process:   "Kubernetes",
		Severity:  "critical",
		Message:   "disk full on /var",
		Timestamp: time.Date(2026, 5, 27, 9, 30, 0, 0, time.UTC),
	}
	body := formatEscalationReply([]snoozetypes.Record{rec}, "https://snooze.egerie.eu")

	require.Contains(t, body, "scalation", "reply must flag the re-escalation")
	require.Contains(t, body, "disk full on /var", "reply must restate the alert message")
	// Must NOT repeat the full card the root already shows.
	require.NotContains(t, body, "<attachment", "reply must not attach an Adaptive Card")
	require.NotContains(t, body, "FactSet")
	require.NotContains(t, body, "Received alert")
	require.Contains(t, body, botMarker, "reply must carry the bot marker for self-detection")
}

// TestFormatEscalationReply_MultipleAlerts verifies a batched reply lists each
// alert on its own succinct line rather than emitting one card per alert.
func TestFormatEscalationReply_MultipleAlerts(t *testing.T) {
	recs := []snoozetypes.Record{
		{Host: "h1", Message: "first"},
		{Host: "h2", Message: "second"},
	}
	body := formatEscalationReply(recs, "https://snooze.egerie.eu")
	require.Contains(t, body, "h1")
	require.Contains(t, body, "first")
	require.Contains(t, body, "h2")
	require.Contains(t, body, "second")
	require.NotContains(t, body, "<attachment")
}

// TestHandleAlert_ReplyChain exercises the inject_response-driven reply
// flow: the bridge accepts a reply_to_ids map, posts to the Graph replies
// endpoint, and — crucially — keeps surfacing the THREAD ROOT id (not the
// reply's own id). MS Graph only allows replies one level deep, under a root
// message, so a chain of N follow-ups must all target the same root. Recording
// the reply's id instead would make the next firing POST to
// /messages/<reply-id>/replies, which Graph rejects. Snooze 1.x stored
// resp.root_id for exactly this reason.
func TestHandleAlert_ReplyChain(t *testing.T) {
	var (
		hits     atomic.Int32
		seenPath atomic.Pointer[string]
	)
	graphHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"AT","expires_in":3600}`))
			return
		case strings.Contains(r.URL.Path, "/replies"):
			hits.Add(1)
			p := r.URL.Path
			seenPath.Store(&p)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"reply-msg-99"}`))
			return
		case strings.HasSuffix(r.URL.Path, "/messages"):
			t.Fatalf("expected /replies but got top-level /messages: %s", r.URL.Path)
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
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, time.Second, 10*time.Millisecond)

	// Channel must match the parseChannelRef shape and the reply_to_ids key
	// must use the same string.
	channel := "teams/team-A/channels/19:aaa@thread.tacv2"
	body := `{
		"channels": ["` + channel + `"],
		"alert": {"host": "myhost", "severity": "warning", "message": "disk full"},
		"reply_to_ids": {"` + channel + `": "root-msg-42"}
	}`
	resp, err := http.Post("http://"+addr+"/alert", "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var decoded alertResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&decoded))
	require.Equal(t, []string{channel}, decoded.Delivered)
	require.Empty(t, decoded.Failed)
	require.Equal(t, "root-msg-42", decoded.MessageIDs[channel],
		"a reply must keep recording the THREAD ROOT id, not the reply's own id: "+
			"the next follow-up must target root-msg-42 again (Graph forbids replying "+
			"to reply-msg-99)")

	require.Eventually(t, func() bool { return hits.Load() == 1 }, time.Second, 10*time.Millisecond)
	require.Contains(t, *seenPath.Load(), "/messages/root-msg-42/replies",
		"the bridge must POST to the replies endpoint when reply_to_ids is set")

	cancel()
	require.NoError(t, <-listenErr)
}

// TestHandleAlert_ReplyBodyIsSuccinct verifies that a threaded follow-up posts
// a short HTML body with NO Adaptive Card attachment — the root already carries
// the card. A new top-level message (no reply_to_ids) still gets the full card,
// covered by the other handleAlert tests.
func TestHandleAlert_ReplyBodyIsSuccinct(t *testing.T) {
	var replyBody atomic.Pointer[map[string]any]
	graphHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"AT","expires_in":3600}`))
			return
		case strings.Contains(r.URL.Path, "/replies"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			replyBody.Store(&body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"reply-1"}`))
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
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, time.Second, 10*time.Millisecond)

	channel := "teams/team-A/channels/19:aaa@thread.tacv2"
	body := `{"channels":["` + channel + `"],"alert":{"host":"myhost","severity":"warning","message":"disk full"},"reply_to_ids":{"` + channel + `":"root-1"}}`
	resp, err := http.Post("http://"+addr+"/alert", "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Eventually(t, func() bool { return replyBody.Load() != nil }, time.Second, 10*time.Millisecond)
	rb := *replyBody.Load()
	require.NotContains(t, rb, "attachments", "a threaded reply must not attach an Adaptive Card")
	bodyMap, _ := rb["body"].(map[string]any)
	content, _ := bodyMap["content"].(string)
	require.Contains(t, content, "disk full", "reply should restate the message")
	require.NotContains(t, content, "<attachment", "reply body must not reference a card attachment")

	cancel()
	require.NoError(t, <-listenErr)
}

// TestHandleAlert_FirstSendCapturesMessageID is the "no reply_to_ids yet"
// path: the bridge posts a fresh top-level message and surfaces its id so the
// first inject_response stamps a root the second firing can chain off.
func TestHandleAlert_FirstSendCapturesMessageID(t *testing.T) {
	graphHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"AT","expires_in":3600}`))
			return
		case strings.Contains(r.URL.Path, "/replies"):
			t.Fatalf("first send must NOT hit /replies")
		case strings.HasSuffix(r.URL.Path, "/messages"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"root-msg-77"}`))
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
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, time.Second, 10*time.Millisecond)

	channel := "teams/team-A/channels/19:aaa@thread.tacv2"
	body := `{
		"channels": ["` + channel + `"],
		"alert": {"host": "myhost", "severity": "warning"}
	}`
	resp, err := http.Post("http://"+addr+"/alert", "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var decoded alertResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&decoded))
	require.Equal(t, "root-msg-77", decoded.MessageIDs[channel])

	cancel()
	require.NoError(t, <-listenErr)
}
