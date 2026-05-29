package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "webhook"))
}

func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "webhook"})
	require.NoError(t, err)
	wp, ok := p.(*Plugin)
	require.True(t, ok)
	// Replace the client builder with one that reuses the httptest server's
	// default transport. The default builder builds a TLS-aware client; for
	// plain httptest.NewServer the default transport works fine, so we just
	// honour the timeout.
	wp.newClient = func(cfg Config) *http.Client {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return wp
}

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
	}
}

func TestSendSimplePOST(t *testing.T) {
	var captured struct {
		method      string
		path        string
		contentType string
		body        []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.contentType = r.Header.Get("Content-Type")
		captured.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url": srv.URL + "/hook",
		},
	})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, captured.method)
	require.Equal(t, "/hook", captured.path)
	require.Equal(t, "application/json", captured.contentType)

	var got snoozetypes.Record
	require.NoError(t, json.Unmarshal(captured.body, &got))
	require.Equal(t, rec.Host, got.Host)
	require.Equal(t, rec.Severity, got.Severity)
}

func TestSendHeadersAndBodyTemplate(t *testing.T) {
	var captured struct {
		headerX string
		body    string
		query   string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.headerX = r.Header.Get("X-Host")
		b, _ := io.ReadAll(r.Body)
		captured.body = string(b)
		captured.query = r.URL.RawQuery
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url":    srv.URL + "/h?host={{.Record.Host}}",
			"method": "PUT",
			"headers": map[string]any{
				"X-Host":       "{{.Record.Host}}",
				"X-Static":     "literal",
				"Content-Type": "text/plain",
			},
			"body": "msg={{.Record.Message}} host={{.Record.Host}}",
		},
	})
	require.NoError(t, err)
	require.Equal(t, rec.Host, captured.headerX)
	require.Equal(t, "msg=disk full host="+rec.Host, captured.body)
	require.Equal(t, "host="+rec.Host, captured.query)
}

func TestSendBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url": srv.URL,
			"auth": map[string]any{
				"type":  "bearer",
				"token": "s3cret",
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Bearer s3cret", gotAuth)
}

func TestSendBasicAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url": srv.URL,
			"auth": map[string]any{
				"type":     "basic",
				"username": "alice",
				"password": "wonderland",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(gotAuth, "Basic "), "got=%q", gotAuth)
	// base64("alice:wonderland") == "YWxpY2U6d29uZGVybGFuZA=="
	require.Equal(t, "Basic YWxpY2U6d29uZGVybGFuZA==", gotAuth)
}

func TestSendErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"url": srv.URL},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "nope")
}

func TestSendTimeout(t *testing.T) {
	released := make(chan struct{})

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-released:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	// Order matters: t.Cleanup is LIFO. The handler blocks until either the
	// request context cancels or `released` closes; closing `released` must
	// happen before srv.Close runs (httptest.Server.Close waits on in-flight
	// handlers).
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })

	p := newPluginForTest(t)
	start := time.Now()
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url":     srv.URL,
			"timeout": "150ms",
		},
	})
	elapsed := time.Since(start)
	require.Error(t, err)
	require.GreaterOrEqual(t, hits.Load(), int32(1))
	require.Less(t, elapsed, 2*time.Second, "timeout should fire well before the default")
}

func TestSendMissingURL(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{},
	})
	require.Error(t, err)
}

func TestAuthValidation(t *testing.T) {
	p := newPluginForTest(t)
	rec := sampleRecord()
	// bearer without token
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url":  "http://example.invalid",
			"auth": map[string]any{"type": "bearer"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bearer")

	// unsupported auth type
	err = p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url":  "http://example.invalid",
			"auth": map[string]any{"type": "weird"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported auth type")
}

// TestSendLegacyPythonPayload verifies the Python-era idioms still produce
// the right body. The action records ported from 1.x use `payload` instead
// of `body`, and embed `{{ __self__ | tojson() }}` to inline the record.
func TestSendLegacyPythonPayload(t *testing.T) {
	var captured struct {
		body        []byte
		contentType string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.body, _ = io.ReadAll(r.Body)
		captured.contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url":     srv.URL + "/alert",
			"payload": `{"channels": ["teams/abc/channels/def"], "alert": {{ __self__  | tojson() }} }`,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "application/json", captured.contentType)

	var got map[string]any
	require.NoError(t, json.Unmarshal(captured.body, &got))
	require.Equal(t, []any{"teams/abc/channels/def"}, got["channels"])
	alert, ok := got["alert"].(map[string]any)
	require.True(t, ok, "alert key should decode to an object")
	require.Equal(t, rec.Host, alert["host"])
	require.Equal(t, rec.Severity, alert["severity"])
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "webhook"})
	require.NoError(t, err)
	require.Equal(t, "webhook", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// --- batched dispatch -----------------------------------------------------

// batchRecorder is the goroutine-safe collector that recordingBatchServer
// hands back to the test. Capturing through a mutex (rather than reading a
// shared slice directly from the test) keeps `go test -race` clean.
type batchRecorder struct {
	mu       sync.Mutex
	captured [][]byte
}

func (b *batchRecorder) add(body []byte) {
	b.mu.Lock()
	b.captured = append(b.captured, body)
	b.mu.Unlock()
}

func (b *batchRecorder) snapshot() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([][]byte, len(b.captured))
	copy(out, b.captured)
	return out
}

func (b *batchRecorder) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.captured)
}

