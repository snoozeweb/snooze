package sentry

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeHost is a minimal plugins.Host that additionally satisfies the local
// recordProcessor interface used by the sentry plugin. ProcessRecord captures
// every record passed in.
type fakeHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error

	// cfg, when non-nil, is returned from Config(). It lets a test inject a
	// custom Ingest section (e.g. a configured SentrySecret). When nil the
	// host returns config.Default(), matching production defaults (verify off).
	cfg *config.Config
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("sentry-test") }
func (h *fakeHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *fakeHost) Config() *config.Config {
	if h.cfg != nil {
		return h.cfg
	}
	return config.Default()
}
func (h *fakeHost) Plugin(string) plugins.Plugin { return nil }

// hostWithSecret builds a fakeHost whose Config().Ingest.SentrySecret is set,
// enabling HMAC verification.
func hostWithSecret(secret string) *fakeHost {
	cfg := config.Default()
	cfg.Ingest.SentrySecret = secret
	return &fakeHost{cfg: cfg}
}

// ProcessRecord makes *fakeHost satisfy the plugin's internal recordProcessor
// runtime assertion.
func (h *fakeHost) ProcessRecord(_ context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	if h.err != nil {
		return rec, plugins.ActionAbort, h.err
	}
	return rec, plugins.ActionContinue, nil
}

func (h *fakeHost) seen() []snoozetypes.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]snoozetypes.Record, len(h.records))
	copy(out, h.records)
	return out
}

func newPlugin(t *testing.T, host plugins.Host) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "sentry"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postWebhook is a small helper that posts body to the plugin's HandleWebhook
// and returns the recorded response.
func postWebhook(t *testing.T, p *Plugin, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return postWebhookH(t, p, body, nil)
}

// postWebhookH posts body with optional extra headers to the plugin's
// HandleWebhook and returns the recorded response.
func postWebhookH(t *testing.T, p *Plugin, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/sentry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

// sign computes the hex-encoded HMAC-SHA256 of body keyed by secret, matching
// the construction expected on the sentry-hook-signature header.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// Registration & contract
// ---------------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "sentry"))
}

func TestPluginContract(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "sentry"}}
	require.Equal(t, "sentry", p.Name())
	require.Equal(t, "/sentry", p.WebhookPath())
	require.Equal(t, "sentry", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))

	// Ensure the plugin satisfies the WebhookReceiver interface at compile time.
	var _ plugins.WebhookReceiver = p
}

// ---------------------------------------------------------------------------
// Legacy webhook shape
// ---------------------------------------------------------------------------

func TestLegacyPayloadEmitsRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"id": "1",
		"project": "my-project",
		"project_name": "My Project",
		"culprit": "pkg/foo.Bar",
		"message": "Something broke",
		"url": "https://sentry.io/my-org/my-project/issues/1/",
		"level": "error",
		"server_name": "web-01",
		"event": {
			"event_id": "abc123",
			"tags": [["server_name", "web-01"], ["env", "production"]],
			"environment": "production",
			"release": "1.0.0"
		}
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.EqualValues(t, 1, resp["received"])
	require.EqualValues(t, 1, resp["accepted"])

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "sentry", rec.Source)
	require.Equal(t, "web-01", rec.Host)
	require.Equal(t, "my-project", rec.Process)
	require.Equal(t, "critical", rec.Severity) // "error" → critical
	require.Equal(t, "Something broke", rec.Message)
	require.Empty(t, rec.State)
	require.NotNil(t, rec.Raw)
	require.Equal(t, "https://sentry.io/my-org/my-project/issues/1/", rec.Raw["url"])
	require.Equal(t, "my-project", rec.Raw["project"])
	require.Equal(t, "abc123", rec.Raw["event_id"])
	require.Equal(t, "production", rec.Raw["environment"])
	require.Equal(t, "1.0.0", rec.Raw["release"])
	require.Equal(t, "error", rec.Raw["level"])
}

