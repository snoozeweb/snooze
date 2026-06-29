package slackinteractive

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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

const testSigningSecret = "8f742231b10e8888abcd99yyyzzz85a5"

// fixedNow is the deterministic clock the tests inject for skew checks.
var fixedNow = time.Unix(1_700_000_000, 0).UTC()

type testHost struct {
	drv     *sqlite.Driver
	cfg     *config.Config
	comment plugins.Plugin
}

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	cfg := config.Default()
	cfg.SlackInteractive.SigningSecret = testSigningSecret

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
func (h *testHost) Tracer() trace.Tracer { return otel.Tracer("slackinteractive-test") }
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

func newPluginForTest(t *testing.T, host *testHost) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "slackinteractive"})
	require.NoError(t, err)
	sp := p.(*Plugin)
	require.NoError(t, sp.PostInit(context.Background(), host))
	sp.clock = func() time.Time { return fixedNow } // deterministic skew check
	return sp
}

// signV0 computes the Slack v0 signature for (ts, body) under secret.
func signV0(secret, ts, body string) string {
	base := "v0:" + ts + ":" + body
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

// slackPayload builds a Slack block_actions interaction payload JSON.
func slackPayload(t *testing.T, action, uid, username string) string {
	t.Helper()
	payload := map[string]any{
		"type": "block_actions",
		"user": map[string]any{"username": username},
		"actions": []map[string]any{
			{"value": action, "block_id": uid, "action_id": "snooze_" + action},
		},
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(b)
}

// formBody returns the application/x-www-form-urlencoded body Slack POSTs.
func formBody(payloadJSON string) string {
	v := url.Values{}
	v.Set("payload", payloadJSON)
	return v.Encode()
}

// signedRequest builds a POST to /slack signed for the given timestamp.
func signedRequest(t *testing.T, ts, payloadJSON string) *http.Request {
	t.Helper()
	body := formBody(payloadJSON)
	req := httptest.NewRequest(http.MethodPost, "/slack", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", signV0(testSigningSecret, ts, body))
	return req.WithContext(tenantCtx())
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "slackinteractive"))
}

func TestWebhookPath(t *testing.T) {
	p, err := factory(plugins.Metadata{Name: "slackinteractive"})
	require.NoError(t, err)
	wr, ok := p.(plugins.WebhookReceiver)
	require.True(t, ok)
	require.Equal(t, "/slack", wr.WebhookPath())
}

// TestVerifySignature_GoodAndBad is a table test over the constant-time HMAC
// verifier.
func TestVerifySignature_GoodAndBad(t *testing.T) {
	host := newTestHost(t)
	p := newPluginForTest(t, host)
	ts := strconv.FormatInt(fixedNow.Unix(), 10)
	body := formBody(slackPayload(t, "ack", "rec-1", "alice"))

	good := signV0(testSigningSecret, ts, body)
	cases := []struct {
		name string
		sig  string
		ts   string
		want bool
	}{
		{"good", good, ts, true},
		{"bad-signature", "v0=deadbeef", ts, false},
		{"wrong-secret", signV0("other-secret", ts, body), ts, false},
		{"empty-signature", "", ts, false},
		{"tampered-body-ts", good, strconv.FormatInt(fixedNow.Unix()+1, 10), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/slack", strings.NewReader(body))
			req.Header.Set("X-Slack-Request-Timestamp", tc.ts)
			req.Header.Set("X-Slack-Signature", tc.sig)
			require.Equal(t, tc.want, p.verifySignature(req, []byte(body)))
		})
	}
}

// TestHandleWebhook_RejectsBadSignature: a wrong signature is 401, no state
// change.
func TestHandleWebhook_RejectsBadSignature(t *testing.T) {
	host := newTestHost(t)
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "")

	ts := strconv.FormatInt(fixedNow.Unix(), 10)
	body := formBody(slackPayload(t, "ack", uid, "alice"))
	req := httptest.NewRequest(http.MethodPost, "/slack", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", "v0=deadbeef")
	req = req.WithContext(tenantCtx())
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "", recordState(t, host, uid))
}

// TestHandleWebhook_RejectsUnsetSecret: an empty signing_secret fails closed —
// every request 401s.
func TestHandleWebhook_RejectsUnsetSecret(t *testing.T) {
	host := newTestHost(t)
	host.cfg.SlackInteractive.SigningSecret = ""
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "")

	ts := strconv.FormatInt(fixedNow.Unix(), 10)
	req := signedRequest(t, ts, slackPayload(t, "ack", uid, "alice"))
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "", recordState(t, host, uid))
}

// TestHandleWebhook_RejectsStaleTimestamp: a timestamp more than 5 minutes from
// "now" is 401 even with a valid signature (replay defense).
func TestHandleWebhook_RejectsStaleTimestamp(t *testing.T) {
	host := newTestHost(t)
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "")

	staleTs := strconv.FormatInt(fixedNow.Unix()-6*60, 10) // 6 min ago
	req := signedRequest(t, staleTs, slackPayload(t, "ack", uid, "alice"))
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "", recordState(t, host, uid))
}

// TestHandleWebhook_AppliesAck: a valid signed request transitions the record to
// "ack" and returns a 200 message-replacement body that strips the buttons and
// adds a status line.
func TestHandleWebhook_AppliesAck(t *testing.T) {
	host := newTestHost(t)
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "")

	ts := strconv.FormatInt(fixedNow.Unix(), 10)
	req := signedRequest(t, ts, slackPayload(t, "ack", uid, "alice"))
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ack", recordState(t, host, uid), "valid ack must transition the record")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, true, resp["replace_original"], "must replace the original message")
	require.NotContains(t, rec.Body.String(), `"actions"`, "buttons must be stripped")
	require.Contains(t, strings.ToLower(rec.Body.String()), "alice", "status line must name the actor")
}

// TestHandleWebhook_IllegalTransition: ack of an already-acked record is refused
// — record unchanged, the 200 body states the refusal.
func TestHandleWebhook_IllegalTransition(t *testing.T) {
	host := newTestHost(t)
	p := newPluginForTest(t, host)
	uid := seedRecord(t, host, "ack")

	ts := strconv.FormatInt(fixedNow.Unix(), 10)
	req := signedRequest(t, ts, slackPayload(t, "ack", uid, "bob"))
	rec := httptest.NewRecorder()

	p.HandleWebhook(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "chat UX: refusal is still HTTP 200")
	require.Equal(t, "ack", recordState(t, host, uid), "illegal transition must leave state unchanged")
	require.Contains(t, strings.ToLower(rec.Body.String()), "cannot", "body must state the refusal")
}
