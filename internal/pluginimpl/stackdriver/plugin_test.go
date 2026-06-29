package stackdriver

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
// recordProcessor interface used by the stackdriver plugin. ProcessRecord
// captures every record passed in.
type fakeHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("stackdriver-test") }
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
	p := &Plugin{meta: plugins.Metadata{Name: "stackdriver"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postWebhook is a small helper that posts body to the plugin's HandleWebhook
// and returns the recorded response.
func postWebhook(t *testing.T, p *Plugin, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/stackdriver", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "stackdriver"))
}

func TestPluginContract(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "stackdriver"}}
	require.Equal(t, "stackdriver", p.Name())
	require.Equal(t, "/stackdriver", p.WebhookPath())
	require.Equal(t, "stackdriver", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))

	// Ensure the plugin satisfies the WebhookReceiver interface at compile time.
	var _ plugins.WebhookReceiver = p
}

// openBody is a realistic GCP Monitoring "open" incident payload without an
// explicit severity field (older notification-channel configs omit it).
const openBody = `{
	"incident": {
		"incident_id": "0.abc123",
		"resource_name": "web-1",
		"resource_id": "1234567890",
		"condition_name": "CPU above 90%",
		"policy_name": "High CPU policy",
		"state": "open",
		"summary": "CPU usage for web-1 is above the threshold",
		"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.abc123",
		"started_at": 1700000000,
		"ended_at": null
	}
}`

func TestOpenIncidentEmitsCriticalRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(openBody))
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.EqualValues(t, 1, resp["received"])
	require.EqualValues(t, 1, resp["accepted"])

	recs := host.seen()
	require.Len(t, recs, 1)
	r := recs[0]

	require.Equal(t, "stackdriver", r.Source)
	require.Equal(t, "critical", r.Severity)
	require.Empty(t, r.State)
	require.Equal(t, "web-1", r.Host)
	require.Equal(t, "CPU above 90%", r.Process)
	require.Equal(t, "CPU usage for web-1 is above the threshold", r.Message)
	require.Contains(t, r.Tags, "High CPU policy")
	require.Equal(t, "0.abc123", r.Raw["incident_id"])
	require.Equal(t, "1234567890", r.Raw["resource_id"])
	// ended_at is null for open incidents — it must NOT appear in Raw.
	_, hasEnded := r.Raw["ended_at"]
	require.False(t, hasEnded, "ended_at must be omitted from Raw when null")
}

// openWithSeverityBody is an "open" incident carrying an explicit severity.
const openWithSeverityBody = `{
	"incident": {
		"incident_id": "0.def456",
		"resource_name": "web-2",
		"resource_id": "2222222222",
		"condition_name": "Latency high",
		"policy_name": "Latency policy",
		"state": "open",
		"severity": "warning",
		"summary": "Latency for web-2 is elevated",
		"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.def456",
		"started_at": 1700000100,
		"ended_at": null
	}
}`

func TestOpenIncidentWithExplicitSeverity(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(openWithSeverityBody))
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "warning", recs[0].Severity)
	require.Empty(t, recs[0].State)
}

// acknowledgedBody is an "acknowledged" incident carrying an explicit severity.
const acknowledgedBody = `{
	"incident": {
		"incident_id": "0.ghi789",
		"resource_name": "web-3",
		"resource_id": "3333333333",
		"condition_name": "Memory high",
		"policy_name": "Memory policy",
		"state": "acknowledged",
		"severity": "warning",
		"summary": "Memory for web-3 is high",
		"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.ghi789",
		"started_at": 1700000200,
		"ended_at": null
	}
}`

func TestAcknowledgedIncidentEmitsAckRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(acknowledgedBody))
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "ack", recs[0].State)
	// Severity is preserved (the original value), not downgraded.
	require.Equal(t, "warning", recs[0].Severity)
}

// closedBody is a "closed" incident with a non-nil ended_at.
const closedBody = `{
	"incident": {
		"incident_id": "0.jkl012",
		"resource_name": "web-4",
		"resource_id": "4444444444",
		"condition_name": "Disk full",
		"policy_name": "Disk policy",
		"state": "closed",
		"severity": "critical",
		"summary": "Disk for web-4 recovered",
		"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.jkl012",
		"started_at": 1700000300,
		"ended_at": 1700000900
	}
}`

func TestClosedIncidentEmitsOkClose(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(closedBody))
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	r := recs[0]
	require.Equal(t, "ok", r.Severity)
	require.Equal(t, "close", r.State)
	// ended_at is present (non-nil) for closed incidents → it must appear in Raw.
	_, hasEnded := r.Raw["ended_at"]
	require.True(t, hasEnded, "ended_at must appear in Raw when non-nil")
}

