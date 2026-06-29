package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultAuthProxy(t *testing.T) {
	d := DefaultAuthProxy()
	require.False(t, d.Enabled)
	require.Equal(t, "X-Forwarded-User", d.UserHeader)
	require.Equal(t, "X-Forwarded-Groups", d.GroupsHeader)
	require.Equal(t, ",", d.GroupsSep)
	require.True(t, d.AutoSignup)
	require.Equal(t, "proxy", d.Method)
	// No trusted proxies by default — fail-open IP gate, validated by a boot WARN.
	require.Empty(t, d.TrustedProxies)
}
