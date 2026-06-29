package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestIngestAllow_Middleware_Blocks — when the predicate returns false the
// middleware writes a 503 ingest_disabled envelope and never calls next.
func TestIngestAllow_Middleware_Blocks(t *testing.T) {
	called := false
	h := IngestAllow(func(context.Context) bool { return false })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/alertmanager", nil)
	h.ServeHTTP(rec, req)

	require.False(t, called, "next must not run while ingest is disabled")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "ingest_disabled", env.Error.Code)
}

// TestIngestAllow_Middleware_Passes — predicate returns true → next runs.
func TestIngestAllow_Middleware_Passes(t *testing.T) {
	called := false
	h := IngestAllow(func(context.Context) bool { return true })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/alertmanager", nil)
	h.ServeHTTP(rec, req)

	require.True(t, called, "next must run while ingest is allowed")
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestIngestAllow_Middleware_NilPasses — a nil predicate disables the switch.
func TestIngestAllow_Middleware_NilPasses(t *testing.T) {
	called := false
	h := IngestAllow(nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/alertmanager", nil)
	h.ServeHTTP(rec, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, rec.Code)
}
