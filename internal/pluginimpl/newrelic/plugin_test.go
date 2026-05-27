package newrelic

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
// recordProcessor interface used by the newrelic plugin. ProcessRecord captures
// every record passed in.
type fakeHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("newrelic-test") }
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
	p := &Plugin{meta: plugins.Metadata{Name: "newrelic"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postWebhook is a small helper that posts body to the plugin's HandleWebhook
// and returns the recorded response.
func postWebhook(t *testing.T, p *Plugin, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/newrelic", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "newrelic"))
}

func TestPluginContract(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "newrelic"}}
	require.Equal(t, "newrelic", p.Name())
	require.Equal(t, "/newrelic", p.WebhookPath())
	require.Equal(t, "newrelic", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))

	// Ensure the plugin satisfies the WebhookReceiver interface at compile time.
	var _ plugins.WebhookReceiver = p
}

func TestWorkflowActivatedCriticalEmitsRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"id": "issue-abc-123",
		"issueUrl": "https://one.newrelic.com/alerts-ai/issues/abc-123",
		"title": "High error rate on payment-service",
		"priority": "CRITICAL",
		"state": "ACTIVATED",
		"trigger": "INCIDENT_ADDED",
		"timestamp": 1716800000000,
		"accountName": "Acme Corp",
		"totalIncidents": 2,
		"owner": "team-backend",
		"impactedEntities": ["payment-service", "checkout-service"],
		"labels": {"env": "production", "team": "backend"}
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

	require.Equal(t, "newrelic", rec.Source)
	require.Equal(t, "payment-service", rec.Host) // first impacted entity
	require.Equal(t, "critical", rec.Severity)
	require.Equal(t, "High error rate on payment-service", rec.Message)
	require.Empty(t, rec.State) // ACTIVATED → not closed
	require.NotNil(t, rec.Raw)
	require.Equal(t, "https://one.newrelic.com/alerts-ai/issues/abc-123", rec.Raw["issueUrl"])
	require.Equal(t, "CRITICAL", rec.Raw["priority"])
	require.Equal(t, "ACTIVATED", rec.Raw["state"])
	require.Equal(t, "Acme Corp", rec.Raw["accountName"])
}

func TestWorkflowClosedEmitsCloseRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"id": "issue-xyz-456",
		"issueUrl": "https://one.newrelic.com/alerts-ai/issues/xyz-456",
		"title": "High error rate on payment-service",
		"priority": "CRITICAL",
		"state": "CLOSED",
		"trigger": "INCIDENT_CLOSED",
		"timestamp": 1716801000000,
		"accountName": "Acme Corp",
		"totalIncidents": 0,
		"impactedEntities": ["payment-service"],
		"labels": {}
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "close", rec.State)
	require.Equal(t, "info", rec.Severity) // CRITICAL close → info
	require.Equal(t, "newrelic", rec.Source)
	require.Equal(t, "payment-service", rec.Host)
}

func TestWorkflowHighPriorityMapsToError(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"title": "Slow response times",
		"priority": "HIGH",
		"state": "ACTIVATED",
		"impactedEntities": ["api-gateway"],
		"labels": {}
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "error", recs[0].Severity)
}

func TestWorkflowNoImpactedEntitiesFallsBackToTitle(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"title": "Database connection pool exhausted",
		"priority": "MEDIUM",
		"state": "ACTIVATED",
		"impactedEntities": [],
		"labels": {}
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	// No impacted entities → host falls back to title.
	require.Equal(t, "Database connection pool exhausted", recs[0].Host)
	require.Equal(t, "warning", recs[0].Severity)
}

