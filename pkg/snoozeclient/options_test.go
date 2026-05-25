package snoozeclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveCachePath_RespectsOverride asserts an explicit override is
// returned verbatim with no probing — that's the caller's responsibility.
func TestResolveCachePath_RespectsOverride(t *testing.T) {
	override := "/some/path/i/picked"
	got, err := resolveCachePath(override)
	require.NoError(t, err)
	require.Equal(t, override, got)
}

// TestResolveCachePath_FallsThroughWhenHomeIsUnwritable mimics the snoozeweb
// deployment shape: $HOME is set to a path that doesn't exist (the daemon
// user has no home directory). The resolver must skip both the
// UserCacheDir() and UserHomeDir() candidates and fall back to tmp without
// surfacing an error.
func TestResolveCachePath_FallsThroughWhenHomeIsUnwritable(t *testing.T) {
	// Point $HOME and $XDG_CACHE_HOME inside a directory we own, then chmod
	// it 0 so MkdirAll under it fails with EACCES. t.Setenv restores the
	// previous value on Cleanup.
	jail := t.TempDir()
	homeDir := filepath.Join(jail, "phantom-home")
	require.NoError(t, os.Mkdir(homeDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(homeDir, 0o700) })

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))

	got, err := resolveCachePath("")
	require.NoError(t, err, "tmp fallback must NOT surface an error to callers")
	require.True(t, strings.HasPrefix(got, os.TempDir()),
		"expected tmp fallback, got %q", got)
}

// TestResolveCachePath_PrefersUserCacheDirWhenWritable asserts the happy
// path: an operator with a normal home dir gets the canonical
// $XDG_CACHE_HOME/snooze/token location and the parent gets created.
func TestResolveCachePath_PrefersUserCacheDirWhenWritable(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	got, err := resolveCachePath("")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cache, "snooze", "token"), got)
	// The probe must have created the parent dir.
	info, err := os.Stat(filepath.Join(cache, "snooze"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

// TestCanWriteDir validates the probe helper end-to-end.
func TestCanWriteDir(t *testing.T) {
	t.Run("writable dir returns true", func(t *testing.T) {
		require.True(t, canWriteDir(t.TempDir()))
	})
	t.Run("empty string returns false", func(t *testing.T) {
		require.False(t, canWriteDir(""))
	})
	t.Run("unwritable parent returns false", func(t *testing.T) {
		jail := t.TempDir()
		locked := filepath.Join(jail, "locked")
		require.NoError(t, os.Mkdir(locked, 0o000))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
		// canWriteDir tries to mkdir <locked>/sub — that should fail.
		require.False(t, canWriteDir(filepath.Join(locked, "sub")))
	})
}
