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
)

// fakeProcessor is the AlertProcessor used in tests. It captures every
// inbound record and returns a synthetic ack envelope.
type fakeProcessor struct {
	got        []map[string]any
	captureCtx func(context.Context)
}

func (f *fakeProcessor) ProcessRecord(ctx context.Context, rec map[string]any) (map[string]any, error) {
	if f.captureCtx != nil {
		f.captureCtx(ctx)
	}
	f.got = append(f.got, rec)
	out := map[string]any{"uid": "u-1"}
	for k, v := range rec {
		out[k] = v
	}
	return out, nil
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
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, fp.got, 2)
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
