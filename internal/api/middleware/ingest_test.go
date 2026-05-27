package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func ingestOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestIngestToken_Unconfigured_PassesThrough(t *testing.T) {
	mw := IngestToken("")(ingestOK())
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/webhook/x", nil))
	require.Equal(t, http.StatusOK, rec.Code, "no configured token means no gate (1.5.0 parity)")
}

func TestIngestToken_BearerMatch_PassesThrough(t *testing.T) {
	mw := IngestToken("s3cret")(ingestOK())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/x", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestIngestToken_QueryMatch_PassesThrough(t *testing.T) {
	mw := IngestToken("s3cret")(ingestOK())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/x?token=s3cret", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestIngestToken_Missing_Returns401(t *testing.T) {
	mw := IngestToken("s3cret")(ingestOK())
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/webhook/x", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestIngestToken_Wrong_Returns401(t *testing.T) {
	mw := IngestToken("s3cret")(ingestOK())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/x", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
