package telegraminteractive

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/pluginimpl/comment"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

const testSecret = "telegram-secret-token"

// testHost wires a real SQLite driver + real comment plugin so the receiver
// exercises the genuine chataction.Apply create seam.
type testHost struct {
	drv     *sqlite.Driver
	cfg     *config.Config
	comment plugins.Plugin
}

func newTestHost(t *testing.T, apiBase string) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	cfg := config.Default()
	cfg.TelegramInteractive.BotToken = "123:ABC"
	cfg.TelegramInteractive.SecretToken = testSecret
	cfg.TelegramInteractive.APIBase = apiBase

	h := &testHost{drv: drv, cfg: cfg}
	cp, err := comment.New(plugins.Metadata{Name: "comment"})
	require.NoError(t, err)
	require.NoError(t, cp.PostInit(context.Background(), h))
	h.comment = cp
	return h
}

func (h *testHost) DB() db.Driver        { return h.drv }
func (h *testHost) Bus() plugins.Bus     { return nil }
func (h *testHost) Logger() *slog.Logger { return slog.Default() }
func (h *testHost) Tracer() trace.Tracer { return otel.Tracer("telegraminteractive-test") }
func (h *testHost) Metrics() *telemetry.Registry {
	return telemetry.NewRegistry(nil)
}
func (h *testHost) Config() *config.Config { return h.cfg }
func (h *testHost) Plugin(name string) plugins.Plugin {
	if name == "comment" {
		return h.comment
	}
	return nil
}

func tenantCtx() context.Context {
	return auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
}

func seedRecord(t *testing.T, host *testHost, state string) string {
	t.Helper()
	res, err := host.DB().Write(tenantCtx(), "record",
		[]db.Document{{"state": state}}, db.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	return res.Added[0]
}

func recordState(t *testing.T, host *testHost, uid string) string {
	t.Helper()
	rec, err := host.DB().GetOne(tenantCtx(), "record", db.Document{"uid": uid})
	require.NoError(t, err)
	s, _ := rec["state"].(string)
	return s
}

// telegramAPIStub captures editMessageText / answerCallbackQuery calls.
type telegramAPIStub struct {
	mu       sync.Mutex
	editText []string // text bodies seen at editMessageText
	answered bool
}

func newTelegramAPIStub(t *testing.T) (*httptest.Server, *telegramAPIStub) {
	t.Helper()
	stub := &telegramAPIStub{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		switch {
		case strings.Contains(r.URL.Path, "editMessageText"):
			var req struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(body, &req)
			stub.editText = append(stub.editText, req.Text)
		case strings.Contains(r.URL.Path, "answerCallbackQuery"):
			stub.answered = true
		}
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, stub
}

func newPluginForTest(t *testing.T, host *testHost) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "telegraminteractive"})
	require.NoError(t, err)
	tp := p.(*Plugin)
	require.NoError(t, tp.PostInit(context.Background(), host))
	tp.newClient = func(timeout time.Duration) *http.Client {
		return &http.Client{Timeout: timeout}
	}
	return tp
}

// callbackBody builds a Telegram update JSON carrying a callback_query.
func callbackBody(t *testing.T, data, username string) []byte {
	t.Helper()
	update := map[string]any{
		"callback_query": map[string]any{
			"id":   "cbq-1",
			"data": data,
			"from": map[string]any{"username": username},
			"message": map[string]any{
				"message_id": 42,
				"chat":       map[string]any{"id": 9001},
			},
		},
	}
	b, err := json.Marshal(update)
	require.NoError(t, err)
	return b
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "telegraminteractive"))
}

func TestWebhookPath(t *testing.T) {
	p, err := factory(plugins.Metadata{Name: "telegraminteractive"})
	require.NoError(t, err)
	wr, ok := p.(plugins.WebhookReceiver)
	require.True(t, ok)
	require.Equal(t, "/telegram", wr.WebhookPath())
}

// TestHandleWebhook_RejectsBadSecret: a request whose
// X-Telegram-Bot-Api-Secret-Token does not match the configured secret is 401,
// no state change.
func TestHandleWebhook_RejectsBadSecret(t *testing.T) {
	srv, _ := newTelegramAPIStub(t)
	host := newTestHost(t, srv.URL)
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "")

	req := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(callbackBody(t, "ack "+uid, "alice"))))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "WRONG")
	req = req.WithContext(tenantCtx())
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "", recordState(t, host, uid), "rejected request must not change state")
}

// TestHandleWebhook_RejectsUnsetSecret: an empty configured secret_token makes
// the receiver fail closed — every request is 401 even with a matching header.
func TestHandleWebhook_RejectsUnsetSecret(t *testing.T) {
	srv, _ := newTelegramAPIStub(t)
	host := newTestHost(t, srv.URL)
	host.cfg.TelegramInteractive.SecretToken = "" // disable → fail closed
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "")

	req := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(callbackBody(t, "ack "+uid, "alice"))))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "")
	req = req.WithContext(tenantCtx())
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "", recordState(t, host, uid))
}

// TestHandleWebhook_AppliesAck: a valid secret + callback_query data "ack <uid>"
// transitions the record to "ack", edits the message in place, and answers the
// callback. HTTP 200.
func TestHandleWebhook_AppliesAck(t *testing.T) {
	srv, stub := newTelegramAPIStub(t)
	host := newTestHost(t, srv.URL)
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "")

	req := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(callbackBody(t, "ack "+uid, "alice"))))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", testSecret)
	req = req.WithContext(tenantCtx())
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ack", recordState(t, host, uid), "valid ack must transition the record")

	stub.mu.Lock()
	defer stub.mu.Unlock()
	require.NotEmpty(t, stub.editText, "message must be edited in place")
	require.True(t, stub.answered, "callback query must be answered to clear the spinner")
}

// TestHandleWebhook_IllegalTransition: ack of an already-acked record is refused
// — the record stays "ack", the message is edited with a refusal, and the
// response is still 200 (chat-client UX: a non-200 surfaces as a client error).
func TestHandleWebhook_IllegalTransition(t *testing.T) {
	srv, stub := newTelegramAPIStub(t)
	host := newTestHost(t, srv.URL)
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "ack")

	req := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(callbackBody(t, "ack "+uid, "bob"))))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", testSecret)
	req = req.WithContext(tenantCtx())
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "chat UX: refusal is still HTTP 200")
	require.Equal(t, "ack", recordState(t, host, uid), "illegal transition must leave state unchanged")

	stub.mu.Lock()
	defer stub.mu.Unlock()
	require.NotEmpty(t, stub.editText, "a refusal message must be edited in")
	require.True(t, stub.answered)
}
