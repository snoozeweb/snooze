package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseClientConfig_BasicShape(t *testing.T) {
	// Exact wire shape from the Snooze 1.x /etc/snooze/client.yaml on the
	// production host; the Go port must keep parsing it.
	data := []byte(`---
server: https://snooze.egerie.eu
credentials:
  username: snooze
  password: ziH6NcmbXwlJCePMq2YfDbkx
`)
	cfg := parseClientConfig(data)
	require.Equal(t, "https://snooze.egerie.eu", cfg.Server)
	require.Equal(t, "snooze", cfg.Credentials.Username)
	require.Equal(t, "ziH6NcmbXwlJCePMq2YfDbkx", cfg.Credentials.Password)
	require.Empty(t, cfg.Method)
	require.False(t, cfg.Insecure)
	require.Zero(t, cfg.Timeout)
}

func TestParseClientConfig_OptionalFields(t *testing.T) {
	data := []byte(`
server: https://x
credentials: {username: u, password: p}
method: ldap
insecure: true
timeout: 5s
`)
	cfg := parseClientConfig(data)
	require.Equal(t, "ldap", cfg.Method)
	require.True(t, cfg.Insecure)
	require.Equal(t, "5s", cfg.Timeout.String())
}

func TestParseClientConfig_AcceptsSnooze1xAliases(t *testing.T) {
	// The Snooze 1.x Python client wrote `auth_method` and used
	// `ca_bundle: false` to mean "skip TLS verify". The Go CLI must keep
	// reading those existing files transparently — the on-disk shape on
	// the fde workstation today is the canonical test case.
	data := []byte(`
server: https://snooze.egerie.eu
auth_method: local
credentials:
  username: snooze
  password: pw
app_name: snooze_client
token_to_disk: true
ca_bundle: false
`)
	cfg := parseClientConfig(data)
	require.Equal(t, "local", cfg.Method, "auth_method should alias method")
	require.True(t, cfg.Insecure, "ca_bundle: false should set insecure=true")
}

func TestParseClientConfig_CanonicalKeysWinOnCollision(t *testing.T) {
	// If both spellings are present the new canonical key wins — useful
	// when an operator hand-edits a 1.x file to migrate to the 2.0 shape
	// and forgets to delete the old key.
	data := []byte(`
method: ldap
auth_method: local
insecure: true
ca_bundle: /etc/ssl/certs/custom.pem
`)
	cfg := parseClientConfig(data)
	require.Equal(t, "ldap", cfg.Method)
	require.True(t, cfg.Insecure)
}

func TestParseClientConfig_CABundlePathIsIgnored(t *testing.T) {
	// ca_bundle as a string path is a 1.x custom-CA mechanism the Go CLI
	// doesn't reproduce yet. Make sure we don't accidentally flip
	// Insecure=true for it.
	data := []byte(`ca_bundle: /etc/ssl/certs/custom.pem`)
	cfg := parseClientConfig(data)
	require.False(t, cfg.Insecure)
}

func TestParseClientConfig_EmptyOrInvalidYieldsZero(t *testing.T) {
	require.Equal(t, ClientConfig{}, parseClientConfig(nil))
	// A malformed YAML must not panic — we swallow the error.
	require.NotPanics(t, func() { parseClientConfig([]byte(":\n:\n:")) })
}

func TestLoadClientConfig_ReadsSNOOZE_CONFIG(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "client.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`
server: https://from-env
credentials: {username: env-user, password: env-pw}
`), 0o600))

	t.Setenv("SNOOZE_CONFIG", p)
	cfg := LoadClientConfig()
	require.Equal(t, "https://from-env", cfg.Server)
	require.Equal(t, "env-user", cfg.Credentials.Username)
}

func TestNewRootCmd_FileConfigSeedsDefaults(t *testing.T) {
	// Smoke-test the precedence chain: when nothing else is set, flag
	// defaults come from rt.fileConfig.
	rt := &runtime{
		flags: &globalFlags{},
		fileConfig: ClientConfig{
			Server:      "https://from-file",
			Credentials: ClientConfigCredentials{Username: "u", Password: "p"},
			Method:      "ldap",
		},
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}
	root := NewRootCmd(rt)
	root.SetArgs([]string{"--help"})
	root.SetContext(withRuntime(context.Background(), rt))
	require.NoError(t, root.Execute())
	require.Equal(t, "https://from-file", rt.flags.Server)
	require.Equal(t, "u", rt.flags.User)
	require.Equal(t, "p", rt.flags.Password)
	require.Equal(t, "ldap", rt.flags.Method)
}

func TestNewRootCmd_FlagOverridesFileConfig(t *testing.T) {
	rt := &runtime{
		flags: &globalFlags{},
		fileConfig: ClientConfig{
			Server:      "https://from-file",
			Credentials: ClientConfigCredentials{Username: "u-file", Password: "p-file"},
		},
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}
	root := NewRootCmd(rt)
	root.SetArgs([]string{"--server", "https://from-flag", "--user", "u-flag", "--help"})
	root.SetContext(withRuntime(context.Background(), rt))
	require.NoError(t, root.Execute())
	require.Equal(t, "https://from-flag", rt.flags.Server)
	require.Equal(t, "u-flag", rt.flags.User)
	// Unset flag still picks up the file default.
	require.Equal(t, "p-file", rt.flags.Password)
}

func TestNewRootCmd_EnvOverridesFileConfig(t *testing.T) {
	t.Setenv("SNOOZE_SERVER", "https://from-env")
	rt := &runtime{
		flags: &globalFlags{},
		fileConfig: ClientConfig{
			Server: "https://from-file",
		},
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}
	root := NewRootCmd(rt)
	root.SetArgs([]string{"--help"})
	root.SetContext(withRuntime(context.Background(), rt))
	require.NoError(t, root.Execute())
	require.Equal(t, "https://from-env", rt.flags.Server)
}
