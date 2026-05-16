package influxdb2

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// recordingHost is a minimal plugins.Host that also satisfies the unexported
// recordProcessor contract, so HandleWebhook drives the full submission path
// during tests.
type recordingHost struct {
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
	tracer trace.Tracer

	mu      sync.Mutex
	records []snoozetypes.Record
	err     error
}

func (h *recordingHost) DB() db.Driver                { return nil }
func (h *recordingHost) Bus() plugins.Bus             { return nil }
func (h *recordingHost) Logger() *slog.Logger         { return h.logger }
func (h *recordingHost) Tracer() trace.Tracer         { return h.tracer }
func (h *recordingHost) Metrics() *telemetry.Registry { return h.metr }
func (h *recordingHost) Config() *config.Config       { return h.cfg }
func (h *recordingHost) Plugin(string) plugins.Plugin { return nil }

func (h *recordingHost) ProcessRecord(_ context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	if h.err != nil {
		return rec, plugins.ActionContinue, h.err
	}
	return rec, plugins.ActionContinue, nil
}

func (h *recordingHost) Records() []snoozetypes.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]snoozetypes.Record, len(h.records))
	copy(out, h.records)
	return out
}

func newHost() *recordingHost {
	return &recordingHost{
		logger: slog.Default(),
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(nil),
		tracer: otel.Tracer("influxdb2-test"),
	}
}

func newPlugin(t *testing.T, h plugins.Host) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "influxdb2"}}
	require.NoError(t, p.PostInit(context.Background(), h))
	return p
}

