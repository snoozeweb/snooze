// internal/api/middleware/tenant_resolver_test.go
package middleware_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestTenantResolver_KnownToken(t *testing.T) {
	r := middleware.NewTenantResolver()
	r.Replace(map[string]string{"tok-acme": "acme", "tok-beta": "beta"})

	tenant, ok := r.Lookup("tok-acme")
	require.True(t, ok)
	require.Equal(t, "acme", tenant)

	tenant, ok = r.Lookup("tok-beta")
	require.True(t, ok)
	require.Equal(t, "beta", tenant)
}

func TestTenantResolver_UnknownToken_ReturnsFalse(t *testing.T) {
	r := middleware.NewTenantResolver()
	r.Replace(map[string]string{"tok-acme": "acme"})

	tenant, ok := r.Lookup("nope")
	require.False(t, ok)
	require.Equal(t, snoozetypes.DefaultTenant, tenant)
}

func TestTenantResolver_EmptyToken_ReturnsFalse(t *testing.T) {
	r := middleware.NewTenantResolver()
	r.Replace(map[string]string{"tok-acme": "acme"})

	tenant, ok := r.Lookup("")
	require.False(t, ok)
	require.Equal(t, snoozetypes.DefaultTenant, tenant)
}

func TestTenantResolver_Replace_IsAtomic(t *testing.T) {
	r := middleware.NewTenantResolver()
	r.Replace(map[string]string{"old": "oldtenant"})

	tenant, ok := r.Lookup("old")
	require.True(t, ok)
	require.Equal(t, "oldtenant", tenant)

	// Replace wholesale — old key must be gone.
	r.Replace(map[string]string{"new": "newtenant"})
	_, ok = r.Lookup("old")
	require.False(t, ok)

	tenant, ok = r.Lookup("new")
	require.True(t, ok)
	require.Equal(t, "newtenant", tenant)
}
