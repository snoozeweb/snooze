package relp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLoadConfigDefaults round-trips a minimal YAML through LoadConfig and
// asserts the documented defaults are applied.
func TestLoadConfigDefaults(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "relp.yaml")
	yaml := "server: https://snooze.example/\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example/", cfg.Server)
	require.Equal(t, "0.0.0.0:2514", cfg.Listen)
	require.Equal(t, "auto", cfg.Parser)
	require.Equal(t, 10*time.Second, cfg.RequestTimeout)
	require.Equal(t, 1<<20, cfg.MaxFrameBytes)
}

// TestConfigValidates surfaces the validation failure modes.
func TestConfigValidates(t *testing.T) {
	t.Parallel()
	_, err := Config{}.WithDefaults()
	require.ErrorContains(t, err, "server")

	_, err = Config{Server: "x", Parser: "bogus"}.WithDefaults()
	require.ErrorContains(t, err, "parser")

	cfg, err := Config{Server: "x", Parser: "rfc5424"}.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "rfc5424", cfg.Parser)
}
