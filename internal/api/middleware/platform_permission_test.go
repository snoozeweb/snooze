package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// platformProbe wires RequirePlatformPerm in front of a handler that records
// whether it ran, and returns the recorder for the given claims.
func platformProbe(t *testing.T, claims *snoozetypes.Claims, perms ...string) (*httptest.ResponseRecorder, *bool) {
	t.Helper()
	ran := false
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ran = true
		w.WriteHeader(http.StatusOK)
	})
	h := middleware.RequirePlatformPerm(perms...)(final)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil)
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), *claims))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, &ran
}

// TestRequirePlatformPerm_RejectsRwAllFromTenantAdmin reproduces C4: a tenant
// admin seeded with the rw_all wildcard must NOT be able to reach a
// platform-gated route. rw_all must not be honored, and the non-default tenant
// origin must be rejected.
func TestRequirePlatformPerm_RejectsRwAllFromTenantAdmin(t *testing.T) {
	claims := &snoozetypes.Claims{
		Subject:     "acme-admin",
		TenantID:    "acme",
		Permissions: []string{auth.AllPermission},
	}
	rec, ran := platformProbe(t, claims, auth.PermReadTenant, auth.PermWriteTenant)
	require.Equal(t, http.StatusForbidden, rec.Code, "rw_all from a tenant admin must be 403")
	require.False(t, *ran, "handler must not run for a tenant admin")
}

// TestRequirePlatformPerm_RejectsTenantOriginEvenWithLiteralPerm guards the
// origin check: even if a tenant user somehow carries the literal rw_tenant
// permission, a non-default tenant origin must be rejected.
func TestRequirePlatformPerm_RejectsTenantOriginEvenWithLiteralPerm(t *testing.T) {
	claims := &snoozetypes.Claims{
		Subject:     "acme-admin",
		TenantID:    "acme",
		Permissions: []string{auth.PermWriteTenant},
	}
	rec, ran := platformProbe(t, claims, auth.PermWriteTenant)
	require.Equal(t, http.StatusForbidden, rec.Code, "non-default tenant origin must be 403")
	require.False(t, *ran)
}

// TestRequirePlatformPerm_AllowsDefaultTenantPlatformAdmin is path (b): a
// default-tenant admin with the literal platform permission is allowed.
func TestRequirePlatformPerm_AllowsDefaultTenantPlatformAdmin(t *testing.T) {
	claims := &snoozetypes.Claims{
		Subject:     "root",
		TenantID:    snoozetypes.DefaultTenant,
		Permissions: []string{auth.PermWriteTenant},
	}
	rec, ran := platformProbe(t, claims, auth.PermWriteTenant)
	require.Equal(t, http.StatusOK, rec.Code, "default-tenant platform admin must pass")
	require.True(t, *ran, "handler must run for a default-tenant platform admin")
}

// TestRequirePlatformPerm_RejectsDefaultTenantMissingPerm: default origin but
// lacking the platform permission is 403 (no rw_all rescue).
func TestRequirePlatformPerm_RejectsDefaultTenantMissingPerm(t *testing.T) {
	claims := &snoozetypes.Claims{
		Subject:     "root",
		TenantID:    snoozetypes.DefaultTenant,
		Permissions: []string{auth.AllPermission, "rw_alert"},
	}
	rec, ran := platformProbe(t, claims, auth.PermWriteTenant)
	require.Equal(t, http.StatusForbidden, rec.Code, "missing literal platform perm must be 403 even with rw_all")
	require.False(t, *ran)
}

// TestRequirePlatformPerm_MissingClaimsUnauthorized: no claims → 401.
func TestRequirePlatformPerm_MissingClaimsUnauthorized(t *testing.T) {
	rec, ran := platformProbe(t, nil, auth.PermWriteTenant)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, *ran)
}
