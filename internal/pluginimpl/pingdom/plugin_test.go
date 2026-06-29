package pingdom

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

// fakeHost is a minimal plugins.Host that additionally satisfies the local
// recordProcessor interface used by the pingdom plugin. ProcessRecord captures
// every record passed in.
type fakeHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("pingdom-test") }
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
	p := &Plugin{meta: plugins.Metadata{Name: "pingdom"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postWebhook is a small helper that posts body to the plugin's HandleWebhook
// and returns the recorded response.
func postWebhook(t *testing.T, p *Plugin, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/pingdom", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "pingdom"))
}

func TestPluginContract(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "pingdom"}}
	require.Equal(t, "pingdom", p.Name())
	require.Equal(t, "/pingdom", p.WebhookPath())
	require.Equal(t, "pingdom", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))

	// Ensure the plugin satisfies the WebhookReceiver interface at compile time.
	var _ plugins.WebhookReceiver = p
}

func TestDownHighEmitsCritical(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"check_id": 12345,
		"check_name": "My homepage",
		"check_type": "HTTP",
		"check_params": {"hostname": "example.com", "url": "/", "port": 443},
		"tags": [{"name": "production"}, {"name": "web"}],
		"previous_state": "UP",
		"current_state": "DOWN",
		"importance_level": "HIGH",
		"state_changed_timestamp": 1700000000,
		"description": "Host is Down",
		"long_description": "Real browser test failed (step 3/5)"
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

	require.Equal(t, "pingdom", rec.Source)
	require.Equal(t, "My homepage", rec.Host)
	require.Equal(t, "HTTP", rec.Process)
	require.Equal(t, "critical", rec.Severity)
	require.Empty(t, rec.State)
	require.Equal(t, "Host is Down", rec.Message)
	require.Equal(t, []string{"production", "web"}, rec.Tags)
	require.NotNil(t, rec.Raw)
	require.EqualValues(t, "12345", numStr(rec.Raw["check_id"]))
	require.Equal(t, "Real browser test failed (step 3/5)", rec.Raw["long_description"])
}

func TestDownLowEmitsWarning(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"check_name": "API endpoint",
		"check_type": "TCP",
		"current_state": "DOWN",
		"importance_level": "LOW",
		"description": "Host is Down"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "warning", recs[0].Severity)
	require.Empty(t, recs[0].State)
}

func TestUpEmitsCloseRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"check_name": "My homepage",
		"check_type": "HTTP",
		"current_state": "UP",
		"importance_level": "HIGH",
		"description": "Host is Up"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "close", recs[0].State)
	require.Equal(t, "ok", recs[0].Severity)
}

func TestPausedEmitsUnknownSeverity(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"check_name": "My homepage",
		"check_type": "HTTP",
		"current_state": "PAUSED",
		"importance_level": "HIGH",
		"description": "Check paused for maintenance"
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "unknown", recs[0].Severity)
	require.Empty(t, recs[0].State)
}

func TestTagsFlattened(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"check_name": "My homepage",
		"check_type": "HTTP",
		"current_state": "DOWN",
		"importance_level": "HIGH",
		"tags": [{"name": "prod"}, {"name": "web"}]
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, []string{"prod", "web"}, recs[0].Tags)
}

func TestTagsFlattenedNilWhenMissing(t *testing.T) {
	// No tags key → nil (not an empty slice).
	require.Nil(t, flattenTags(map[string]any{}))

	// tags present but not a list → nil.
	require.Nil(t, flattenTags(map[string]any{"tags": "not-a-list"}))
}

func TestTagsFlattenedSkipsNonMapElements(t *testing.T) {
	// A non-map element and a map missing the "name" key are skipped; valid
	// entries survive. No panic.
	raw := map[string]any{
		"tags": []any{
			map[string]any{"name": "keep"},
			"plain-string",          // non-map element → skipped
			map[string]any{},        // missing "name" → skipped
			map[string]any{"id": 1}, // wrong key → skipped
			map[string]any{"name": "also-keep"},
		},
	}
	require.Equal(t, []string{"keep", "also-keep"}, flattenTags(raw))
}

func TestTimestampFromPayload(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body := []byte(`{
		"check_name": "My homepage",
		"check_type": "HTTP",
		"current_state": "DOWN",
		"importance_level": "HIGH",
		"state_changed_timestamp": 1700000000
	}`)

	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, time.Unix(1700000000, 0).UTC(), recs[0].Timestamp)
}

func TestMalformedJSONReturns400(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(`{not-json`))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.seen())
	require.True(t,
		strings.Contains(w.Body.String(), "invalid Pingdom payload"),
		"body=%q", w.Body.String(),
	)
}

func TestWrongMethodReturns405(t *testing.T) {
	p := newPlugin(t, &fakeHost{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/pingdom", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

func TestPipelineErrorIsNotFatal(t *testing.T) {
	host := &fakeHost{err: errors.New("pipeline boom")}
	p := newPlugin(t, host)

	body := []byte(`{
		"check_name": "My homepage",
		"check_type": "HTTP",
		"current_state": "DOWN",
		"importance_level": "HIGH",
		"description": "Host is Down"
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
	p := &Plugin{meta: plugins.Metadata{Name: "pingdom"}}
	// PostInit with a host that does NOT satisfy recordProcessor.
	require.NoError(t, p.PostInit(context.Background(), &nakedPluginHost{}))

	body := []byte(`{
		"check_name": "My homepage",
		"check_type": "HTTP",
		"current_state": "DOWN",
		"importance_level": "HIGH",
		"description": "Host is Down"
	}`)
	w := postWebhook(t, p, body)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// With no pipeline, record still counts as "accepted" (no-op success).
	require.EqualValues(t, 1, resp["accepted"])
}

// numStr renders a json.Number (or any) as its string form for assertions.
func numStr(v any) string {
	if n, ok := v.(json.Number); ok {
		return n.String()
	}
	return ""
}

// nakedPluginHost is a plugins.Host that does not satisfy recordProcessor.
type nakedPluginHost struct{}

func (nakedPluginHost) DB() db.Driver                { return nil }
func (nakedPluginHost) Bus() plugins.Bus             { return nil }
func (nakedPluginHost) Logger() *slog.Logger         { return slog.Default() }
func (nakedPluginHost) Tracer() trace.Tracer         { return otel.Tracer("pingdom-test") }
func (nakedPluginHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nakedPluginHost) Config() *config.Config       { return config.Default() }
func (nakedPluginHost) Plugin(string) plugins.Plugin { return nil }
