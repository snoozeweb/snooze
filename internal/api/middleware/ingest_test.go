package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

func TestIngestToken_SuspendedTenantRejected(t *testing.T) {
	// When a per-tenant token resolves to a suspended tenant, the request
	// must be rejected with 403. This requires a TenantLookup to be wired.
	lookup := &fakeTenantLookup{
		tenantID: "acme",
		status:   "suspended",
	}
	h := IngestTokenWithLookup("", lookup)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", nil)
	req.Header.Set("X-Snooze-Token", "some-ingest-token")
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestIngestToken_ActiveTenantPasses(t *testing.T) {
	lookup := &fakeTenantLookup{
		tenantID: "acme",
		status:   "active",
	}
	h := IngestTokenWithLookup("", lookup)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := auth.TenantFrom(r.Context())
		require.True(t, ok)
		require.Equal(t, "acme", tenantID)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", nil)
	req.Header.Set("X-Snooze-Token", "acme-token")
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestIngestToken_NoTokenFallsBackToDefault(t *testing.T) {
	lookup := &fakeTenantLookup{
		tenantID: "",
		status:   "",
	}
	h := IngestTokenWithLookup("", lookup)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := auth.TenantFrom(r.Context())
		require.True(t, ok)
		require.Equal(t, snoozetypes.DefaultTenant, tenantID)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", nil)
	// No X-Snooze-Token header → falls back to default tenant.
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

// fakeTenantLookup is a stub TenantLookup for ingest middleware tests.
type fakeTenantLookup struct {
	tenantID string
	status   string
}

func (f *fakeTenantLookup) LookupByIngestToken(_ context.Context, _ string) (tenantID, status string, err error) {
	return f.tenantID, f.status, nil
}
