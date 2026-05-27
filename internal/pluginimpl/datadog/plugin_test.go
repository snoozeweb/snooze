package datadog

import (
	"bytes"
	"context"
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
// recordProcessor interface used by the datadog plugin. ProcessRecord captures
// every record passed in.
type fakeHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("datadog-test") }
func (h *fakeHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *fakeHost) Config() *config.Config       { return config.Default() }
func (h *fakeHost) Plugin(string) plugins.Plugin { return nil }

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
	p := &Plugin{meta: plugins.Metadata{Name: "datadog"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postWebhook posts body to the plugin's HandleWebhook and returns the
// recorded response.
func postWebhook(t *testing.T, p *Plugin, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/datadog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "datadog"))
}

func TestPluginContract(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "datadog"}}
	require.Equal(t, "datadog", p.Name())
	require.Equal(t, "/datadog", p.WebhookPath())
	require.Equal(t, "datadog", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))

	// Compile-time proof the plugin satisfies the WebhookReceiver interface.
	var _ plugins.WebhookReceiver = p
}

// triggeredPayload is the recommended JSON template body for a "triggered"
// Datadog monitor alert with alert_type "error".
var triggeredPayload = []byte(`{
	"alert_id": "123456789",
	"title": "[Triggered] High CPU on web-1",
	"body": "CPU usage exceeded threshold on web-1",
	"event_type": "triggered",
	"alert_type": "error",
	"alert_transition": "Triggered",
	"date": 1716800000000,
	"org_id": "99",
	"host": "web-1",
	"tags": "service:nginx,env:prod,team:ops",
	"priority": "normal",
	"aggreg_key": "agg-001",
	"link": "https://app.datadoghq.com/monitors/123456789"
}`)

func TestTriggeredErrorEmitsCriticalRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, triggeredPayload)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.EqualValues(t, 1, resp["received"])
	require.EqualValues(t, 1, resp["accepted"])

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "datadog", rec.Source)
	require.Equal(t, "web-1", rec.Host)
	require.Equal(t, "critical", rec.Severity)
	require.Equal(t, "[Triggered] High CPU on web-1", rec.Message)
	require.Empty(t, rec.State)

	// Process extracted from service: tag.
	require.Equal(t, "nginx", rec.Process)

	// Tags split from comma string.
	require.Equal(t, []string{"service:nginx", "env:prod", "team:ops"}, rec.Tags)

	// Raw fields.
	require.Equal(t, "123456789", rec.Raw["alert_id"])
	require.Equal(t, "agg-001", rec.Raw["aggreg_key"])
	require.Equal(t, "https://app.datadoghq.com/monitors/123456789", rec.Raw["link"])
	require.Equal(t, "triggered", rec.Raw["event_type"])
	require.Equal(t, "error", rec.Raw["alert_type"])
	require.Equal(t, "normal", rec.Raw["priority"])
	require.Equal(t, "99", rec.Raw["org_id"])
	require.Equal(t, "service:nginx,env:prod,team:ops", rec.Raw["tags"])
}

// recoveredPayload is the body produced by Datadog for a monitor recovery
// (alert_type "success", alert_transition "Recovered").
var recoveredPayload = []byte(`{
	"alert_id": "123456789",
	"title": "[Recovered] High CPU on web-1",
	"body": "CPU usage is back to normal on web-1",
	"event_type": "recovered",
	"alert_type": "success",
	"alert_transition": "Recovered",
	"date": 1716800300000,
	"org_id": "99",
	"host": "web-1",
	"tags": "service:nginx,env:prod",
	"priority": "normal",
	"aggreg_key": "agg-001",
	"link": "https://app.datadoghq.com/monitors/123456789"
}`)

func TestRecoveredSuccessEmitsCloseInfoRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, recoveredPayload)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "close", rec.State)
	require.Equal(t, "info", rec.Severity)
	require.Equal(t, "datadog", rec.Source)
	require.Equal(t, "web-1", rec.Host)
	require.Equal(t, "[Recovered] High CPU on web-1", rec.Message)
}

