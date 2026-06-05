// internal/api/middleware/ingest_test.go
package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

// --- new tests for per-tenant IngestTenant middleware ---

// tenantCapture is a handler that records the tenant slug from context.
type tenantCapture struct {
	tenant string
	ok     bool
}

func (tc *tenantCapture) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	tc.tenant, tc.ok = auth.TenantFrom(r.Context())
}

func TestIngestTenant_KnownToken_SetsTenant(t *testing.T) {
	resolver := NewTenantResolver()
	resolver.Replace(map[string]string{"tok-acme": "acme"})

	tc := &tenantCapture{}
	mw := IngestTenant(resolver, nil)(tc)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer tok-acme")
	mw.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, tc.ok)
	require.Equal(t, "acme", tc.tenant)
}

func TestIngestTenant_UnknownToken_FallsBackToDefault(t *testing.T) {
	resolver := NewTenantResolver()
	resolver.Replace(map[string]string{"tok-acme": "acme"})

	tc := &tenantCapture{}
	mw := IngestTenant(resolver, nil)(tc)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer unknown-tok")
	mw.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, tc.ok)
	require.Equal(t, snoozetypes.DefaultTenant, tc.tenant)
}

func TestIngestTenant_NoToken_FallsBackToDefault(t *testing.T) {
	resolver := NewTenantResolver()
	resolver.Replace(map[string]string{"tok-acme": "acme"})

	tc := &tenantCapture{}
	mw := IngestTenant(resolver, nil)(tc)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	mw.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, tc.ok)
	require.Equal(t, snoozetypes.DefaultTenant, tc.tenant)
}

// statusChecker is a TenantStatusChecker that returns the pre-configured status.
type stubStatusChecker func(ctx context.Context, tenantID string) (string, error)

func (f stubStatusChecker) TenantStatus(ctx context.Context, tenantID string) (string, error) {
	return f(ctx, tenantID)
}

func TestIngestTenant_SuspendedTenant_Returns503(t *testing.T) {
	resolver := NewTenantResolver()
	resolver.Replace(map[string]string{"tok-acme": "acme"})

	checker := stubStatusChecker(func(_ context.Context, _ string) (string, error) {
		return "suspended", nil
	})

	tc := &tenantCapture{}
	mw := IngestTenant(resolver, checker)(tc)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer tok-acme")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.False(t, tc.ok, "handler must not run for suspended tenant")
}

func TestIngestTenant_ActiveTenant_Passes(t *testing.T) {
	resolver := NewTenantResolver()
	resolver.Replace(map[string]string{"tok-acme": "acme"})

	checker := stubStatusChecker(func(_ context.Context, _ string) (string, error) {
		return "active", nil
	})

	tc := &tenantCapture{}
	mw := IngestTenant(resolver, checker)(tc)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer tok-acme")
	mw.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, tc.ok)
	require.Equal(t, "acme", tc.tenant)
}

func TestIngestTenant_NilChecker_SkipsStatusCheck(t *testing.T) {
	resolver := NewTenantResolver()
	resolver.Replace(map[string]string{"tok-acme": "acme"})

	tc := &tenantCapture{}
	mw := IngestTenant(resolver, nil)(tc)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer tok-acme")
	mw.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, tc.ok)
	require.Equal(t, "acme", tc.tenant)
}
