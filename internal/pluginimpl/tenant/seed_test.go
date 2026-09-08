package tenant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDefaultRolesGrantDeliveryLogRead pins the per-tenant seed against the
// platform seed in internal/core/bootstrap_db.go. The two lists drifted once
// already (this one had ro_all, the other did not), and the consequence is
// invisible until an operator with only the notifications role opens the
// Deliveries tab and gets a 403 from GET /api/v1/notificationlog.
func TestDefaultRolesGrantDeliveryLogRead(t *testing.T) {
	byName := map[string][]string{}
	for _, r := range defaultRoles {
		name, _ := r["name"].(string)
		perms, _ := r["permissions"].([]string)
		byName[name] = perms
	}

	require.Contains(t, byName, "notifications")
	require.Contains(t, byName["notifications"], "rw_notification")
	require.Contains(t, byName["notifications"], "ro_notificationlog",
		"the notifications role must be able to read the delivery log")

	// The other two seeds are untouched by this change.
	require.Equal(t, []string{"rw_all"}, byName["admin"])
	require.Equal(t, []string{"ro_all"}, byName["viewer"])
}