// warningPayload tests alert_type "warning" → severity "warning", no close.
var warningPayload = []byte(`{
	"alert_id": "555",
	"title": "[Triggered] Disk usage warning",
	"body": "Disk usage at 80%",
	"event_type": "triggered",
	"alert_type": "warning",
	"alert_transition": "Triggered",
	"date": 1716800000000,
	"org_id": "10",
	"host": "db-2",
	"tags": "process:mysqld,env:staging",
	"priority": "low",
	"aggreg_key": "",
	"link": ""
}`)

func TestWarningAlertTypeMapsToWarningSeverity(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, warningPayload)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "warning", rec.Severity)
	require.Empty(t, rec.State)
	// Process extracted from process: tag.
	require.Equal(t, "mysqld", rec.Process)
	require.Equal(t, "db-2", rec.Host)
}

// infoAlertPayload tests alert_type "info" → severity "info".
var infoAlertPayload = []byte(`{
	"alert_id": "777",
	"title": "Info event",
	"body": "Something informational",
	"event_type": "triggered",
	"alert_type": "info",
	"alert_transition": "Triggered",
	"date": 1716800000000,
	"org_id": "10",
	"host": "mon-1",
	"tags": "",
	"priority": "normal",
	"aggreg_key": "",
	"link": ""
}`)

func TestInfoAlertTypeMapsToInfoSeverity(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, infoAlertPayload)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "info", recs[0].Severity)
	// Empty tags string → empty Tags slice.
	require.Empty(t, recs[0].Tags)
}

// hostFallbackPayload has no "host" field: should fall back to alert_id.
var hostFallbackPayload = []byte(`{
	"alert_id": "fallback-42",
	"title": "No host alert",
	"body": "Some alert with no host",
	"event_type": "triggered",
	"alert_type": "error",
	"alert_transition": "Triggered",
	"date": 1716800000000,
	"org_id": "1",
	"host": "",
	"tags": "",
	"priority": "normal",
	"aggreg_key": "",
	"link": ""
}`)

func TestMissingHostFallsBackToAlertID(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, hostFallbackPayload)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "fallback-42", recs[0].Host)
}

func TestMalformedJSONReturns400(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(`{not-json`))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.seen())
	require.True(t,
		strings.Contains(w.Body.String(), "invalid Datadog payload"),
		"body=%q", w.Body.String(),
	)
}

func TestWrongMethodReturns405(t *testing.T) {
	p := newPlugin(t, &fakeHost{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/datadog", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

func TestPipelineErrorIsNotFatal(t *testing.T) {
	host := &fakeHost{err: errors.New("boom")}
	p := newPlugin(t, host)

	w := postWebhook(t, p, triggeredPayload)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// Record errors → none accepted, but the response is still 200.
	require.EqualValues(t, 0, resp["accepted"])
	require.Len(t, host.seen(), 1)
}

func TestNoRecordProcessorDegradesGracefully(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "datadog"}}
	// PostInit with a host that does NOT satisfy recordProcessor.
	require.NoError(t, p.PostInit(context.Background(), &nakedPluginHost{}))

	w := postWebhook(t, p, triggeredPayload)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// With no pipeline, records still count as "accepted" (no-op success).
	require.EqualValues(t, 1, resp["accepted"])
}

// nakedPluginHost is a plugins.Host that does not satisfy recordProcessor.
type nakedPluginHost struct{}

func (nakedPluginHost) DB() db.Driver                { return nil }
func (nakedPluginHost) Bus() plugins.Bus             { return nil }
func (nakedPluginHost) Logger() *slog.Logger         { return slog.Default() }
func (nakedPluginHost) Tracer() trace.Tracer         { return otel.Tracer("datadog-test") }
func (nakedPluginHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nakedPluginHost) Config() *config.Config       { return config.Default() }
func (nakedPluginHost) Plugin(string) plugins.Plugin { return nil }
