package user

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestGuardDelete_LastPlatformAdmin_Blocked(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host, db.Document{"tenant_id": "default", "name": "root", "method": "local", "enabled": true, "roles": []any{"platform_admin"}})
	require.Error(t, p.GuardDelete(defCtx(rwTenant), []string{uids[0]}))
}

func TestGuardDelete_NonLastPlatformAdmin_Allowed(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host,
		db.Document{"tenant_id": "default", "name": "root", "method": "local", "enabled": true, "roles": []any{"platform_admin"}},
		db.Document{"tenant_id": "default", "name": "two", "method": "local", "enabled": true, "roles": []any{"platform_admin"}},
	)
	require.NoError(t, p.GuardDelete(defCtx(rwTenant), []string{uids[1]}))
}

func TestGuardDelete_DeletingAllPlatformAdminsAtOnce_Blocked(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host,
		db.Document{"tenant_id": "default", "name": "root", "method": "local", "enabled": true, "roles": []any{"platform_admin"}},
		db.Document{"tenant_id": "default", "name": "two", "method": "local", "enabled": true, "roles": []any{"platform_admin"}},
	)
	// Bulk-deleting every holder must be blocked, not allowed because "another
	// holder exists" within the same batch.
	require.Error(t, p.GuardDelete(defCtx(rwTenant), []string{uids[0], uids[1]}))
}

func TestGuardDelete_NonAdminUser_Allowed(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host, db.Document{"tenant_id": "default", "name": "bob", "method": "local", "enabled": true, "roles": []any{"admin"}})
	require.NoError(t, p.GuardDelete(defCtx(rwTenant), []string{uids[0]}))
}

func TestGuardDelete_PlatformScopeExempt(t *testing.T) {
	p, _ := newGuardPlugin(t)
	require.NoError(t, p.GuardDelete(snoozetypes.WithPlatformScope(context.Background()), []string{"anything"}))
}
