package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func testClaims() snoozetypes.Claims {
	return snoozetypes.Claims{
		Subject:     "alice",
		Method:      "local",
		Roles:       []string{"admin"},
		Permissions: []string{"rw_all"},
		Groups:      []string{"ops"},
	}
}

func TestRefreshStore_IssueAndVerifyRotate(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := NewRefreshTokenStore(newFakeDB(), time.Hour)
	s.now = func() time.Time { return now }

	raw, exp, err := s.Issue(ctx, testClaims())
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.Equal(t, now.Add(time.Hour).Unix(), exp.Unix())

	got, newRaw, newExp, err := s.VerifyAndRotate(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, "alice", got.Subject)
	require.Equal(t, "local", got.Method)
	require.Equal(t, []string{"admin"}, got.Roles)
	require.Equal(t, []string{"rw_all"}, got.Permissions)
	require.Equal(t, []string{"ops"}, got.Groups)
	require.NotEmpty(t, newRaw)
	require.NotEqual(t, raw, newRaw, "rotation must mint a different token")
	require.Equal(t, now.Add(time.Hour).Unix(), newExp.Unix())
}

func TestRefreshStore_RotationRevokesOldToken(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := NewRefreshTokenStore(newFakeDB(), time.Hour)
	s.now = func() time.Time { return now }

	raw, _, err := s.Issue(ctx, testClaims())
	require.NoError(t, err)

	_, _, _, err = s.VerifyAndRotate(ctx, raw)
	require.NoError(t, err)

	// Replaying the same token must fail — it has been rotated.
	_, _, _, err = s.VerifyAndRotate(ctx, raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRefreshRevoked), "expected ErrRefreshRevoked, got %v", err)
}

func TestRefreshStore_ExpiredTokenRejected(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := NewRefreshTokenStore(newFakeDB(), time.Hour)
	s.now = func() time.Time { return now }

	raw, _, err := s.Issue(ctx, testClaims())
	require.NoError(t, err)

	// Advance the clock past the lease.
	s.now = func() time.Time { return now.Add(2 * time.Hour) }

	_, _, _, err = s.VerifyAndRotate(ctx, raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRefreshExpired), "expected ErrRefreshExpired, got %v", err)
}

func TestRefreshStore_UnknownTokenRejected(t *testing.T) {
	ctx := context.Background()
	s := NewRefreshTokenStore(newFakeDB(), time.Hour)

	// Arbitrary base64-url-shaped string that was never issued.
	_, _, _, err := s.VerifyAndRotate(ctx, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRefreshNotFound), "expected ErrRefreshNotFound, got %v", err)
}

func TestRefreshStore_RevokeMakesReuseFail(t *testing.T) {
	ctx := context.Background()
	s := NewRefreshTokenStore(newFakeDB(), time.Hour)

	raw, _, err := s.Issue(ctx, testClaims())
	require.NoError(t, err)

	require.NoError(t, s.Revoke(ctx, raw))

	_, _, _, err = s.VerifyAndRotate(ctx, raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRefreshRevoked), "expected ErrRefreshRevoked, got %v", err)
}

func TestRefreshStore_RevokeUnknownIsNoop(t *testing.T) {
	ctx := context.Background()
	s := NewRefreshTokenStore(newFakeDB(), time.Hour)

	// Best-effort: an unknown token must not surface an error to callers,
	// otherwise logout against a stale token would 500.
	require.NoError(t, s.Revoke(ctx, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
}

func TestRefreshStore_IssueProducesUniqueTokens(t *testing.T) {
	ctx := context.Background()
	s := NewRefreshTokenStore(newFakeDB(), time.Hour)

	a, _, err := s.Issue(ctx, testClaims())
	require.NoError(t, err)
	b, _, err := s.Issue(ctx, testClaims())
	require.NoError(t, err)
	require.NotEqual(t, a, b, "Issue must mint a fresh token every call")
}
