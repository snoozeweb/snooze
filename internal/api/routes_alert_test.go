package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// fakeProcessor is the AlertProcessor used in tests. It captures every
// inbound record and returns a synthetic ack envelope.
type fakeProcessor struct {
	got        []map[string]any
	captureCtx func(context.Context)
	// override lets a test inject custom per-call responses.
	// If set, override is called instead of the default pass-through logic.
	override func(rec map[string]any) (map[string]any, plugins.Action, error)
}

func (f *fakeProcessor) ProcessRecord(ctx context.Context, rec map[string]any) (map[string]any, plugins.Action, error) {
	if f.captureCtx != nil {
		f.captureCtx(ctx)
	}
	f.got = append(f.got, rec)
	if f.override != nil {
		return f.override(rec)
	}
	out := map[string]any{"uid": "u-1"}
	for k, v := range rec {
		out[k] = v
	}
	return out, plugins.ActionContinue, nil
}

func TestAlertRoute_SingleObject(t *testing.T) {
	fp := &fakeProcessor{}
	r := chi.NewRouter()
	rt := &Router{Processor: fp}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`{"host":"a","severity":"err"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, fp.got, 1)
	require.Equal(t, "a", fp.got[0]["host"])

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "u-1", resp.Data[0]["uid"])
}

func TestAlertRoute_BatchArray(t *testing.T) {
	fp := &fakeProcessor{}
	r := chi.NewRouter()
	rt := &Router{Processor: fp}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`[{"host":"a"},{"host":"b"}]`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, fp.got, 2)
	// Both records in the batch receive the same resolved source_ip.
	require.Equal(t, "203.0.113.5", fp.got[0]["source_ip"])
	require.Equal(t, "203.0.113.5", fp.got[1]["source_ip"])
}

// TestAlertPost_StampsSourceIP verifies that handleAlertPost injects
// source_ip into every record that lacks one, using the resolved client IP.
func TestAlertPost_StampsSourceIP(t *testing.T) {
	fp := &fakeProcessor{}
	r := chi.NewRouter()
	rt := &Router{Processor: fp}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`{"host":"a"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, fp.got, 1)
	require.Equal(t, "203.0.113.5", fp.got[0]["source_ip"])
}

// TestAlertPost_DoesNotOverwriteSourceIP verifies that a caller-supplied
// non-empty source_ip survives the handler unchanged.
func TestAlertPost_DoesNotOverwriteSourceIP(t *testing.T) {
	fp := &fakeProcessor{}
	r := chi.NewRouter()
	rt := &Router{Processor: fp}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`{"host":"a","source_ip":"10.0.0.1"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, fp.got, 1)
	require.Equal(t, "10.0.0.1", fp.got[0]["source_ip"])
}

// TestAlertRoute_KillSwitch_Blocks — when IngestAllowed returns false the
// handler short-circuits with 503 before the processor is ever called.
func TestAlertRoute_KillSwitch_Blocks(t *testing.T) {
	fp := &fakeProcessor{}
	r := chi.NewRouter()
	rt := &Router{
		Processor:     fp,
		IngestAllowed: func(context.Context) bool { return false },
	}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`{"host":"a"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Empty(t, fp.got, "processor must not run while ingest is disabled")
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "unavailable", resp.Error.Code)
}

// TestAlertRoute_KillSwitch_Allows — IngestAllowed returns true → normal 200.
func TestAlertRoute_KillSwitch_Allows(t *testing.T) {
	fp := &fakeProcessor{}
	r := chi.NewRouter()
	rt := &Router{
		Processor:     fp,
		IngestAllowed: func(context.Context) bool { return true },
	}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`{"host":"a"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, fp.got, 1)
}

func TestAlertRoute_BadJSON(t *testing.T) {
	fp := &fakeProcessor{}
	r := chi.NewRouter()
	rt := &Router{Processor: fp}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`not json`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAlertRoute_NoProcessorMeansNotMounted(t *testing.T) {
	r := chi.NewRouter()
	rt := &Router{}
	rt.mountAlerts(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/alerts", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestAlertRoute_PolicyReject_Returns422 — processor returns ActionAbort with
// reject_policy set → response code 422, body contains policy_rejected code.
func TestAlertRoute_PolicyReject_Returns422(t *testing.T) {
	fp := &fakeProcessor{
		override: func(_ map[string]any) (map[string]any, plugins.Action, error) {
			return map[string]any{"reject_policy": "blacklist"}, plugins.ActionAbort, nil
		},
	}
	r := chi.NewRouter()
	rt := &Router{Processor: fp}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`{"host":"a","source":"evil"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "policy_rejected", resp.Error.Code)
}

// TestAlertRoute_PolicyReject_MixedBatch — batch of two records; first passes
// (ActionContinue), second is rejected → 200, data has one entry, errors has one entry.
func TestAlertRoute_PolicyReject_MixedBatch(t *testing.T) {
	callCount := 0
	fp := &fakeProcessor{
		override: func(rec map[string]any) (map[string]any, plugins.Action, error) {
			callCount++
			if callCount == 1 {
				// First record: pass through.
				out := map[string]any{"uid": "u-1"}
				for k, v := range rec {
					out[k] = v
				}
				return out, plugins.ActionContinue, nil
			}
			// Second record: policy reject.
			return map[string]any{"reject_policy": "blacklist"}, plugins.ActionAbort, nil
		},
	}
	r := chi.NewRouter()
	rt := &Router{Processor: fp}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`[{"host":"a"},{"host":"b","source":"evil"}]`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data   []map[string]any `json:"data"`
		Errors []string         `json:"errors,omitempty"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Len(t, resp.Errors, 1)
	require.Contains(t, resp.Errors[0], "blacklist")
}

// TestAlertRoute_SnoozeSilentDiscard_Returns200 — processor returns ActionAbort
// WITHOUT reject_policy (snooze discard path) → 200, data is empty, no errors.
func TestAlertRoute_SnoozeSilentDiscard_Returns200(t *testing.T) {
	fp := &fakeProcessor{
		override: func(_ map[string]any) (map[string]any, plugins.Action, error) {
			// snooze discard: ActionAbort, no reject_policy field.
			return map[string]any{"snoozed": "Filter 1"}, plugins.ActionAbort, nil
		},
	}
	r := chi.NewRouter()
	rt := &Router{Processor: fp}
	rt.mountAlerts(r)

	body := bytes.NewBufferString(`{"host":"a"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data   []map[string]any `json:"data"`
		Errors []string         `json:"errors,omitempty"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Empty(t, resp.Data)
	require.Empty(t, resp.Errors)
}