// recordingBatchServer captures POST bodies. The test fires N Sends and then
// inspects how many requests landed and what each request's array contained.
func recordingBatchServer(t *testing.T) (*httptest.Server, *batchRecorder) {
	t.Helper()
	rec := &batchRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.add(body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func batchMeta(url, action string, maxsize int, timerSec int) map[string]any {
	return map[string]any{
		"url":           url,
		"payload":       `{"channels": ["teams/T/channels/C"], "alert": {{ __self__  | tojson() }} }`,
		"batch":         true,
		"batch_maxsize": maxsize,
		"batch_timer":   timerSec,
		"action_name":   action,
	}
}

func TestBatchFlushesOnMaxsize(t *testing.T) {
	srv, captured := recordingBatchServer(t)
	p := newPluginForTest(t)
	meta := batchMeta(srv.URL+"/alert", "Teams VM", 3, 60) // long timer — size must fire

	for i := 0; i < 3; i++ {
		rec := sampleRecord()
		rec.UID = "rec-" + string(rune('A'+i))
		require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}))
	}

	require.Eventually(t, func() bool { return captured.len() == 1 },
		2*time.Second, 10*time.Millisecond, "expected exactly one batched POST after 3rd record")

	// The body should be a JSON array with 3 elements, each shaped like the
	// rendered template.
	var arr []map[string]any
	require.NoError(t, json.Unmarshal(captured.snapshot()[0], &arr))
	require.Len(t, arr, 3, "expected three records in the flush")
	for _, item := range arr {
		require.Equal(t, []any{"teams/T/channels/C"}, item["channels"])
		_, ok := item["alert"].(map[string]any)
		require.True(t, ok)
	}
}

func TestBatchFlushesOnTimer(t *testing.T) {
	srv, captured := recordingBatchServer(t)
	p := newPluginForTest(t)
	meta := batchMeta(srv.URL+"/alert", "Teams VM", 100, 0) // 0 → fallback; need >0
	meta["batch_timer"] = 1                                 // 1 second; tests still finish fast

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	// Should fire roughly 1s later, well before the size threshold of 100.
	require.Eventually(t, func() bool { return captured.len() == 1 },
		2500*time.Millisecond, 20*time.Millisecond, "expected one timer-driven flush")

	var arr []map[string]any
	require.NoError(t, json.Unmarshal(captured.snapshot()[0], &arr))
	require.Len(t, arr, 2)
}

func TestBatchKeyIsolation(t *testing.T) {
	srv, captured := recordingBatchServer(t)
	p := newPluginForTest(t)
	a := batchMeta(srv.URL+"/alert", "Action A", 2, 60)
	b := batchMeta(srv.URL+"/alert", "Action B", 2, 60)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: a}))
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: b}))

	// One record in each bucket so far — no flush yet.
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, 0, captured.len())

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: a}))
	require.Eventually(t, func() bool { return captured.len() == 1 },
		time.Second, 10*time.Millisecond, "Action A should flush first")

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: b}))
	require.Eventually(t, func() bool { return captured.len() == 2 },
		time.Second, 10*time.Millisecond, "Action B should flush after its second record")
}

func TestBatchStopDrains(t *testing.T) {
	srv, captured := recordingBatchServer(t)
	p := newPluginForTest(t)
	meta := batchMeta(srv.URL+"/alert", "Teams VM", 100, 60) // both bounds far away

	for i := 0; i < 3; i++ {
		require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))
	}
	require.Equal(t, 0, captured.len(), "no flush expected before Stop")

	require.NoError(t, p.Stop(context.Background()))

	require.Eventually(t, func() bool { return captured.len() == 1 },
		time.Second, 10*time.Millisecond, "Stop should drain the pending bucket")
	var arr []map[string]any
	require.NoError(t, json.Unmarshal(captured.snapshot()[0], &arr))
	require.Len(t, arr, 3)
}

