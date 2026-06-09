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
