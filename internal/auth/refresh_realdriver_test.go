package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// newRealRefreshStore builds a RefreshTokenStore backed by a real on-disk
// SQLite driver (NOT the scope-ignoring fakeDB). The real driver enforces the
// tenant_id fence at the db layer, so it reproduces the production fail-closed
// behaviour that the fakeDB masks.
func newRealRefreshStore(t *testing.T) *auth.RefreshTokenStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "refresh.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return auth.NewRefreshTokenStore(drv, time.Hour)
}

// TestRefreshStore_RealDriver_RefreshSurvivesNakedContext is the H1 regression
// guard.
//
// In production:
//   - Login mints the refresh token under a tenant context (signSession).
//   - /login/refresh and /login/logout sit on the auth-skip path and therefore
//     run with a NAKED context (no tenant, no platform scope).
//
// refresh_token is a TENANT-SCOPED collection, so a naked context makes the
// driver fail-closed (ErrNoTenant). Before the fix the store swallowed that
// into ErrRefreshNotFound, so VerifyAndRotate against a real driver returned an
// error (→ 401 in the handler) even for a perfectly valid token.
//
// This test issues under a tenant ctx, then rotates under a NAKED ctx — exactly
// the production flow. It MUST fail on the pre-fix code (naked ctx → no row
// found → error) and pass once the store performs its lookups under platform
// scope.
func TestRefreshStore_RealDriver_RefreshSurvivesNakedContext(t *testing.T) {
	t.Parallel()
	s := newRealRefreshStore(t)

	claims := snoozetypes.Claims{
		Subject:  "alice",
		Method:   "local",
		TenantID: "acme",
		Roles:    []string{"admin"},
	}

	// Login path: Issue under the tenant context.
	issueCtx := snoozetypes.WithTenant(context.Background(), "acme")
	raw, _, err := s.Issue(issueCtx, claims)
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	// Refresh path: NAKED context (auth-skip path).
	got, newRaw, _, err := s.VerifyAndRotate(context.Background(), raw)
	require.NoError(t, err, "refresh must succeed under a naked context, not 401")
	require.NotEmpty(t, newRaw)
	require.NotEqual(t, raw, newRaw, "rotation must mint a fresh token")

	// The rotated claims must preserve the original tenant_id — a refresh must
	// never silently move a session into a different tenant.
	require.Equal(t, "acme", got.TenantID, "rotated claims must keep the original tenant_id")

	// The new token must itself be rotatable (also under a naked ctx).
	got2, _, _, err := s.VerifyAndRotate(context.Background(), newRaw)
	require.NoError(t, err, "the rotated token must itself be valid")
	require.Equal(t, "acme", got2.TenantID)

	// The OLD token must now be revoked (rotation invalidates it).
	_, _, _, err = s.VerifyAndRotate(context.Background(), raw)
	require.Error(t, err, "replaying the rotated-away token must fail")
	require.True(t, errors.Is(err, auth.ErrRefreshRevoked),
		"expected ErrRefreshRevoked, got %v", err)
}

// TestRefreshStore_RealDriver_LogoutActuallyRevokes is the second half of H1:
// logout must really revoke against a real driver under a naked context.
//
// Before the fix, Revoke under a naked ctx hit ErrNoTenant on the lookup, which
// the store swallowed into ErrRefreshNotFound and then treated as a no-op — so
// logout returned nil while leaving the token fully usable. This test proves
// the token is actually dead after logout.
func TestRefreshStore_RealDriver_LogoutActuallyRevokes(t *testing.T) {
	t.Parallel()
	s := newRealRefreshStore(t)

	claims := snoozetypes.Claims{Subject: "bob", Method: "local", TenantID: "acme"}
	issueCtx := snoozetypes.WithTenant(context.Background(), "acme")
	raw, _, err := s.Issue(issueCtx, claims)
	require.NoError(t, err)

	// Logout path: NAKED context.
	require.NoError(t, s.Revoke(context.Background(), raw),
		"logout must not surface an error")

	// The token must now be unusable. Pre-fix the silent no-op left it valid,
	// so this VerifyAndRotate would (wrongly) succeed.
	_, _, _, err = s.VerifyAndRotate(context.Background(), raw)
	require.Error(t, err, "logout must actually revoke the token")
	require.True(t, errors.Is(err, auth.ErrRefreshRevoked),
		"verify after logout must report ErrRefreshRevoked, got %v", err)
}
