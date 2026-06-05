// internal/mq/tenant_test.go
package mq

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantQueue(t *testing.T) {
	require.Equal(t, "notifications.acme", TenantQueue("notifications", "acme"))
}

func TestTenantQueue_DefaultTenant(t *testing.T) {
	require.Equal(t, "notifications.default", TenantQueue("notifications", "default"))
}

func TestTenantQueue_EmptyTenant(t *testing.T) {
	// When tenant is empty the queue name is returned as-is (global queue).
	require.Equal(t, "notifications", TenantQueue("notifications", ""))
}
