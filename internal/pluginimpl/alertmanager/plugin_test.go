package alertmanager

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

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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
func (h *recordingHost) Tracer() trace.Tracer         { return otel.Tracer("alertmanager-test") }
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
func (h *bareHost) Tracer() trace.Tracer         { return otel.Tracer("alertmanager-test") }
func (h *bareHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *bareHost) Config() *config.Config       { return config.Default() }
func (h *bareHost) Plugin(string) plugins.Plugin { return nil }

func newPlugin(t *testing.T, host plugins.Host) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "alertmanager"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postJSON builds an HTTP POST to /alertmanager with body and runs the
// handler, returning the recorder.
func postJSON(t *testing.T, p *Plugin, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/alertmanager", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "alertmanager"),
		"alertmanager plugin should be registered after init()")
}

func TestPluginIdentity(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "alertmanager"}}
	require.Equal(t, "alertmanager", p.Name())
	require.Equal(t, "/alertmanager", p.WebhookPath())
	require.NotNil(t, p.Metadata())
	require.NoError(t, p.Reload(context.Background()))
}

func TestHandleWebhook_FiringAlert(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		Alerts: []am4Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "HighCPU",
					"severity":  "warning",
					"instance":  "node-1.local:9100",
					"job":       "node-exporter",
				},
				Annotations: map[string]string{
					"summary":     "CPU is high",
					"description": "CPU > 90% for 5m",
				},
				StartsAt:     ts,
				GeneratorURL: "http://prom/graph?g0=foo",
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 1)
	got := recs[0]
	require.Equal(t, "AlertManager", got.Source, "Snooze 1.x rules filter on source = AlertManager")
	require.Equal(t, "node-1.local", got.Host, "port should be stripped")
	require.Equal(t, "warning", got.Severity)
	require.Equal(t, "CPU is high", got.Message)
	// Process: no `process`/`service`/`alertgroup` label set → falls back
	// to `job` per the Python order.
	require.Equal(t, "node-exporter", got.Process)
	require.Equal(t, ts, got.Timestamp)
	require.Empty(t, got.State, "firing alert must have empty state")
	// labels/annotations/generatorURL live at the top level (in Extra)
	// like the Python plugin, not nested under raw.
	require.NotNil(t, got.Extra)
	require.Contains(t, got.Extra, "labels")
	require.Contains(t, got.Extra, "annotations")
	require.Equal(t, "http://prom/graph?g0=foo", got.Extra["generatorURL"])
	require.Nil(t, got.Raw, "raw stays unused — labels/annotations are top-level fields")
}

func TestHandleWebhook_ResolvedAlertSetsCloseState(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "resolved",
		Alerts: []am4Alert{
			{
				Status: "resolved",
				Labels: map[string]string{
					"alertname": "HighCPU",
					"severity":  "warning",
					"instance":  "node-1",
				},
				Annotations: map[string]string{"summary": "OK now"},
				StartsAt:    time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
				EndsAt:      time.Date(2024, 6, 1, 12, 30, 0, 0, time.UTC),
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 1)
	require.Equal(t, "close", recs[0].State)
	require.Equal(t, "ok", recs[0].Severity, "resolved alerts force severity=ok")
	require.Equal(t, "node-1", recs[0].Host, "no port to strip")
}

func TestHandleWebhook_MissingFieldsFallBackToDefaults(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	// No annotations, no severity label, no instance label.
	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		Alerts: []am4Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "MysteryAlert",
				},
				// No StartsAt — handler must substitute time.Now().
			},
		},
	})
	require.NoError(t, err)

	before := time.Now().Add(-time.Second)
	w := postJSON(t, p, body)
	after := time.Now().Add(time.Second)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 1)
	got := recs[0]
	require.Equal(t, "-", got.Host, "missing host/instance label falls back to dash")
	require.Equal(t, "critical", got.Severity,
		"unlabelled firing alerts default to critical (Python behaviour)")
	require.Equal(t, "", got.Message,
		"empty annotations + no message fallback to alertname (matches Python's ''-default)")
	require.Equal(t, "-", got.Process,
		"no process/service/alertgroup/job → '-' fallback")
	require.Nil(t, got.Tags)
	require.True(t, got.Timestamp.After(before) && got.Timestamp.Before(after),
		"missing StartsAt is substituted with time.Now()")
}

