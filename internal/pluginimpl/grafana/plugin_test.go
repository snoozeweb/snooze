package grafana

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

	"github.com/japannext/snooze/internal/config"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/telemetry"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// fakeHost is a minimal plugins.Host that additionally satisfies the local
// recordProcessor interface used by the grafana plugin. ProcessRecord captures
// every record passed in.
type fakeHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("grafana-test") }
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
	p := &Plugin{meta: plugins.Metadata{Name: "grafana"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postWebhook is a small helper that posts body to the plugin's HandleWebhook
// and returns the recorded response.
func postWebhook(t *testing.T, p *Plugin, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/grafana", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "grafana"))
}

func TestPluginContract(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "grafana"}}
	require.Equal(t, "grafana", p.Name())
	require.Equal(t, "/grafana", p.WebhookPath())
	require.Equal(t, "grafana", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))

	// Ensure the plugin satisfies the WebhookReceiver interface at compile time.
	var _ plugins.WebhookReceiver = p
}

func TestAlertingStateEmitsRecordPerMatch(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"title": "high cpu",
		"ruleId": 42,
		"ruleName": "CPU rule",
		"ruleUrl": "http://grafana/rules/42",
		"state": "alerting",
		"message": "CPU is on fire",
		"imageUrl": "http://grafana/image.png",
		"evalMatches": [
			{
				"metric": "cpu_usage",
				"value": 91.5,
				"tags": {"host": "web-1", "process": "nginx", "severity": "critical"}
			},
			{
				"metric": "cpu_usage",
				"value": 88.2,
				"tags": {"host": "web-2"}
			}
		]
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.EqualValues(t, 2, resp["received"])
	require.EqualValues(t, 2, resp["accepted"])

	recs := host.seen()
	require.Len(t, recs, 2)

	// First match: tags fully specified.
	require.Equal(t, "web-1", recs[0].Host)
	require.Equal(t, "nginx", recs[0].Process)
	require.Equal(t, "critical", recs[0].Severity)
	require.Equal(t, "grafana", recs[0].Source)
	require.Equal(t, "CPU is on fire", recs[0].Message)
	require.Empty(t, recs[0].State)
	require.NotNil(t, recs[0].Raw)
	require.Equal(t, "cpu_usage", recs[0].Raw["metric"])
	require.Equal(t, "alerting", recs[0].Raw["state"])
	require.Equal(t, "42", recs[0].Raw["ruleId"])

	// Second match: tags only provide host; process falls back to metric,
	// severity to "critical".
	require.Equal(t, "web-2", recs[1].Host)
	require.Equal(t, "cpu_usage", recs[1].Process)
	require.Equal(t, "critical", recs[1].Severity)
}

func TestOkStateEmitsCloseRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"title": "high cpu",
		"ruleId": 42,
		"ruleName": "CPU rule",
		"state": "ok",
		"message": "back to normal"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "close", recs[0].State)
	require.Equal(t, "info", recs[0].Severity)
	require.Equal(t, "grafana", recs[0].Source)
	require.Equal(t, "CPU rule", recs[0].Host)
	require.Equal(t, "CPU rule", recs[0].Process)
	require.Equal(t, "back to normal", recs[0].Message)
	require.Equal(t, "ok", recs[0].Raw["state"])
}

func TestNoDataStateEmitsWarning(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"title": "missing metric",
		"ruleName": "DiskRule",
		"state": "no_data"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "warning", recs[0].Severity)
	require.Empty(t, recs[0].State)
	require.Equal(t, "DiskRule", recs[0].Host)
	// Message falls back to title when message is empty.
	require.Equal(t, "missing metric", recs[0].Message)
}

func TestPausedStateEmitsNothing(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{"state": "paused", "ruleName": "x"}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	require.Empty(t, host.seen())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 0, resp["received"])
	require.EqualValues(t, 0, resp["accepted"])
}

func TestMalformedJSONReturns400(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(`{not-json`))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.seen())
	require.True(t,
		strings.Contains(w.Body.String(), "invalid Grafana payload"),
		"body=%q", w.Body.String(),
	)
}

func TestWrongMethodReturns405(t *testing.T) {
	p := newPlugin(t, &fakeHost{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/grafana", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

func TestPipelineErrorIsNotFatal(t *testing.T) {
	host := &fakeHost{err: errors.New("boom")}
	p := newPlugin(t, host)

	body := []byte(`{
		"state": "alerting",
		"ruleName": "rule",
		"evalMatches": [
			{"metric": "m1", "tags": {"host": "a"}},
			{"metric": "m2", "tags": {"host": "b"}}
		]
	}`)
	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 2, resp["received"])
	// Every record errors → none accepted, but the response is still 200.
	require.EqualValues(t, 0, resp["accepted"])
	require.Len(t, host.seen(), 2)
}

func TestNoRecordProcessorDegradesGracefully(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "grafana"}}
	// PostInit with a host that does NOT satisfy recordProcessor.
	require.NoError(t, p.PostInit(context.Background(), &nakedPluginHost{}))

	body := []byte(`{
		"state": "alerting",
		"ruleName": "rule",
		"evalMatches": [{"metric": "m1", "tags": {"host": "a"}}]
	}`)
	w := postWebhook(t, p, body)
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
func (nakedPluginHost) Tracer() trace.Tracer         { return otel.Tracer("grafana-test") }
func (nakedPluginHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nakedPluginHost) Config() *config.Config       { return config.Default() }
func (nakedPluginHost) Plugin(string) plugins.Plugin { return nil }
