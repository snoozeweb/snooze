package prometheus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/japannext/snooze/internal/config"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/telemetry"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// recordingHost is a plugins.Host whose ProcessRecord method captures every
// record the plugin submits, allowing tests to assert on the field mapping.
type recordingHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error

	logger *slog.Logger
}

func (h *recordingHost) DB() db.Driver                { return nil }
func (h *recordingHost) Bus() plugins.Bus             { return nil }
func (h *recordingHost) Logger() *slog.Logger         { return h.logger }
func (h *recordingHost) Tracer() trace.Tracer         { return otel.Tracer("prometheus-test") }
func (h *recordingHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *recordingHost) Config() *config.Config       { return config.Default() }
func (h *recordingHost) Plugin(string) plugins.Plugin { return nil }

// ProcessRecord lets the host satisfy the local recordProcessor interface.
func (h *recordingHost) ProcessRecord(_ context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err != nil {
		return rec, plugins.ActionAbort, h.err
	}
	h.records = append(h.records, rec)
	return rec, plugins.ActionContinue, nil
}

func (h *recordingHost) Records() []snoozetypes.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]snoozetypes.Record, len(h.records))
	copy(out, h.records)
	return out
}

// bareHost satisfies plugins.Host but NOT recordProcessor, so the plugin
// must degrade to a no-op when wired with it.
type bareHost struct {
	logger *slog.Logger
}

func (h *bareHost) DB() db.Driver                { return nil }
func (h *bareHost) Bus() plugins.Bus             { return nil }
func (h *bareHost) Logger() *slog.Logger         { return h.logger }
func (h *bareHost) Tracer() trace.Tracer         { return otel.Tracer("prometheus-test") }
func (h *bareHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *bareHost) Config() *config.Config       { return config.Default() }
func (h *bareHost) Plugin(string) plugins.Plugin { return nil }

func newPlugin(t *testing.T, host plugins.Host) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "prometheus"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postJSON posts an arbitrary JSON-marshalable value to /prometheus and runs
// the handler, returning the recorder.
func postJSON(t *testing.T, p *Plugin, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/prometheus", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "prometheus"),
		"prometheus plugin should be registered after init()")
}

func TestPluginIdentity(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "prometheus"}}
	require.Equal(t, "prometheus", p.Name())
	require.Equal(t, "/prometheus", p.WebhookPath())
	require.NotNil(t, p.Metadata())
	require.NoError(t, p.Reload(context.Background()))
}