// postJSON wraps an HTTP POST against the plugin's HandleWebhook using a
// chi-compatible httptest recorder.
func postJSON(t *testing.T, p *Plugin, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if s, ok := body.(string); ok {
		buf.WriteString(s)
	} else {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/influxdb2", &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.HandleWebhook(rec, req)
	return rec
}

func TestInfluxDB2(t *testing.T) {
	t.Run("plugin_identity", func(t *testing.T) {
		p := newPlugin(t, newHost())
		require.Equal(t, "influxdb2", p.Name())
		require.Equal(t, "/influxdb2", p.WebhookPath())
	})

	t.Run("level_to_severity_mapping", func(t *testing.T) {
		cases := []struct {
			level    string
			severity string
			state    string
		}{
			{"crit", "critical", ""},
			{"warn", "warning", ""},
			{"info", "info", ""},
			{"ok", "ok", "close"},
			{"normal", "ok", "close"},
			{"unknown", "unknown", ""},
		}
		for _, tc := range cases {
			t.Run(tc.level, func(t *testing.T) {
				host := newHost()
				p := newPlugin(t, host)

				payload := map[string]any{
					"_check_id":                   "001",
					"_check_name":                 "disk-full",
					"_level":                      tc.level,
					"_source_measurement":         "disk",
					"_status_timestamp":           1700000000,
					"_message":                    "boom",
					"_notification_endpoint_name": "ops",
				}
				rec := postJSON(t, p, payload)
				require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

				recs := host.Records()
				require.Len(t, recs, 1)
				got := recs[0]
				require.Equal(t, "influxdb2", got.Source)
				require.Equal(t, tc.severity, got.Severity)
				require.Equal(t, tc.state, got.State)
				require.Equal(t, "boom", got.Message)
				require.Equal(t, "disk", got.Process)
				require.False(t, got.Timestamp.IsZero())
				require.Equal(t, int64(1700000000), got.Timestamp.Unix())
				require.NotNil(t, got.Raw)
				require.Equal(t, tc.level, got.Raw["_level"])
			})
		}
	})

	t.Run("severity_override_wins_over_level", func(t *testing.T) {
		// Python: media.get('severity', level) — explicit `severity` always
		// wins over the mapped `_level`.
		host := newHost()
		p := newPlugin(t, host)

		payload := map[string]any{
			"_level":              "crit",
			"_message":            "explicit override",
			"_status_timestamp":   1700000000,
			"_source_measurement": "cpu",
			"severity":            "alert",
		}
		rec := postJSON(t, p, payload)
		require.Equal(t, http.StatusOK, rec.Code)

		recs := host.Records()
		require.Len(t, recs, 1)
		require.Equal(t, "alert", recs[0].Severity)
		require.Empty(t, recs[0].State, "non-ok severity should not close")
	})

	t.Run("process_override_wins_over_measurement", func(t *testing.T) {
		host := newHost()
		p := newPlugin(t, host)

		payload := map[string]any{
			"_level":              "warn",
			"_source_measurement": "cpu",
			"process":             "explicit-process",
			"_status_timestamp":   1700000000,
			"_message":            "x",
		}
		rec := postJSON(t, p, payload)
		require.Equal(t, http.StatusOK, rec.Code)

		recs := host.Records()
		require.Len(t, recs, 1)
		require.Equal(t, "explicit-process", recs[0].Process)
	})

	t.Run("malformed_body_returns_400", func(t *testing.T) {
		host := newHost()
		p := newPlugin(t, host)

		rec := postJSON(t, p, "this is not json")
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Empty(t, host.Records(), "no record submitted on malformed body")
	})

	t.Run("non_post_returns_405", func(t *testing.T) {
		host := newHost()
		p := newPlugin(t, host)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/influxdb2", nil)
		rec := httptest.NewRecorder()
		p.HandleWebhook(rec, req)
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		require.Equal(t, http.MethodPost, rec.Header().Get("Allow"))
		require.Empty(t, host.Records())
	})

	t.Run("missing_timestamp_falls_back_to_now", func(t *testing.T) {
		host := newHost()
		p := newPlugin(t, host)

		payload := map[string]any{
			"_level":   "info",
			"_message": "no ts",
		}
		rec := postJSON(t, p, payload)
		require.Equal(t, http.StatusOK, rec.Code)

		recs := host.Records()
		require.Len(t, recs, 1)
		require.False(t, recs[0].Timestamp.IsZero(), "timestamp should default to now")
	})

	t.Run("response_envelope_reports_acceptance", func(t *testing.T) {
		host := newHost()
		p := newPlugin(t, host)

		rec := postJSON(t, p, map[string]any{
			"_level":            "crit",
			"_message":          "x",
			"_status_timestamp": 1700000000,
		})
		require.Equal(t, http.StatusOK, rec.Code)

		var env map[string]any
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
		require.Equal(t, "ok", env["status"])
		require.Equal(t, float64(1), env["received"])
		require.Equal(t, float64(1), env["accepted"])
	})

	t.Run("no_processor_host_is_a_noop", func(t *testing.T) {
		// A bare plugins.Host (no recordProcessor) must not panic; the
		// handler still responds 200 with accepted=0 and emits a single
		// warning to the logger.
		var sink strings.Builder
		lg := slog.New(slog.NewTextHandler(&sink, &slog.HandlerOptions{Level: slog.LevelWarn}))

		bare := &bareHost{logger: lg, cfg: config.Default(), metr: telemetry.NewRegistry(nil)}
		p := newPlugin(t, bare)

		rec := postJSON(t, p, map[string]any{
			"_level":   "info",
			"_message": "x",
		})
		require.Equal(t, http.StatusOK, rec.Code)

		var env map[string]any
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
		require.Equal(t, float64(0), env["accepted"])
		require.Contains(t, sink.String(), "does not satisfy recordProcessor")
	})
}

// bareHost is a plugins.Host that deliberately does NOT satisfy
// recordProcessor — used to exercise the degraded path.
type bareHost struct {
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
}

func (h *bareHost) DB() db.Driver                { return nil }
func (h *bareHost) Bus() plugins.Bus             { return nil }
func (h *bareHost) Logger() *slog.Logger         { return h.logger }
func (h *bareHost) Tracer() trace.Tracer         { return otel.Tracer("test") }
func (h *bareHost) Metrics() *telemetry.Registry { return h.metr }
func (h *bareHost) Config() *config.Config       { return h.cfg }
func (h *bareHost) Plugin(string) plugins.Plugin { return nil }
