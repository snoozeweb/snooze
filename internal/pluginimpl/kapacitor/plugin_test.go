package kapacitor

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// fakeHost is a minimal plugins.Host that ALSO satisfies recordProcessor.
// It records every ProcessRecord call so tests can assert the records the
// plugin produced from a webhook POST.
type fakeHost struct {
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
	tracer trace.Tracer

	mu   sync.Mutex
	recs []snoozetypes.Record
	// err is the error returned by ProcessRecord, useful for the
	// "pipeline error" path.
	err error
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		logger: slog.Default(),
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(nil),
		tracer: otel.Tracer("kapacitor-test"),
	}
}

// plugins.Host surface.
func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return h.logger }
func (h *fakeHost) Tracer() trace.Tracer         { return h.tracer }
func (h *fakeHost) Metrics() *telemetry.Registry { return h.metr }
func (h *fakeHost) Config() *config.Config       { return h.cfg }
func (h *fakeHost) Plugin(string) plugins.Plugin { return nil }

// recordProcessor surface — what the plugin asserts at runtime.
func (h *fakeHost) ProcessRecord(_ context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err != nil {
		return rec, plugins.ActionContinue, h.err
	}
	h.recs = append(h.recs, rec)
	return rec, plugins.ActionContinue, nil
}

func (h *fakeHost) records() []snoozetypes.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]snoozetypes.Record, len(h.recs))
	copy(out, h.recs)
	return out
}

func newPlugin(t *testing.T, host plugins.Host) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "kapacitor"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postJSON drives one webhook call and returns the response.
func postJSON(t *testing.T, p *Plugin, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/kapacitor", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.HandleWebhook(rec, req)
	return rec
}

const criticalSingleSeries = `{
  "id": "cpu_usage_high",
  "message": "CPU usage above 95%",
  "details": "host=node-1 cpu=user",
  "time": "2024-05-13T11:00:00Z",
  "duration": 3000000000,
  "level": "CRITICAL",
  "data": {
    "series": [
      {
        "name": "cpu",
        "tags": {"host": "node-1", "cpu": "cpu-total", "datacenter": "dc1"},
        "columns": ["time", "usage_user"],
        "values": [["2024-05-13T11:00:00Z", 97.3]]
      }
    ]
  }
}`

