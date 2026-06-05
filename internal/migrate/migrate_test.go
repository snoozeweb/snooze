// internal/migrate/migrate_test.go
package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScopedCollections_DoesNotContainGlobals(t *testing.T) {
	t.Parallel()
	globals := map[string]struct{}{
		"tenant":    {},
		"secrets":   {},
		"nodes":     {},
		"heartbeat": {},
	}
	for _, c := range TenantScopedCollections {
		_, isGlobal := globals[c]
		require.False(t, isGlobal, "TenantScopedCollections must not contain global collection %q", c)
	}
}

func TestScopedCollections_ContainsExpected(t *testing.T) {
	t.Parallel()
	required := []string{"record", "rule", "user", "role", "snooze", "aggregaterule",
		"notification", "audit", "stats", "settings", "refresh_token"}
	have := make(map[string]struct{}, len(TenantScopedCollections))
	for _, c := range TenantScopedCollections {
		have[c] = struct{}{}
	}
	for _, want := range required {
		_, ok := have[want]
		require.True(t, ok, "TenantScopedCollections must include %q", want)
	}
}
