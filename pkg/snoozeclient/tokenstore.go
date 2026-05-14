package snoozeclient

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// readTokenFile returns the trimmed token stored at path. A missing file is
// reported as (empty string, nil) so callers can treat "no cache" as a soft
// state. Any other I/O error is returned.
func readTokenFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is user-configurable on purpose
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("snoozeclient: read token cache: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// writeTokenFile atomically writes token to path with 0600 permissions. The
// parent directory is created if missing (0700). The atomic write uses a
// temporary file in the same directory followed by os.Rename so the cache
// is never observed half-written.
func writeTokenFile(path, token string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("snoozeclient: mkdir token cache: %w", err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".snooze-token-*")
	if err != nil {
		return fmt.Errorf("snoozeclient: create temp token file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails.
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("snoozeclient: chmod token cache: %w", err)
	}
	if _, err := tmp.WriteString(token); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("snoozeclient: write token: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("snoozeclient: sync token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("snoozeclient: close token: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("snoozeclient: rename token: %w", err)
	}
	return nil
}

// deleteTokenFile removes the cache file, ignoring "missing" errors so callers
// can use it as an idempotent purge.
func deleteTokenFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("snoozeclient: remove token cache: %w", err)
	}
	return nil
}