func TestKapacitor(t *testing.T) {
	t.Run("plugin_identity", func(t *testing.T) {
		// Sanity check on Name/Metadata/WebhookPath. Catches accidental
		// regressions in registry wiring.
		host := newFakeHost()
		p := newPlugin(t, host)
		require.Equal(t, "kapacitor", p.Name())
		require.Equal(t, "/kapacitor", p.WebhookPath())
		require.Equal(t, "kapacitor", p.Metadata().Name)
	})

	t.Run("critical_single_series_emits_one_record", func(t *testing.T) {
		// Happy path: 1 series in → 1 record out, CRITICAL → critical,
		// host pulled from tags, process from envelope id, raw preserved.
		host := newFakeHost()
		p := newPlugin(t, host)

		resp := postJSON(t, p, criticalSingleSeries)
		require.Equal(t, http.StatusOK, resp.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
		require.Equal(t, "ok", body["status"])
		require.EqualValues(t, 1, body["received"])
		require.EqualValues(t, 1, body["accepted"])

		recs := host.records()
		require.Len(t, recs, 1)
		got := recs[0]
		require.Equal(t, "node-1", got.Host)
		require.Equal(t, "kapacitor", got.Source)
		require.Equal(t, "critical", got.Severity)
		require.Equal(t, "CPU usage above 95%", got.Message)
		require.Equal(t, "cpu_usage_high", got.Process)
		require.Equal(t, time.Date(2024, 5, 13, 11, 0, 0, 0, time.UTC), got.Timestamp)
		require.Empty(t, got.State)
		// host/process/severity are popped from the tags; cpu+datacenter remain.
		require.Equal(t, []string{"cpu", "datacenter"}, got.Tags)
		// Raw round-trips the full payload.
		require.Equal(t, "cpu_usage_high", got.Raw["id"])
		require.Equal(t, "CRITICAL", got.Raw["level"])
	})

	t.Run("level_to_severity_mapping", func(t *testing.T) {
		// Exercise each level → severity row in the mapping table. OK also
		// flips State to "close" — that's the close-the-alert convention.
		cases := []struct {
			level    string
			severity string
			state    string
		}{
			{"CRITICAL", "critical", ""},
			{"WARNING", "warning", ""},
			{"INFO", "info", ""},
			{"OK", "info", "close"},
		}
		for _, tc := range cases {
			t.Run(tc.level, func(t *testing.T) {
				host := newFakeHost()
				p := newPlugin(t, host)

				body := `{
  "id": "x",
  "message": "m",
  "time": "2024-05-13T11:00:00Z",
  "level": "` + tc.level + `",
  "data": {"series": [{"name": "n", "tags": {"host": "h"}, "columns": ["time"], "values": [["2024-05-13T11:00:00Z"]]}]}
}`
				resp := postJSON(t, p, body)
				require.Equal(t, http.StatusOK, resp.Code)

				recs := host.records()
				require.Len(t, recs, 1)
				require.Equal(t, tc.severity, recs[0].Severity)
				require.Equal(t, tc.state, recs[0].State)
			})
		}
	})

	t.Run("multiple_series_fan_out", func(t *testing.T) {
		// Two series in one envelope produce two records sharing the
		// envelope-level message/id but with distinct hosts and tag sets.
		host := newFakeHost()
		p := newPlugin(t, host)

		body := `{
  "id": "disk_full",
  "message": "Disk usage above threshold",
  "time": "2024-05-13T12:00:00Z",
  "level": "WARNING",
  "data": {
    "series": [
      {"name": "disk", "tags": {"host": "node-1", "mount": "/"}, "columns": ["time", "used"], "values": [["2024-05-13T12:00:00Z", 91]]},
      {"name": "disk", "tags": {"instance": "node-2", "mount": "/var"}, "columns": ["time", "used"], "values": [["2024-05-13T12:00:00Z", 93]]}
    ]
  }
}`
		resp := postJSON(t, p, body)
		require.Equal(t, http.StatusOK, resp.Code)

		var body2 map[string]any
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body2))
		require.EqualValues(t, 2, body2["received"])
		require.EqualValues(t, 2, body2["accepted"])

		recs := host.records()
		require.Len(t, recs, 2)
		require.Equal(t, "node-1", recs[0].Host)
		// Second series uses the `instance` tag as the host fallback.
		require.Equal(t, "node-2", recs[1].Host)
		require.Equal(t, "disk_full", recs[0].Process)
		require.Equal(t, "disk_full", recs[1].Process)
		require.Equal(t, "warning", recs[0].Severity)
		require.Equal(t, "warning", recs[1].Severity)
	})

	t.Run("series_severity_tag_overrides_level", func(t *testing.T) {
		// Python pops a per-series `severity` tag when present, leaving the
		// envelope-level `level` as a fallback. Verify the same precedence.
		host := newFakeHost()
		p := newPlugin(t, host)

		body := `{
  "id": "load_avg",
  "message": "Load too high",
  "time": "2024-05-13T13:00:00Z",
  "level": "CRITICAL",
  "data": {
    "series": [
      {"name": "load", "tags": {"host": "node-3", "severity": "warning"}, "columns": ["time"], "values": [["2024-05-13T13:00:00Z"]]}
    ]
  }
}`
		resp := postJSON(t, p, body)
		require.Equal(t, http.StatusOK, resp.Code)

		recs := host.records()
		require.Len(t, recs, 1)
		// Tag wins verbatim, not the CRITICAL→critical mapping.
		require.Equal(t, "warning", recs[0].Severity)
		// And the severity tag is consumed, not surfaced as a Tag key.
		require.NotContains(t, recs[0].Tags, "severity")
		require.NotContains(t, recs[0].Tags, "host")
	})

	t.Run("process_tag_overrides_id", func(t *testing.T) {
		// Python: tags.pop('process', media.get('id', '')).
		host := newFakeHost()
		p := newPlugin(t, host)

		body := `{
  "id": "envelope-id",
  "message": "m",
  "time": "2024-05-13T14:00:00Z",
  "level": "INFO",
  "data": {
    "series": [{"name": "n", "tags": {"host": "h", "process": "custom-proc"}, "columns": [], "values": []}]
  }
}`
		resp := postJSON(t, p, body)
		require.Equal(t, http.StatusOK, resp.Code)

		recs := host.records()
		require.Len(t, recs, 1)
		require.Equal(t, "custom-proc", recs[0].Process)
	})

	t.Run("malformed_payload_400", func(t *testing.T) {
		// Garbage in → 400, with no pipeline submission.
		host := newFakeHost()
		p := newPlugin(t, host)

		resp := postJSON(t, p, `{not-json`)
		require.Equal(t, http.StatusBadRequest, resp.Code)
		require.Empty(t, host.records())
	})

	t.Run("wrong_method_405", func(t *testing.T) {
		// GET /webhook/kapacitor should bounce with Allow: POST.
		host := newFakeHost()
		p := newPlugin(t, host)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/kapacitor", nil)
		rec := httptest.NewRecorder()
		p.HandleWebhook(rec, req)
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		require.Equal(t, http.MethodPost, rec.Header().Get("Allow"))
	})

	t.Run("no_processor_logs_once_but_still_200", func(t *testing.T) {
		// When the host is not a recordProcessor, the plugin must not
		// panic and must still respond 200 — but `accepted` reflects the
		// no-op (every record is "accepted" since the pipeline is skipped,
		// matching the alertmanager plugin's behaviour).
		host := &minimalHost{logger: slog.Default(), cfg: config.Default(), metr: telemetry.NewRegistry(nil)}
		p := newPlugin(t, host)

		resp := postJSON(t, p, criticalSingleSeries)
		require.Equal(t, http.StatusOK, resp.Code)
		// First call sets the warned flag.
		require.True(t, p.warnedNoProcessor.Load())
	})
}

// minimalHost satisfies plugins.Host but NOT recordProcessor. It exists only
// to exercise the no-processor degraded path.
type minimalHost struct {
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
}

func (h *minimalHost) DB() db.Driver                { return nil }
func (h *minimalHost) Bus() plugins.Bus             { return nil }
func (h *minimalHost) Logger() *slog.Logger         { return h.logger }
func (h *minimalHost) Tracer() trace.Tracer         { return otel.Tracer("kapacitor-test-minimal") }
func (h *minimalHost) Metrics() *telemetry.Registry { return h.metr }
func (h *minimalHost) Config() *config.Config       { return h.cfg }
func (h *minimalHost) Plugin(string) plugins.Plugin { return nil }
