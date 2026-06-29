package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// validateWithOIDC builds a valid Default() config, swaps in the given OIDC
// section, and runs the full Config.Validate() — exercising validateOIDC
// through the public entry point, the same way the LDAP/database cases are
// tested.
func validateWithOIDC(t *testing.T, o schema.OIDC) error {
	t.Helper()
	c := Default()
	c.OIDC = o
	return c.Validate()
}

func TestValidateOIDC_DisabledNoRequirements(t *testing.T) {
	require.NoError(t, validateWithOIDC(t, schema.OIDC{Enabled: false}))
}

func TestValidateOIDC_EnabledRequiresFields(t *testing.T) {
	err := validateWithOIDC(t, schema.OIDC{Enabled: true})
	require.Error(t, err)
	for _, want := range []string{"issuer", "client_id", "client_secret", "redirect_url"} {
		require.Contains(t, err.Error(), want)
	}
}

func TestValidateOIDC_EnabledHTTPSIssuer(t *testing.T) {
	err := validateWithOIDC(t, schema.OIDC{
		Enabled: true, Issuer: "http://insecure/x", ClientID: "c",
		ClientSecret: "s", RedirectURL: "https://snooze/cb",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "https")
}

// TestValidateOIDCEntries_DuplicateMethod: two entries sharing the same method
// slug fail validation with a "duplicate method" error.
func TestValidateOIDCEntries_DuplicateMethod(t *testing.T) {
	c := Default()
	c.OIDCProviders = []schema.OIDC{
		{Method: "google", Enabled: false},
		{Method: "google", Enabled: false},
	}
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate method")
}

// TestValidateOIDCEntries_PresetParamError: an enabled entry with a bad preset
// (azure missing tenant) fails validation, surfacing the preset error early.
func TestValidateOIDCEntries_PresetParamError(t *testing.T) {
	c := Default()
	c.OIDCProviders = []schema.OIDC{
		{Method: "azure", Provider: "azure", Enabled: true,
			ClientID: "c", ClientSecret: "s", RedirectURL: "https://snooze/cb"},
	}
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "tenant")
}

// TestValidateOIDCEntries_PresetFillsIssuer: an enabled entry with a valid
// preset and no explicit issuer passes validation (ResolvePreset fills the
// issuer before the https check).
func TestValidateOIDCEntries_PresetFillsIssuer(t *testing.T) {
	c := Default()
	c.OIDCProviders = []schema.OIDC{
		{Method: "google", Provider: "google", Enabled: true,
			ClientID: "c", ClientSecret: "s", RedirectURL: "https://snooze/cb"},
	}
	require.NoError(t, c.Validate())
}

// TestValidateOIDCEntries_EnabledRequiresSecret: an enabled multi-IdP entry with
// no client_secret fails validation (mirrors the legacy single-OIDC rule).
func TestValidateOIDCEntries_EnabledRequiresSecret(t *testing.T) {
	c := Default()
	c.OIDCProviders = []schema.OIDC{
		{Method: "google", Provider: "google", Enabled: true,
			ClientID: "c", RedirectURL: "https://snooze/cb"},
	}
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "client_secret")
}

func TestLoad_OIDCEnvOverride(t *testing.T) {
	t.Setenv("SNOOZE_SERVER_OIDC_ENABLED", "true")
	t.Setenv("SNOOZE_SERVER_OIDC_ISSUER", "https://login.microsoftonline.com/tid/v2.0")
	t.Setenv("SNOOZE_SERVER_OIDC_CLIENT_ID", "cid")
	t.Setenv("SNOOZE_SERVER_OIDC_CLIENT_SECRET", "sec")
	t.Setenv("SNOOZE_SERVER_OIDC_REDIRECT_URL", "https://snooze/api/v1/login/microsoft/callback")
	t.Setenv("SNOOZE_SERVER_OIDC_SCOPES", "openid,profile,email,User.Read")
	cfg, err := Load("")
	require.NoError(t, err)
	require.True(t, cfg.OIDC.Enabled)
	require.Equal(t, "cid", cfg.OIDC.ClientID)
	require.Len(t, cfg.OIDC.Scopes, 4)
}
