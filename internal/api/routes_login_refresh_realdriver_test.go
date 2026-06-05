package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// loginRealRefreshRouter wires the /api/v1/login/* routes against a REAL
// sqlite-backed RefreshTokenStore (not the scope-ignoring fakeRefresh stub).
// This exercises the full HTTP handler path the way production does: the
// refresh/logout endpoints are on the auth-skip path and therefore run with a
// naked (tenant-less) request context against the tenant-scoped refresh_token
// collection.
func loginRealRefreshRouter(t *testing.T) (chi.Router, *Router, *auth.RefreshTokenStore) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "refresh.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	store := auth.NewRefreshTokenStore(drv, time.Hour)
	rt := &Router{
		Auth:    testTokenEngine(t),
		Refresh: store,
	}
	r := chi.NewRouter()
	rt.mountLogin(r)
	return r, rt, store
}

// issueRefreshForTenant mints a refresh token the way signSession does on
// login: under the tenant context, so the stored row carries the tenant_id.
func issueRefreshForTenant(t *testing.T, store *auth.RefreshTokenStore, tenant string) string {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), tenant)
	raw, _, err := store.Issue(ctx, snoozetypes.Claims{
		Subject:  "alice",
		Method:   "local",
		TenantID: tenant,
		Roles:    []string{"admin"},
	})
	require.NoError(t, err)
	return raw
}

// TestRefreshHandler_RealDriver_RotatesUnderNakedContext is the H1 handler-level
// regression guard. With the real driver, POST /login/refresh must NOT 401: the
// handler must scope the lookup to platform so the tenant-scoped refresh_token
// row is reachable from the naked auth-skip context.
func TestRefreshHandler_RealDriver_RotatesUnderNakedContext(t *testing.T) {
	t.Parallel()
	r, rt, store := loginRealRefreshRouter(t)
	raw := issueRefreshForTenant(t, store, "acme")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/refresh",
		bytes.NewBufferString(`{"refresh_token":"`+raw+`"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "refresh must succeed, not 401")
	var resp loginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token, "must return a new access token")
	require.NotEmpty(t, resp.RefreshToken, "must return a rotated refresh token")
	require.NotEqual(t, raw, resp.RefreshToken, "rotation must mint a fresh token")

	// The new access token must keep the original tenant_id claim.
	claims, err := rt.Auth.Verify(resp.Token)
	require.NoError(t, err)
	require.Equal(t, "acme", claims.TenantID, "rotated access token must keep tenant_id")

	// The OLD refresh token must now be dead.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/login/refresh",
		bytes.NewBufferString(`{"refresh_token":"`+raw+`"}`))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusUnauthorized, rec2.Code,
		"replaying a rotated-away token must 401")
}

// TestLogoutHandler_RealDriver_ActuallyRevokes is the second half of H1 at the
// handler level: POST /login/logout must really kill the token even from the
// naked auth-skip context. Before the fix the revoke silently no-opped and the
// token stayed usable.
func TestLogoutHandler_RealDriver_ActuallyRevokes(t *testing.T) {
	t.Parallel()
	r, _, store := loginRealRefreshRouter(t)
	raw := issueRefreshForTenant(t, store, "acme")

	// Logout.
	logoutRec := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/login/logout",
		bytes.NewBufferString(`{"refresh_token":"`+raw+`"}`))
	logoutReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(logoutRec, logoutReq)
	require.Equal(t, http.StatusNoContent, logoutRec.Code)

	// The token must be unusable now — a subsequent refresh must 401.
	refreshRec := httptest.NewRecorder()
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/login/refresh",
		bytes.NewBufferString(`{"refresh_token":"`+raw+`"}`))
	refreshReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(refreshRec, refreshReq)
	require.Equal(t, http.StatusUnauthorized, refreshRec.Code,
		"token must be revoked after logout")
}
