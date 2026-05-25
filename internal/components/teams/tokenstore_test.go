package teams

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTokenStore_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tok.json")
	s := newTokenStore(path)

	// First Load should signal missing.
	_, err := s.Load()
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoToken), "expected ErrNoToken, got %v", err)

	saved := cachedToken{
		AccessToken:  "AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(time.Hour).Round(time.Second).UTC(),
		Scope:        "ChannelMessage.Send offline_access",
		ObtainedAt:   time.Now().Round(time.Second).UTC(),
		TenantID:     "tenant",
		ClientID:     "client",
	}
	require.NoError(t, s.Save(saved))

	// Re-open via a new store and confirm we can load what we wrote.
	s2 := newTokenStore(path)
	got, err := s2.Load()
	require.NoError(t, err)
	require.Equal(t, saved.AccessToken, got.AccessToken)
	require.Equal(t, saved.RefreshToken, got.RefreshToken)
	require.True(t, saved.ExpiresAt.Equal(got.ExpiresAt))
}

func TestTokenStore_RejectsSaveWithoutRefreshToken(t *testing.T) {
	s := newTokenStore(filepath.Join(t.TempDir(), "tok.json"))
	err := s.Save(cachedToken{AccessToken: "AT"})
	require.Error(t, err)
}

func TestTokenStore_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tok.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
	s := newTokenStore(path)
	_, err := s.Load()
	require.Error(t, err)
}

func TestTokenStore_AtomicWrite(t *testing.T) {
	// The store is supposed to write to "<path>.tmp" then rename. After a
	// successful Save no leftover .tmp should exist.
	path := filepath.Join(t.TempDir(), "tok.json")
	s := newTokenStore(path)
	require.NoError(t, s.Save(cachedToken{
		AccessToken:  "AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(time.Hour),
	}))
	_, err := os.Stat(path + ".tmp")
	require.True(t, os.IsNotExist(err), "expected no leftover tmp file, got %v", err)
}
