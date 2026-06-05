package snmptrap_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/components/snmptrap"
)

func TestLoadConfig_AppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snmptrap.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server: https://snooze.example/\n"), 0o600))

	cfg, err := snmptrap.LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example/", cfg.Server)
	require.Equal(t, "0.0.0.0:162", cfg.Listen)
	require.Equal(t, "public", cfg.Community)
	require.Equal(t, "local", cfg.Method)
	require.Equal(t, 30*time.Second, cfg.Timeout)
	require.Nil(t, cfg.V3)
}

func TestLoadConfig_V3Block(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snmptrap.yaml")
	body := `
server: https://snooze.example
listen: 127.0.0.1:1162
community: "*"
v3:
  user: ingest
  auth_proto: SHA
  auth_passphrase: secret-auth
  priv_proto: AES
  priv_passphrase: secret-priv
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cfg, err := snmptrap.LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:1162", cfg.Listen)
	// "*" must round-trip verbatim (operator opt-in for any-community).
	require.Equal(t, "*", cfg.Community)
	require.NotNil(t, cfg.V3)
	require.Equal(t, "ingest", cfg.V3.User)
	require.Equal(t, "SHA", cfg.V3.AuthProto)
	require.Equal(t, "AES", cfg.V3.PrivProto)
}

func TestLoadConfig_MissingServerRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snmptrap.yaml")
	require.NoError(t, os.WriteFile(path, []byte("listen: 0.0.0.0:162\n"), 0o600))

	_, err := snmptrap.LoadConfig(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "server")
}

func TestLoadConfig_V3RequiresUser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snmptrap.yaml")
	body := `
server: https://snooze.example
v3:
  auth_proto: SHA
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	_, err := snmptrap.LoadConfig(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "v3.user")
}

func TestLoadConfig_IngestTokenRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snmptrap.yaml")
	body := "server: https://snooze.example/\ningest_token: tok-abc\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cfg, err := snmptrap.LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "tok-abc", cfg.IngestToken)
}