func TestHandleWebhook_FiringAlert(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	w := postJSON(t, p, map[string]any{
		"version": "4",
		"status":  "firing",
		"alerts": []map[string]any{
			{
				"status": "firing",
				"labels": map[string]any{
					"alertname": "HighCPU",
					"severity":  "warning",
					"instance":  "node-1.local:9100",
					"service":   "node-exporter",
				},
				"annotations": map[string]any{
					"summary":     "CPU is high",
					"description": "CPU > 90% for 5m",
				},
				"startsAt":     ts,
				"generatorURL": "http://prom/graph?g0=foo",
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 1)
	got := recs[0]
	require.Equal(t, "prometheus", got.Source)
	// Python prometheus plugin does NOT strip the port off labels.instance.
	require.Equal(t, "node-1.local:9100", got.Host, "port must NOT be stripped (Python parity)")
	require.Equal(t, "warning", got.Severity)
	require.Equal(t, "CPU is high", got.Message)
	// process falls back to labels.service when labels.process is absent.
	require.Equal(t, "node-exporter", got.Process)
	require.Equal(t, ts, got.Timestamp)
	require.Empty(t, got.State, "firing alert must have empty state")
	require.NotNil(t, got.Raw)
	require.Contains(t, got.Raw, "labels")
	require.Contains(t, got.Raw, "annotations")
	require.Equal(t, "http://prom/graph?g0=foo", got.Raw["generatorURL"])
}

func TestHandleWebhook_ResolvedAlertSetsCloseStateAndOkSeverity(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	w := postJSON(t, p, map[string]any{
		"version": "4",
		"status":  "resolved",
		"alerts": []map[string]any{
			{
				"status": "resolved",
				"labels": map[string]any{
					"alertname": "HighCPU",
					"severity":  "warning",
					"instance":  "node-1",
				},
				"annotations": map[string]any{"summary": "OK now"},
				"startsAt":    time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
				"endsAt":      time.Date(2024, 6, 1, 12, 30, 0, 0, time.UTC),
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 1)
	require.Equal(t, "close", recs[0].State)
	require.Equal(t, "ok", recs[0].Severity, "resolved alerts force severity=ok")
	require.Equal(t, "node-1", recs[0].Host)
}

func TestHandleWebhook_DefaultsMatchPython(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	// No annotations beyond message, no severity label, no instance label,
	// no startsAt — covers every default branch.
	w := postJSON(t, p, map[string]any{
		"version": "4",
		"status":  "firing",
		"alerts": []map[string]any{
			{
				"status": "firing",
				"labels": map[string]any{
					"alertname": "MysteryAlert",
					"host":      "myhost.example",
					"process":   "snmpd",
				},
				"annotations": map[string]any{
					"externalURL": "http://prom/foo",
				},
				// No startsAt — handler must substitute time.Now().
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 1)
	got := recs[0]
	require.Equal(t, "myhost.example", got.Host, "labels.host takes priority over labels.instance")
	require.Equal(t, "critical", got.Severity, "firing default severity is critical (Python parity)")
	require.Equal(t, "http://prom/foo", got.Message, "falls back through annotations to externalURL")
	require.Equal(t, "snmpd", got.Process, "labels.process wins over labels.service")
	require.False(t, got.Timestamp.IsZero(), "missing startsAt is substituted with time.Now()")
}

func TestHandleWebhook_UnknownStatusYieldsUnknownSeverity(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	w := postJSON(t, p, map[string]any{
		"version": "4",
		"alerts": []map[string]any{
			{
				"status": "weird",
				"labels": map[string]any{
					"alertname": "X",
					"severity":  "warning", // ignored when status != firing
					"instance":  "h",
				},
				"startsAt": time.Now(),
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 1)
	require.Equal(t, "unknown", recs[0].Severity,
		"status outside firing/resolved must yield severity=unknown")
	require.Empty(t, recs[0].State)
}

func TestHandleWebhook_CommonLabelsAndAnnotationsMerge(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	w := postJSON(t, p, map[string]any{
		"version":     "4",
		"status":      "firing",
		"externalURL": "http://prom.example",
		"commonLabels": map[string]any{
			"environment": "prod",
			"severity":    "critical", // overridden per alert below
		},
		"commonAnnotations": map[string]any{
			"runbook": "https://runbooks.example/foo",
		},
		"alerts": []map[string]any{
			{
				"status": "firing",
				"labels": map[string]any{
					"alertname": "DiskFull",
					"severity":  "warning",
					"instance":  "host-a",
				},
				"annotations": map[string]any{"summary": "host-a disk almost full"},
				"startsAt":    time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			},
			{
				"status": "firing",
				"labels": map[string]any{
					"alertname": "DiskFull",
					"instance":  "host-b",
				},
				"startsAt": time.Date(2024, 6, 1, 0, 1, 0, 0, time.UTC),
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 2)

	require.Equal(t, "host-a", recs[0].Host)
	require.Equal(t, "warning", recs[0].Severity, "per-alert severity overrides commonLabels")
	require.Equal(t, "host-a disk almost full", recs[0].Message)

	require.Equal(t, "host-b", recs[1].Host)
	require.Equal(t, "critical", recs[1].Severity, "falls back to commonLabels.severity")

	// Raw should preserve the envelope externalURL.
	require.Equal(t, "http://prom.example", recs[0].Raw["externalURL"])

	// Verify the response body counts.
	var resp struct {
		Status   string `json:"status"`
		Received int    `json:"received"`
		Accepted int    `json:"accepted"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "ok", resp.Status)
	require.Equal(t, 2, resp.Received)
	require.Equal(t, 2, resp.Accepted)
}

func TestHandleWebhook_MalformedJSONReturns400(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	req := httptest.NewRequest(http.MethodPost, "/prometheus", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.Records())
}

func TestHandleWebhook_WrongMethodReturns405(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	req := httptest.NewRequest(http.MethodGet, "/prometheus", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, "POST", w.Header().Get("Allow"))
}

func TestHandleWebhook_PipelineErrorDoesNotBreakRequest(t *testing.T) {
	host := &recordingHost{logger: slog.Default(), err: errors.New("pipeline down")}
	p := newPlugin(t, host)

	w := postJSON(t, p, map[string]any{
		"version": "4",
		"status":  "firing",
		"alerts": []map[string]any{
			{
				"status":   "firing",
				"labels":   map[string]any{"alertname": "X", "instance": "h"},
				"startsAt": time.Now(),
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Received int `json:"received"`
		Accepted int `json:"accepted"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, 1, resp.Received)
	require.Equal(t, 0, resp.Accepted, "pipeline error means zero accepted")
}

func TestHandleWebhook_HostWithoutProcessorIsNoOp(t *testing.T) {
	// bareHost intentionally does not implement ProcessRecord, so the
	// runtime assertion fails and the handler must degrade to a no-op.
	host := &bareHost{logger: slog.Default()}
	p := newPlugin(t, host)

	w := postJSON(t, p, map[string]any{
		"version": "4",
		"status":  "firing",
		"alerts": []map[string]any{
			{
				"status":   "firing",
				"labels":   map[string]any{"alertname": "X", "instance": "h"},
				"startsAt": time.Now(),
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	// And the warning fires only once across multiple calls.
	require.True(t, p.warnedNoProcessor.Load())
	w2 := postJSON(t, p, map[string]any{"alerts": []map[string]any{}})
	require.Equal(t, http.StatusOK, w2.Code)
}
