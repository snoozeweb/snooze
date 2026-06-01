package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeNotifier is a plugins.Notifier whose Send is driven by sendFn so tests
// can assert on the record/payload it received and simulate upstream failure.
type fakeNotifier struct {
	name    string
	sendFn  func(rec snoozetypes.Record, payload plugins.NotificationPayload) error
	lastRec snoozetypes.Record
	lastMsg plugins.NotificationPayload
}

func (f *fakeNotifier) Name() string                                 { return f.name }
func (f *fakeNotifier) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: f.name} }
func (f *fakeNotifier) PostInit(context.Context, plugins.Host) error { return nil }
func (f *fakeNotifier) Reload(context.Context) error                 { return nil }
func (f *fakeNotifier) Send(_ context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	f.lastRec = rec
	f.lastMsg = payload
	if f.sendFn != nil {
		return f.sendFn(rec, payload)
	}
	return nil
}

// (For the "not a notifier" case we reuse the package's existing stubPlugin
// from routes_schema_test.go — it satisfies plugins.Plugin but not Notifier.)

func postActionTest(t *testing.T, rt *Router, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	rt.mountActionTest(r)
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/test", bytes.NewReader(raw))
	r.ServeHTTP(rec, req)
	return rec
}

func TestActionTest_Success(t *testing.T) {
	fake := &fakeNotifier{name: "teams"}
	rt := &Router{Plugins: map[string]plugins.Plugin{"teams": fake}}

	rec := postActionTest(t, rt, map[string]any{
		"selected":   "teams",
		"subcontent": map[string]any{"webhook_url": "https://example.com"},
	})

	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, true, got["ok"])

	require.Equal(t, "test-host.example.com", fake.lastRec.Host)
	require.Equal(t, "https://example.com", fake.lastMsg.Meta["webhook_url"])
	require.Equal(t, "__test__", fake.lastMsg.Meta["action_name"])
}

func TestActionTest_UnknownPlugin(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{}}
	rec := postActionTest(t, rt, map[string]any{"selected": "nope", "subcontent": map[string]any{}})
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestActionTest_NotANotifier(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{"record": &stubPlugin{name: "record"}}}
	rec := postActionTest(t, rt, map[string]any{"selected": "record", "subcontent": map[string]any{}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestActionTest_UpstreamFailureSurfacesDetail(t *testing.T) {
	fake := &fakeNotifier{
		name: "teams",
		sendFn: func(snoozetypes.Record, plugins.NotificationPayload) error {
			return errors.New("teams: HTTP 500: boom")
		},
	}
	rt := &Router{Plugins: map[string]plugins.Plugin{"teams": fake}}
	rec := postActionTest(t, rt, map[string]any{"selected": "teams", "subcontent": map[string]any{}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Contains(t, env.Error.Message, "boom")
}

func TestActionTest_MissingSelected(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{}}
	rec := postActionTest(t, rt, map[string]any{"subcontent": map[string]any{}})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