func TestHandleWebhook_BatchAndTagsAndCommonLabels(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		CommonLabels: map[string]string{
			"environment": "prod",
			"severity":    "critical", // overridden per alert below
		},
		CommonAnnotations: map[string]string{
			"runbook": "https://runbooks.example/foo",
		},
		Alerts: []am4Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "DiskFull",
					"severity":  "warning",
					"instance":  "host-a:9100",
					"tags":      "disk,storage prod",
				},
				Annotations: map[string]string{"summary": "host-a disk almost full"},
				StartsAt:    time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			},
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "DiskFull",
					"instance":  "host-b:9100",
				},
				StartsAt: time.Date(2024, 6, 1, 0, 1, 0, 0, time.UTC),
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 2)

	require.Equal(t, "host-a", recs[0].Host)
	require.Equal(t, "warning", recs[0].Severity, "per-alert severity overrides commonLabels")
	require.ElementsMatch(t, []string{"disk", "storage", "prod"}, recs[0].Tags)

	require.Equal(t, "host-b", recs[1].Host)
	require.Equal(t, "critical", recs[1].Severity, "falls back to commonLabels.severity")
	// No alert-level annotations; runbook from commonAnnotations isn't
	// in the message-fallback list, so the message stays empty (matches
	// Python's `or ''` final default).
	require.Equal(t, "", recs[1].Message)

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

func TestHandleWebhook_SanitizesDottedKeys(t *testing.T) {
	// AlertManager annotation names are free-form and frequently contain
	// dots in the wild (kubernetes.io/..., runbook.example.com/...).
	// MongoDB-safe storage requires we replace them with underscores
	// before the record reaches the DB layer, matching what
	// snooze.utils.functions.sanitize did in the Python era.
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		Alerts: []am4Alert{
			{
				Status: "firing",
				Labels: map[string]string{"alertname": "X", "instance": "h"},
				Annotations: map[string]string{
					"kubernetes.io/cluster": "prod-eu",
					"runbook.example.com":   "https://wiki/x",
				},
				StartsAt: time.Now(),
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.Records()
	require.Len(t, recs, 1)
	annotations, ok := recs[0].Extra["annotations"].(map[string]any)
	require.True(t, ok, "annotations should be a sanitized map under Extra")
	require.Equal(t, "prod-eu", annotations["kubernetes_io/cluster"])
	require.Equal(t, "https://wiki/x", annotations["runbook_example_com"])
	require.NotContains(t, annotations, "kubernetes.io/cluster",
		"dotted keys must be rewritten before they hit the DB")
}

func TestHandleWebhook_MessagePriorityMatchesPython(t *testing.T) {
	// Python priority: annotations.message → summary → description → externalURL → "".
	// When all four are set, `message` wins.
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		Alerts: []am4Alert{
			{
				Status: "firing",
				Labels: map[string]string{"alertname": "X", "instance": "h"},
				Annotations: map[string]string{
					"message":     "MSG-WINS",
					"summary":     "SUMMARY",
					"description": "DESC",
					"externalURL": "https://x",
				},
				StartsAt: time.Now(),
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "MSG-WINS", host.Records()[0].Message)
}

func TestHandleWebhook_HostPriorityMatchesPython(t *testing.T) {
	// Python priority: labels.host → labels.instance → labels.exported_instance.
	// host wins over instance when both are present.
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		Alerts: []am4Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "X",
					"host":      "explicit-host",
					"instance":  "node-1:9100",
				},
				StartsAt: time.Now(),
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "explicit-host", host.Records()[0].Host,
		"the `host` label outranks `instance` per the Python plugin")
}

func TestHandleWebhook_ProcessPriorityMatchesPython(t *testing.T) {
	// Python priority: process → service → alertgroup → job. The `process`
	// label outranks `alertname` (which Python never consulted).
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		Alerts: []am4Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "HighCPU",
					"process":   "my-proc",
					"service":   "my-svc",
					"job":       "my-job",
					"instance":  "h",
				},
				StartsAt: time.Now(),
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "my-proc", host.Records()[0].Process,
		"the `process` label outranks service/job and alertname is never consulted")
}

func TestHandleWebhook_MalformedJSONReturns400(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	w := postJSON(t, p, []byte("{not json"))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.Records())
}

func TestHandleWebhook_WrongMethodReturns405(t *testing.T) {
	host := &recordingHost{logger: slog.Default()}
	p := newPlugin(t, host)

	req := httptest.NewRequest(http.MethodGet, "/alertmanager", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, "POST", w.Header().Get("Allow"))
}

func TestHandleWebhook_PipelineErrorDoesNotBreakRequest(t *testing.T) {
	host := &recordingHost{logger: slog.Default(), err: errors.New("pipeline down")}
	p := newPlugin(t, host)

	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		Alerts: []am4Alert{
			{
				Status:   "firing",
				Labels:   map[string]string{"alertname": "X", "instance": "h"},
				StartsAt: time.Now(),
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
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

	body, err := json.Marshal(am4Webhook{
		Version: "4",
		Status:  "firing",
		Alerts: []am4Alert{
			{
				Status:   "firing",
				Labels:   map[string]string{"alertname": "X", "instance": "h"},
				StartsAt: time.Now(),
			},
		},
	})
	require.NoError(t, err)

	w := postJSON(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	// And the warning fires only once across multiple calls.
	require.True(t, p.warnedNoProcessor.Load())
	_ = postJSON(t, p, body)
}