func TestLegacyDefaultWebhookMapsCorrectly(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"incident_id": 99001,
		"condition_name": "High CPU usage",
		"details": "CPU at 95% on web-3",
		"severity": "CRITICAL",
		"current_state": "open",
		"policy_name": "Infrastructure Policy",
		"targets": [
			{"name": "web-3", "type": "Host", "labels": {"environment": "prod"}}
		],
		"incident_url": "https://alerts.newrelic.com/accounts/1/incidents/99001",
		"account_name": "Acme Corp"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "newrelic", rec.Source)
	require.Equal(t, "web-3", rec.Host) // from targets[0].name
	require.Equal(t, "critical", rec.Severity)
	require.Equal(t, "High CPU usage: CPU at 95% on web-3", rec.Message)
	require.Empty(t, rec.State) // open → not closed
	require.Equal(t, "https://alerts.newrelic.com/accounts/1/incidents/99001", rec.Raw["incident_url"])
	require.Equal(t, "CRITICAL", rec.Raw["severity"])
	require.Equal(t, "open", rec.Raw["current_state"])
}

func TestLegacyClosedStateEmitsCloseRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"incident_id": 99002,
		"condition_name": "High CPU usage",
		"details": "Resolved",
		"severity": "CRITICAL",
		"current_state": "closed",
		"policy_name": "Infrastructure Policy",
		"targets": [{"name": "web-3", "type": "Host", "labels": {}}],
		"incident_url": "https://alerts.newrelic.com/accounts/1/incidents/99002",
		"account_name": "Acme Corp"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "close", rec.State)
	require.Equal(t, "info", rec.Severity)
}

func TestLegacyNoTargetsFallsBackToConditionName(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"incident_id": 99003,
		"condition_name": "Disk usage alert",
		"details": "",
		"severity": "LOW",
		"current_state": "open",
		"targets": [],
		"account_name": "Acme Corp"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "Disk usage alert", recs[0].Host)
	require.Equal(t, "info", recs[0].Severity) // LOW → info
	// No details → message is just condition_name
	require.Equal(t, "Disk usage alert", recs[0].Message)
}

func TestMalformedJSONReturns400(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(`{not-json`))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.seen())
	require.True(t,
		strings.Contains(w.Body.String(), "invalid New Relic payload"),
		"body=%q", w.Body.String(),
	)
}

func TestWrongMethodReturns405(t *testing.T) {
	p := newPlugin(t, &fakeHost{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/newrelic", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

func TestPipelineErrorIsNotFatal(t *testing.T) {
	host := &fakeHost{err: errors.New("pipeline boom")}
	p := newPlugin(t, host)

	body := []byte(`{
		"title": "Some alert",
		"priority": "HIGH",
		"state": "ACTIVATED",
		"impactedEntities": ["svc-a"],
		"labels": {}
	}`)
	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// Pipeline error → not accepted, but response is still 200.
	require.EqualValues(t, 0, resp["accepted"])
	// The record was still attempted.
	require.Len(t, host.seen(), 1)
}

func TestNoProcessorDegradesGracefully(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "newrelic"}}
	// PostInit with a host that does NOT satisfy recordProcessor.
	require.NoError(t, p.PostInit(context.Background(), &nakedPluginHost{}))

	body := []byte(`{
		"title": "Some alert",
		"priority": "CRITICAL",
		"state": "ACTIVATED",
		"impactedEntities": ["svc-b"],
		"labels": {}
	}`)
	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// With no pipeline, record still counts as "accepted" (no-op success).
	require.EqualValues(t, 1, resp["accepted"])
}

// nakedPluginHost is a plugins.Host that does not satisfy recordProcessor.
type nakedPluginHost struct{}

func (nakedPluginHost) DB() db.Driver                { return nil }
func (nakedPluginHost) Bus() plugins.Bus             { return nil }
func (nakedPluginHost) Logger() *slog.Logger         { return slog.Default() }
func (nakedPluginHost) Tracer() trace.Tracer         { return otel.Tracer("newrelic-test") }
func (nakedPluginHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nakedPluginHost) Config() *config.Config       { return config.Default() }
func (nakedPluginHost) Plugin(string) plugins.Plugin { return nil }