func TestLegacySeverityMapping(t *testing.T) {
	cases := []struct {
		level    string
		expected string
	}{
		{"fatal", "critical"},
		{"error", "critical"},
		{"warning", "warning"},
		{"info", "info"},
		{"debug", "info"},
		{"unknown", "critical"},
		{"", "critical"},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			require.Equal(t, tc.expected, mapSeverity(tc.level))
		})
	}
}

func TestLegacyHostFallbackToEventTagsServerName(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	// server_name is absent at top level, but present in event.tags.
	body := []byte(`{
		"project": "proj",
		"message": "msg",
		"level": "warning",
		"event": {
			"tags": [["server_name", "db-02"]]
		}
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "db-02", recs[0].Host)
	require.Equal(t, "warning", recs[0].Severity)
}

func TestLegacyHostFallbackToProject(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	// No server_name anywhere, fall back to project.
	body := []byte(`{
		"project": "fallback-proj",
		"project_name": "Fallback Project",
		"message": "hello",
		"level": "info"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "fallback-proj", recs[0].Host)
	require.Equal(t, "fallback-proj", recs[0].Process)
	require.Equal(t, "info", recs[0].Severity)
}

// ---------------------------------------------------------------------------
// Modern Integration shape
// ---------------------------------------------------------------------------

func TestModernIssuePayloadEmitsRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"action": "triggered",
		"data": {
			"issue": {
				"title": "ZeroDivisionError: division by zero",
				"culprit": "divide_things in app.py",
				"level": "error",
				"permalink": "https://sentry.io/organizations/myorg/issues/42/",
				"project": {
					"slug": "backend",
					"name": "Backend Service"
				}
			},
			"triggered_rule": "High error rate"
		}
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.EqualValues(t, 1, resp["received"])
	require.EqualValues(t, 1, resp["accepted"])

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "sentry", rec.Source)
	require.Equal(t, "backend", rec.Host)
	require.Equal(t, "backend", rec.Process)
	require.Equal(t, "critical", rec.Severity) // "error" → critical
	require.Equal(t, "ZeroDivisionError: division by zero", rec.Message)
	require.Empty(t, rec.State)
	require.NotNil(t, rec.Raw)
	require.Equal(t, "https://sentry.io/organizations/myorg/issues/42/", rec.Raw["url"])
	require.Equal(t, "backend", rec.Raw["project"])
	require.Equal(t, "error", rec.Raw["level"])
}

func TestModernResolvedActionSetsStateClose(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"action": "resolved",
		"data": {
			"issue": {
				"title": "ZeroDivisionError: division by zero",
				"level": "error",
				"permalink": "https://sentry.io/organizations/myorg/issues/42/",
				"project": {
					"slug": "backend"
				}
			}
		}
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "close", recs[0].State)
	require.Equal(t, "critical", recs[0].Severity) // level "error" still maps to critical even on resolve
}

func TestModernEventPayload(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"action": "created",
		"data": {
			"event": {
				"event_id": "deadbeef",
				"level": "warning",
				"title": "High memory usage",
				"server_name": "app-server-3",
				"environment": "staging",
				"release": "2.1.0",
				"project": "frontend"
			}
		}
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "sentry", rec.Source)
	require.Equal(t, "app-server-3", rec.Host)
	require.Equal(t, "frontend", rec.Process)
	require.Equal(t, "warning", rec.Severity)
	require.Equal(t, "High memory usage", rec.Message)
	require.Equal(t, "deadbeef", rec.Raw["event_id"])
	require.Equal(t, "staging", rec.Raw["environment"])
	require.Equal(t, "2.1.0", rec.Raw["release"])
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestMalformedJSONReturns400(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(`{not-json`))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.seen())
	require.True(t,
		strings.Contains(w.Body.String(), "invalid Sentry payload"),
		"body=%q", w.Body.String(),
	)
}

