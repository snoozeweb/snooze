package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWithDefaults_fillsDefaults(t *testing.T) {
	cfg, err := Config{Server: "https://snooze.example.com", Token: "tok"}.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "local", cfg.Method)
	require.Equal(t, 30*time.Second, cfg.RequestTimeout)
}

func TestWithDefaults_trimsTrailingSlash(t *testing.T) {
	cfg, err := Config{Server: "https://snooze.example.com/", Token: "tok"}.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example.com", cfg.Server)
}

func TestWithDefaults_requiresServer(t *testing.T) {
	_, err := Config{Token: "tok"}.WithDefaults()
	require.ErrorIs(t, err, ErrMissingConfig)
	require.Contains(t, err.Error(), "server")
}

func TestWithDefaults_requiresCredentials(t *testing.T) {
	_, err := Config{Server: "https://snooze.example.com"}.WithDefaults()
	require.ErrorIs(t, err, ErrMissingConfig)
	require.Contains(t, err.Error(), "token or username")
}

func TestWithDefaults_anonymousNeedsNoCreds(t *testing.T) {
	cfg, err := Config{Server: "https://snooze.example.com", Method: "anonymous"}.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "anonymous", cfg.Method)
}

func TestApplyEnv_overridesFile(t *testing.T) {
	env := map[string]string{
		"SNOOZE_SERVER":          "https://env.example.com",
		"SNOOZE_TOKEN":           "env-tok",
		"SNOOZE_INSECURE":        "true",
		"SNOOZE_REQUEST_TIMEOUT": "5s",
		"SNOOZE_DEBUG":           "true",
	}
	c := Config{Server: "https://file.example.com", Token: "file-tok"}
	c.applyEnv(func(k string) string { return env[k] })
	require.Equal(t, "https://env.example.com", c.Server)
	require.Equal(t, "env-tok", c.Token)
	require.True(t, c.Insecure)
	require.Equal(t, 5*time.Second, c.RequestTimeout)
	require.True(t, c.Debug)
}

func TestLoadConfig_file(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
server: https://snooze.example.com
token: ATATT
request_timeout: 15s
`), 0o600))
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example.com", cfg.Server)
	require.Equal(t, "ATATT", cfg.Token)
	require.Equal(t, 15*time.Second, cfg.RequestTimeout)
}

func TestLoadConfig_missingFileTolerated_whenEnvProvides(t *testing.T) {
	// A missing file is fine when env supplies the required fields. We can't
	// easily inject env into LoadConfig (it reads os.Getenv), so set + unset.
	t.Setenv("SNOOZE_SERVER", "https://snooze.example.com")
	t.Setenv("SNOOZE_TOKEN", "tok")
	cfg, err := LoadConfig("/no/such/mcp.yaml")
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example.com", cfg.Server)
}

func TestLoadConfig_missingFileAndNoEnv_errors(t *testing.T) {
	// Ensure no stray env from the host leaks in.
	t.Setenv("SNOOZE_SERVER", "")
	t.Setenv("SNOOZE_TOKEN", "")
	t.Setenv("SNOOZE_USERNAME", "")
	_, err := LoadConfig("/no/such/mcp.yaml")
	require.ErrorIs(t, err, ErrMissingConfig)
}
