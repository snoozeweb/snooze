package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultOIDC(t *testing.T) {
	d := DefaultOIDC()
	require.False(t, d.Enabled)
	require.Equal(t, "microsoft", d.Method)
	require.Equal(t, "Microsoft 365", d.DisplayName)
	require.Equal(t, "microsoft", d.Icon)
	require.Equal(t, "roles", d.RolesClaim)
	require.Equal(t, "groups", d.GroupsClaim)
	require.Equal(t, "Admin", d.AdminRoleValue)
	require.Equal(t, []string{"openid", "profile", "email"}, d.Scopes)
	// Secret/connection fields have no defaults; they are validated only when enabled.
	require.Empty(t, d.Issuer)
	require.Empty(t, d.ClientID)
	require.Empty(t, d.ClientSecret)
	require.Empty(t, d.RedirectURL)
}

// TestDefaultOIDC_NoPresetFields confirms the canonical (Microsoft) default
// carries no preset key — it is the explicit-issuer legacy path.
func TestDefaultOIDC_NoPresetFields(t *testing.T) {
	d := DefaultOIDC()
	require.Empty(t, d.Provider)
	require.Nil(t, d.ProviderParams)
}

// TestOIDC_PresetsInDefault checks the preset convenience constructors pre-fill
// the preset key, display name, icon, and the standard scopes/claim names.
func TestOIDC_PresetsInDefault(t *testing.T) {
	g := DefaultGoogleOIDC()
	require.Equal(t, "google", g.Provider)
	require.Equal(t, "google", g.Method)
	require.Equal(t, "Google", g.DisplayName)
	require.Equal(t, "google", g.Icon)
	require.Equal(t, []string{"openid", "profile", "email"}, g.Scopes)

	a := DefaultAzureOIDC()
	require.Equal(t, "azure", a.Provider)
	require.Equal(t, "azure", a.Method)
	require.Equal(t, "Microsoft Entra", a.DisplayName)
	require.Equal(t, "microsoft", a.Icon)
	require.Equal(t, "roles", a.RolesClaim)

	k := DefaultKeycloakOIDC()
	require.Equal(t, "keycloak", k.Provider)
	require.Equal(t, "keycloak", k.Method)
	require.Equal(t, "Keycloak", k.DisplayName)

	gl := DefaultGitLabOIDC()
	require.Equal(t, "gitlab", gl.Provider)
	require.Equal(t, "gitlab", gl.Method)
	require.Equal(t, "GitLab", gl.DisplayName)

	c := DefaultCognitoOIDC()
	require.Equal(t, "cognito", c.Provider)
	require.Equal(t, "cognito", c.Method)
	require.Equal(t, "AWS Cognito", c.DisplayName)
}