func TestWrongMethodReturns405(t *testing.T) {
	p := newPlugin(t, &fakeHost{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/sentry", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

func TestPipelineErrorIsNotFatal(t *testing.T) {
	host := &fakeHost{err: errors.New("boom")}
	p := newPlugin(t, host)

	body := []byte(`{
		"project": "proj",
		"message": "msg",
		"level": "error"
	}`)
	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// Record errored → not accepted, but the response is still 200.
	require.EqualValues(t, 0, resp["accepted"])
	require.Len(t, host.seen(), 1)
}

func TestNoRecordProcessorDegradesGracefully(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "sentry"}}
	// PostInit with a host that does NOT satisfy recordProcessor.
	require.NoError(t, p.PostInit(context.Background(), &nakedPluginHost{}))

	body := []byte(`{
		"project": "proj",
		"message": "msg",
		"level": "error"
	}`)
	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// With no pipeline, records still count as "accepted" (no-op success).
	require.EqualValues(t, 1, resp["accepted"])
}

// ---------------------------------------------------------------------------
// Inbound HMAC signature verification (opt-in via Ingest.SentrySecret)
// ---------------------------------------------------------------------------

const testSentrySecret = "s3cr3t-sentry-client-secret"

// TestSignatureVerificationAcceptsCorrectlySignedBody asserts that, with a
// configured secret, a body carrying a valid sentry-hook-signature is
// processed normally.
func TestSignatureVerificationAcceptsCorrectlySignedBody(t *testing.T) {
	host := hostWithSecret(testSentrySecret)
	p := newPlugin(t, host)

	body := []byte(`{
		"project": "proj",
		"message": "signed message",
		"level": "error"
	}`)

	w := postWebhookH(t, p, body, map[string]string{
		"sentry-hook-signature": sign(testSentrySecret, body),
	})
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["accepted"])

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "signed message", recs[0].Message)
}

// TestSignatureVerificationRejectsWrongSignature asserts that a body with an
// incorrect signature is rejected with 403 and never processed.
func TestSignatureVerificationRejectsWrongSignature(t *testing.T) {
	host := hostWithSecret(testSentrySecret)
	p := newPlugin(t, host)

	body := []byte(`{
		"project": "proj",
		"message": "tampered",
		"level": "error"
	}`)

	// Sign with the WRONG secret to produce a mismatching signature.
	w := postWebhookH(t, p, body, map[string]string{
		"sentry-hook-signature": sign("wrong-secret", body),
	})
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Empty(t, host.seen())
}

// TestSignatureVerificationRejectsMissingHeader asserts that, with a configured
// secret, a body lacking the signature header is rejected with 403.
func TestSignatureVerificationRejectsMissingHeader(t *testing.T) {
	host := hostWithSecret(testSentrySecret)
	p := newPlugin(t, host)

	body := []byte(`{
		"project": "proj",
		"message": "unsigned",
		"level": "error"
	}`)

	// No sentry-hook-signature header at all.
	w := postWebhookH(t, p, body, nil)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Empty(t, host.seen())
}

// TestNoSecretConfiguredAcceptsUnsignedBody asserts today's behavior is
// preserved: with no secret configured (the default), an unsigned body is
// processed exactly as before.
func TestNoSecretConfiguredAcceptsUnsignedBody(t *testing.T) {
	host := &fakeHost{} // config.Default() → SentrySecret == ""
	p := newPlugin(t, host)

	body := []byte(`{
		"project": "proj",
		"message": "unsigned-but-ok",
		"level": "error"
	}`)

	w := postWebhookH(t, p, body, nil)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "unsigned-but-ok", recs[0].Message)
}

// nakedPluginHost is a plugins.Host that does not satisfy recordProcessor.
type nakedPluginHost struct{}

func (nakedPluginHost) DB() db.Driver                { return nil }
func (nakedPluginHost) Bus() plugins.Bus             { return nil }
func (nakedPluginHost) Logger() *slog.Logger         { return slog.Default() }
func (nakedPluginHost) Tracer() trace.Tracer         { return otel.Tracer("sentry-test") }
func (nakedPluginHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nakedPluginHost) Config() *config.Config       { return config.Default() }
func (nakedPluginHost) Plugin(string) plugins.Plugin { return nil }