func TestBatchDegenerateConfigFallsBackToImmediate(t *testing.T) {
	srv, captured := recordingBatchServer(t)
	p := newPluginForTest(t)
	// maxsize=1 is treated as degenerate (a "batch of one" is just an
	// immediate send). The plugin must fall back to per-record dispatch.
	meta := batchMeta(srv.URL+"/alert", "Teams VM", 1, 10)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))
	require.Eventually(t, func() bool { return captured.len() == 1 },
		time.Second, 10*time.Millisecond, "immediate dispatch expected")
	// And the body should be the rendered object directly, not wrapped in an
	// array — proves we took the non-batched path.
	var obj map[string]any
	require.NoError(t, json.Unmarshal(captured.snapshot()[0], &obj))
	require.Equal(t, []any{"teams/T/channels/C"}, obj["channels"])
}

// --- proxy + inject_response ----------------------------------------------

func TestProxyWiresIntoTransport(t *testing.T) {
	// The proxy server records hits; the destination server records hits too.
	// We expect the request to land on the proxy, not the destination, when
	// `proxy` is set in the action_form.
	var proxyHits, destHits int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&proxyHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(proxy.Close)
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&destHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(dest.Close)

	p, err := factory(plugins.Metadata{Name: "webhook"})
	require.NoError(t, err)
	wp := p.(*Plugin)
	// Use the real defaultClient so the proxy plumbing actually runs.
	require.NoError(t, p.PostInit(context.Background(), nil))

	require.NoError(t, wp.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url":   dest.URL + "/hook",
			"proxy": proxy.URL,
		},
	}))
	require.EqualValues(t, 1, atomic.LoadInt32(&proxyHits), "proxy should have been hit")
	require.EqualValues(t, 0, atomic.LoadInt32(&destHits), "destination must not be hit directly")
}

func TestInjectResponseCallsInjectFunc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "id": 42}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	var (
		mu     sync.Mutex
		gotF   string
		gotV   any
		called int
	)
	inject := func(field string, value any) {
		mu.Lock()
		defer mu.Unlock()
		gotF = field
		gotV = value
		called++
	}
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url":             srv.URL + "/hook",
			"inject_response": true,
			"action_name":     "my-hook",
		},
		Inject: inject,
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, called)
	require.Equal(t, "response_my-hook", gotF)
	asMap, ok := gotV.(map[string]any)
	require.True(t, ok, "response should decode as a JSON object: %T", gotV)
	require.Equal(t, true, asMap["ok"])
	require.Equal(t, float64(42), asMap["id"])
}

// TestSendBodyTemplateReplyToIDs verifies the body template can emit the Teams
// reply pointers via the computed `.ReplyToIDs` variable. The action name has
// spaces ("Teams Kube Prod"), which a `.Record` field path could not address —
// the variable is derived in Go from `response_<action_name>.message_ids`, so
// the template stays free of the action name. This is what makes the follow-up
// thread under the recorded message.
func TestSendBodyTemplateReplyToIDs(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	// A previous send's inject_response carried forward onto the record.
	rec.Extra = map[string]any{
		"response_Teams Kube Prod": map[string]any{
			"message_ids": map[string]any{"teams/x/channels/y": "1700000000001"},
		},
	}
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url":         srv.URL + "/alert",
			"action_name": "Teams Kube Prod",
			"body":        `{"reply_to_ids": {{ .ReplyToIDs | tojson }}}`,
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"reply_to_ids": {"teams/x/channels/y": "1700000000001"}}`, body)
}

// TestSendBodyTemplateReplyToIDsAbsent verifies that when the record has no
// prior response for this action, `.ReplyToIDs` renders as JSON null so the
// bridge posts a fresh root message rather than erroring.
func TestSendBodyTemplateReplyToIDsAbsent(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url":         srv.URL + "/alert",
			"action_name": "Teams Kube Prod",
			"body":        `{"reply_to_ids": {{ .ReplyToIDs | tojson }}}`,
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"reply_to_ids": null}`, body)
}

func TestInjectResponseDisablesBatch(t *testing.T) {
	// When inject_response is on, batching is forced off so the response can
	// be stamped onto the originating record (which the batch flush has
	// already forgotten).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	var injected int32
	inject := func(_ string, _ any) { atomic.AddInt32(&injected, 1) }

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url":             srv.URL + "/hook",
			"batch":           true,
			"batch_maxsize":   3,
			"batch_timer":     60,
			"inject_response": true,
			"action_name":     "h",
		},
		Inject: inject,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, atomic.LoadInt32(&injected),
		"inject_response forces immediate dispatch even when batch is on")
}
