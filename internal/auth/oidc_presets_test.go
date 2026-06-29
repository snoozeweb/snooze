package auth

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// TestResolvePreset_Google: provider "google" fills the well-known issuer and
// leaves the operator-supplied client fields untouched.
func TestResolvePreset_Google(t *testing.T) {
	o := schema.OIDC{Provider: "google", ClientID: "cid", ClientSecret: "sec"}
	require.NoError(t, ResolvePreset(&o))
	require.Equal(t, "https://accounts.google.com", o.Issuer)
	require.Equal(t, "cid", o.ClientID)
	require.Equal(t, "sec", o.ClientSecret)
}

// TestResolvePreset_Azure_MissingTenant: azure without a tenant param errors.
func TestResolvePreset_Azure_MissingTenant(t *testing.T) {
	o := schema.OIDC{Provider: "azure"}
	err := ResolvePreset(&o)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tenant")
}

// TestResolvePreset_Azure_OK: azure with a tenant resolves to the v2.0 issuer.
func TestResolvePreset_Azure_OK(t *testing.T) {
	o := schema.OIDC{Provider: "azure", ProviderParams: map[string]string{"tenant": "tid-123"}}
	require.NoError(t, ResolvePreset(&o))
	require.Equal(t, "https://login.microsoftonline.com/tid-123/v2.0", o.Issuer)
}

// TestResolvePreset_Keycloak_OK: keycloak needs url + realm.
func TestResolvePreset_Keycloak_OK(t *testing.T) {
	o := schema.OIDC{Provider: "keycloak", ProviderParams: map[string]string{
		"url":   "https://idp.corp.example",
		"realm": "snooze",
	}}
	require.NoError(t, ResolvePreset(&o))
	require.Equal(t, "https://idp.corp.example/realms/snooze", o.Issuer)
}

// TestResolvePreset_ExplicitIssuerWins: an operator-set issuer is never
// overwritten by the preset (preset is advisory).
func TestResolvePreset_ExplicitIssuerWins(t *testing.T) {
	o := schema.OIDC{Provider: "google", Issuer: "https://custom.example/oidc"}
	require.NoError(t, ResolvePreset(&o))
	require.Equal(t, "https://custom.example/oidc", o.Issuer)
}

// TestResolvePreset_UnknownProvider: an unrecognised provider name errors
// (e.g. github, which has no id_token and is out of scope).
func TestResolvePreset_UnknownProvider(t *testing.T) {
	o := schema.OIDC{Provider: "github"}
	err := ResolvePreset(&o)
	require.Error(t, err)
	require.Contains(t, err.Error(), "github")
}

// TestResolvePreset_NoProvider: an empty provider key is a no-op (no error,
// issuer left as-is) — this is the legacy single-OIDC path.
func TestResolvePreset_NoProvider(t *testing.T) {
	o := schema.OIDC{Issuer: "https://login.microsoftonline.com/tid/v2.0"}
	require.NoError(t, ResolvePreset(&o))
	require.Equal(t, "https://login.microsoftonline.com/tid/v2.0", o.Issuer)
}

// TestResolvePreset_Cognito_OK: cognito needs region + pool_id.
func TestResolvePreset_Cognito_OK(t *testing.T) {
	o := schema.OIDC{Provider: "cognito", ProviderParams: map[string]string{
		"region":  "us-east-1",
		"pool_id": "us-east-1_abc",
	}}
	require.NoError(t, ResolvePreset(&o))
	require.Equal(t, "https://cognito-idp.us-east-1.amazonaws.com/us-east-1_abc", o.Issuer)
}

// TestResolvePreset_GitLab_DefaultsToGitlabCom: gitlab with no url defaults to
// gitlab.com.
func TestResolvePreset_GitLab_DefaultsToGitlabCom(t *testing.T) {
	o := schema.OIDC{Provider: "gitlab"}
	require.NoError(t, ResolvePreset(&o))
	require.Equal(t, "https://gitlab.com", o.Issuer)
}
