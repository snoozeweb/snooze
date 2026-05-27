package azuremonitor

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
// recordProcessor interface used by the azuremonitor plugin. ProcessRecord
// captures every record passed in.
type fakeHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("azuremonitor-test") }
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
	p := &Plugin{meta: plugins.Metadata{Name: "azuremonitor"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postWebhook is a small helper that posts body to the plugin's HandleWebhook
// and returns the recorded response.
func postWebhook(t *testing.T, p *Plugin, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/azuremonitor", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "azuremonitor"))
}

func TestPluginContract(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "azuremonitor"}}
	require.Equal(t, "azuremonitor", p.Name())
	require.Equal(t, "/azuremonitor", p.WebhookPath())
	require.Equal(t, "azuremonitor", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))

	// Ensure the plugin satisfies the WebhookReceiver interface at compile time.
	var _ plugins.WebhookReceiver = p
}

// firedBody is a realistic Common Alert Schema "Fired" Sev1 payload.
const firedBody = `{
	"schemaId": "azureMonitorCommonAlertSchema",
	"data": {
		"essentials": {
			"alertId": "/subscriptions/sub-1/providers/Microsoft.AlertsManagement/alerts/alert-1",
			"alertRule": "High CPU on web-1",
			"severity": "Sev1",
			"signalType": "Metric",
			"monitorCondition": "Fired",
			"monitoringService": "Platform",
			"alertTargetIDs": [
				"/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.Compute/virtualMachines/web-1"
			],
			"firedDateTime": "2026-05-27T10:00:00Z",
			"description": "CPU exceeded 90% threshold"
		},
		"alertContext": {
			"properties": {"threshold": "90"},
			"conditionType": "SingleResourceMultipleMetricCriteria"
		}
	}
}`

func TestFiredSev1EmitsCriticalRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(firedBody))
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.EqualValues(t, 1, resp["received"])
	require.EqualValues(t, 1, resp["accepted"])

	recs := host.seen()
	require.Len(t, recs, 1)
	r := recs[0]

	require.Equal(t, "azuremonitor", r.Source)
	require.Equal(t, "critical", r.Severity)
	// Host is the last "/"-segment of alertTargetIDs[0].
	require.Equal(t, "web-1", r.Host)
	// Process = monitoringService + "/" + signalType.
	require.Equal(t, "Platform/Metric", r.Process)
	// Message = description.
	require.Equal(t, "CPU exceeded 90% threshold", r.Message)
	// Fired → no state.
	require.Empty(t, r.State)
	// Raw contains essentials fields.
	require.Equal(t, "Sev1", r.Raw["severity"])
	require.Equal(t, "Fired", r.Raw["monitorCondition"])
	require.NotNil(t, r.Raw["alertContext"])
}

// resolvedBody is a Common Alert Schema "Resolved" payload.
const resolvedBody = `{
	"schemaId": "azureMonitorCommonAlertSchema",
	"data": {
		"essentials": {
			"alertId": "/subscriptions/sub-1/providers/Microsoft.AlertsManagement/alerts/alert-1",
			"alertRule": "High CPU on web-1",
			"severity": "Sev1",
			"signalType": "Metric",
			"monitorCondition": "Resolved",
			"monitoringService": "Platform",
			"alertTargetIDs": [
				"/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.Compute/virtualMachines/web-1"
			],
			"firedDateTime": "2026-05-27T10:00:00Z",
			"resolvedDateTime": "2026-05-27T10:15:00Z",
			"description": "CPU back to normal"
		},
		"alertContext": {}
	}
}`

func TestResolvedEmitsCloseInfo(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(resolvedBody))
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	r := recs[0]

	require.Equal(t, "close", r.State)
	require.Equal(t, "info", r.Severity)
	require.Equal(t, "azuremonitor", r.Source)
	require.Equal(t, "web-1", r.Host)
	require.Equal(t, "CPU back to normal", r.Message)
	require.Equal(t, "Resolved", r.Raw["monitorCondition"])
}

// sev3Body is a Common Alert Schema "Fired" Sev3 payload.
const sev3Body = `{
	"schemaId": "azureMonitorCommonAlertSchema",
	"data": {
		"essentials": {
			"alertId": "/subscriptions/sub-1/providers/Microsoft.AlertsManagement/alerts/alert-3",
			"alertRule": "Disk space low",
			"severity": "Sev3",
			"signalType": "Metric",
			"monitorCondition": "Fired",
			"monitoringService": "Platform",
			"alertTargetIDs": [
				"/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.Compute/virtualMachines/db-1"
			],
			"firedDateTime": "2026-05-27T11:00:00Z",
			"description": "Disk usage above 80%"
		},
		"alertContext": {}
	}
}`

func TestSev3EmitsWarning(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(sev3Body))
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "warning", recs[0].Severity)
	require.Equal(t, "db-1", recs[0].Host)
	require.Empty(t, recs[0].State)
}

func TestMalformedJSONReturns400(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(`{not-json`))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.seen())
	require.True(t,
		strings.Contains(w.Body.String(), "invalid Azure Monitor payload"),
		"body=%q", w.Body.String(),
	)
}

func TestWrongMethodReturns405(t *testing.T) {
	p := newPlugin(t, &fakeHost{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/azuremonitor", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

func TestPipelineErrorIsNotFatal(t *testing.T) {
	host := &fakeHost{err: errors.New("pipeline boom")}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(firedBody))
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// Record errored → none accepted, but response is still 200.
	require.EqualValues(t, 0, resp["accepted"])
	// ProcessRecord was still called.
	require.Len(t, host.seen(), 1)
}

func TestNoRecordProcessorDegradesGracefully(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "azuremonitor"}}
	// PostInit with a host that does NOT satisfy recordProcessor.
	require.NoError(t, p.PostInit(context.Background(), &nakedPluginHost{}))

	w := postWebhook(t, p, []byte(firedBody))
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
func (nakedPluginHost) Tracer() trace.Tracer         { return otel.Tracer("azuremonitor-test") }
func (nakedPluginHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nakedPluginHost) Config() *config.Config       { return config.Default() }
func (nakedPluginHost) Plugin(string) plugins.Plugin { return nil }
