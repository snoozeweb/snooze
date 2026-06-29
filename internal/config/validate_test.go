package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

func TestValidate_DefaultsAreValid(t *testing.T) {
	require.NoError(t, Default().Validate())
}

func TestValidate_RejectsPortOutOfRange(t *testing.T) {
	c := Default()
	c.Core.Port = 0
	require.Error(t, c.Validate())
	c.Core.Port = 70000
	require.Error(t, c.Validate())
}

func TestValidate_RejectsBadAuthBackend(t *testing.T) {
	c := Default()
	c.General.DefaultAuthBackend = "bogus"
	require.Error(t, c.Validate())
}

func TestValidate_RejectsBadTokenAlgorithm(t *testing.T) {
	c := Default()
	c.Auth.TokenAlgorithm = "RS256"
	require.Error(t, c.Validate())
}

func TestValidate_RejectsUnknownDatabaseType(t *testing.T) {
	c := Default()
	c.Core.Database.Type = "bogus"
	require.Error(t, c.Validate())
}

func TestValidate_RejectsFileBackendWithoutPath(t *testing.T) {
	c := Default()
	c.Core.Database = schema.Database{Type: "file"}
	require.Error(t, c.Validate())
}

func TestValidate_RejectsPostgresBackendWithoutConnection(t *testing.T) {
	c := Default()
	c.Core.Database = schema.Database{Type: "postgres"}
	require.Error(t, c.Validate())
}

func TestValidate_AcceptsPostgresDSN(t *testing.T) {
	c := Default()
	c.Core.Database = schema.Database{Type: "postgres", DSN: "postgres://u:p@host/db"}
	require.NoError(t, c.Validate())
}

// validateWithAuthProxy builds a valid Default() config, swaps in the given
// AuthProxy section, and runs the full Config.Validate() — exercising
// validateAuthProxy through the public entry point.
func validateWithAuthProxy(t *testing.T, a schema.AuthProxy) error {
	t.Helper()
	c := Default()
	c.AuthProxy = a
	return c.Validate()
}

// TestValidate_DefaultAuthProxyMethod pins the seeded method tag — proxy users
// are provisioned and stamped under method "proxy".
func TestValidate_DefaultAuthProxyMethod(t *testing.T) {
	require.Equal(t, "proxy", Default().AuthProxy.Method)
}

func TestValidateAuthProxy_DisabledIgnoresGarbage(t *testing.T) {
	// A disabled proxy section never validates its fields, even malformed ones.
	require.NoError(t, validateWithAuthProxy(t, schema.AuthProxy{
		Enabled:        false,
		UserHeader:     "",
		TrustedProxies: []string{"not-an-ip", "garbage/99"},
	}))
}

func TestValidateAuthProxy_EnabledRequiresUserHeader(t *testing.T) {
	err := validateWithAuthProxy(t, schema.AuthProxy{Enabled: true, UserHeader: ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "user_header")
}

func TestValidateAuthProxy_EnabledRejectsBadTrustedProxy(t *testing.T) {
	err := validateWithAuthProxy(t, schema.AuthProxy{
		Enabled:        true,
		UserHeader:     "X-Forwarded-User",
		TrustedProxies: []string{"10.0.0.0/8", "not-an-ip"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "trusted_proxies")
}

func TestValidateAuthProxy_EnabledValidIsOK(t *testing.T) {
	// Bare IP and CIDR are both accepted; empty TrustedProxies is valid (WARN, not error).
	require.NoError(t, validateWithAuthProxy(t, schema.AuthProxy{
		Enabled:        true,
		UserHeader:     "X-Forwarded-User",
		TrustedProxies: []string{"10.0.0.0/8", "192.168.1.10", "::1"},
	}))
	require.NoError(t, validateWithAuthProxy(t, schema.AuthProxy{
		Enabled:        true,
		UserHeader:     "X-Forwarded-User",
		TrustedProxies: nil,
	}))
}

func TestIsListField_AuthProxyTrustedProxies(t *testing.T) {
	require.True(t, isListField("auth_proxy.trusted_proxies"))
	// A non-list auth_proxy field must NOT comma-split.
	require.False(t, isListField("auth_proxy.user_header"))
}

// The validator must accept every spelling the openDB dispatch understands,
// so a config copied from the docs (`type: sqlite`) does not hard-fail at boot.
func TestValidate_AcceptsDriverTypeAliases(t *testing.T) {
	cases := []schema.Database{
		{Type: "sqlite"},                               // canonical SQLite spelling
		{Type: "sqlite", Path: "/tmp/db.sqlite"},       // with an explicit path
		{Type: "mongodb", Host: "mongodb://localhost"}, // mongo alias
		{Type: "pg", DSN: "postgres://u:p@host/db"},    // postgres alias
	}
	for _, db := range cases {
		c := Default()
		c.Core.Database = db
		require.NoErrorf(t, c.Validate(), "type %q should validate", db.Type)
	}
}