// unknownStateBody is an incident in a state we do not recognise.
const unknownStateBody = `{
	"incident": {
		"incident_id": "0.mno345",
		"resource_name": "web-5",
		"resource_id": "5555555555",
		"condition_name": "Unknown condition",
		"policy_name": "Some policy",
		"state": "unknown_future",
		"summary": "Something happened",
		"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.mno345",
		"started_at": 1700000400,
		"ended_at": null
	}
}`

func TestUnknownStateEmitsIndeterminate(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(unknownStateBody))
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "indeterminate", recs[0].Severity)
	require.Empty(t, recs[0].State)
}

// docOverrideBody carries a documentation.content JSON blob overriding fields.
const docOverrideBody = `{
	"incident": {
		"incident_id": "0.pqr678",
		"resource_name": "web-6",
		"resource_id": "6666666666",
		"condition_name": "Override condition",
		"policy_name": "Override policy",
		"state": "open",
		"severity": "critical",
		"summary": "original summary",
		"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.pqr678",
		"started_at": 1700000500,
		"ended_at": null,
		"documentation": {
			"content": "{\"severity\":\"warning\",\"summary\":\"overridden\"}"
		}
	}
}`

func TestDocumentationOverridesFields(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(docOverrideBody))
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "warning", recs[0].Severity)
	require.Equal(t, "overridden", recs[0].Message)
}

// invalidDocBody carries a documentation.content that is not valid JSON.
const invalidDocBody = `{
	"incident": {
		"incident_id": "0.stu901",
		"resource_name": "web-7",
		"resource_id": "7777777777",
		"condition_name": "Bad doc condition",
		"policy_name": "Bad doc policy",
		"state": "open",
		"severity": "warning",
		"summary": "the real summary",
		"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.stu901",
		"started_at": 1700000600,
		"ended_at": null,
		"documentation": {
			"content": "this is not json {{{"
		}
	}
}`

func TestInvalidDocumentationContentIsIgnored(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(invalidDocBody))
	// Invalid documentation must not cause a 400 — the record is still built.
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	// Fields come from the raw incident, unchanged.
	require.Equal(t, "warning", recs[0].Severity)
	require.Equal(t, "the real summary", recs[0].Message)
}

func TestMalformedJSONReturns400(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(`{not-json`))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.seen())
	require.True(t,
		strings.Contains(w.Body.String(), "invalid Stackdriver payload"),
		"body=%q", w.Body.String(),
	)
}

func TestWrongMethodReturns405(t *testing.T) {
	p := newPlugin(t, &fakeHost{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/stackdriver", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

func TestPipelineErrorIsNotFatal(t *testing.T) {
	host := &fakeHost{err: errors.New("pipeline boom")}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(openBody))
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
	p := &Plugin{meta: plugins.Metadata{Name: "stackdriver"}}
	// PostInit with a host that does NOT satisfy recordProcessor.
	require.NoError(t, p.PostInit(context.Background(), &nakedPluginHost{}))

	w := postWebhook(t, p, []byte(openBody))
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// With no pipeline, records still count as "accepted" (no-op success).
	require.EqualValues(t, 1, resp["accepted"])
}

// emptyPolicyBody is an "open" incident with an empty policy_name.
const emptyPolicyBody = `{
	"incident": {
		"incident_id": "0.vwx234",
		"resource_name": "web-8",
		"resource_id": "8888888888",
		"condition_name": "No policy condition",
		"policy_name": "",
		"state": "open",
		"summary": "incident with no policy name",
		"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.vwx234",
		"started_at": 1700000700,
		"ended_at": null
	}
}`

func TestEmptyPolicyNameOmitsTags(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(emptyPolicyBody))
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Empty(t, recs[0].Tags, "Tags must be nil/empty when policy_name is empty")
}

// nakedPluginHost is a plugins.Host that does not satisfy recordProcessor.
type nakedPluginHost struct{}

func (nakedPluginHost) DB() db.Driver                { return nil }
func (nakedPluginHost) Bus() plugins.Bus             { return nil }
func (nakedPluginHost) Logger() *slog.Logger         { return slog.Default() }
func (nakedPluginHost) Tracer() trace.Tracer         { return otel.Tracer("stackdriver-test") }
func (nakedPluginHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nakedPluginHost) Config() *config.Config       { return config.Default() }
func (nakedPluginHost) Plugin(string) plugins.Plugin { return nil }
