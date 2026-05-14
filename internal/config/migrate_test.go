package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrateFromPython_CopiesRecognisedFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(src, "core.yaml"),
		[]byte("port: 5300\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(src, "ldap_auth.yaml"),
		[]byte("enabled: false\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(src, "ignored.txt"),
		[]byte("nope"), 0o600))

	require.NoError(t, MigrateFromPython(src, dst))

	corePath := filepath.Join(dst, "core.yaml")
	require.FileExists(t, corePath)
	require.NoFileExists(t, filepath.Join(dst, "ignored.txt"))

	ldapPath := filepath.Join(dst, "ldap.yaml")
	require.FileExists(t, ldapPath) // renamed from ldap_auth.yaml

	cfg, err := Load(dst)
	require.NoError(t, err)
	require.Equal(t, 5300, cfg.Core.Port)
}

func TestMigrateFromPython_EmptyArgs(t *testing.T) {
	require.Error(t, MigrateFromPython("", t.TempDir()))
	require.Error(t, MigrateFromPython(t.TempDir(), ""))
}

func TestMigrateFromPython_MissingSource(t *testing.T) {
	require.Error(t, MigrateFromPython("/nonexistent/snooze-src", t.TempDir()))
}
