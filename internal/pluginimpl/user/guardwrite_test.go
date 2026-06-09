package user

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func newGuardPlugin(t *testing.T) (*Plugin, *testHost) {
	t.Helper()
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "user"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p, host
}

// seedUsers writes users letting the driver assign uids (the sqlite driver
// rejects pre-set uids) and returns the assigned uids in write order.
func seedUsers(t *testing.T, host *testHost, ctx context.Context, docs ...db.Document) []string {
	t.Helper()
	res, err := host.drv.Write(ctx, "user", docs, db.WriteOptions{Primary: []string{"tenant_id", "name", "method"}})
	require.NoError(t, err)
	return res.Added
}

func defCtx(claims snoozetypes.Claims) context.Context {
	return auth.WithClaims(auth.WithTenant(context.Background(), snoozetypes.DefaultTenant), claims)
}

var rwTenant = snoozetypes.Claims{Subject: "root", Method: "local", TenantID: "default", Permissions: []string{"rw_tenant", "rw_all"}}
var rwAllOnly = snoozetypes.Claims{Subject: "alice", Method: "local", TenantID: "default", Permissions: []string{"rw_all"}}

func TestGuardWrite_GrantPlatformAdmin_RequiresLiteralRwTenant(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host, defCtx(rwTenant), db.Document{"tenant_id": "default", "name": "bob", "method": "local", "enabled": true, "roles": []any{"admin"}})
	bob := uids[0]
	require.Error(t, p.GuardWrite(defCtx(rwAllOnly), bob, map[string]any{"roles": []any{"admin", "platform_admin"}}))
	require.NoError(t, p.GuardWrite(defCtx(rwTenant), bob, map[string]any{"roles": []any{"admin", "platform_admin"}}))
}

func TestGuardWrite_RemovePlatformAdmin_RequiresRwTenant_AndBlocksSelfAndLast(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host, defCtx(rwTenant),
		db.Document{"tenant_id": "default", "name": "root", "method": "local", "enabled": true, "roles": []any{"platform_admin"}},
		db.Document{"tenant_id": "default", "name": "two", "method": "local", "enabled": true, "roles": []any{"platform_admin"}},
	)
	root, two := uids[0], uids[1]
	require.Error(t, p.GuardWrite(defCtx(rwAllOnly), two, map[string]any{"roles": []any{}}))
	require.NoError(t, p.GuardWrite(defCtx(rwTenant), two, map[string]any{"roles": []any{}}))
	require.Error(t, p.GuardWrite(defCtx(rwTenant), root, map[string]any{"roles": []any{}})) // self-removal blocked
}

func TestGuardWrite_RemoveLastPlatformAdmin_Blocked(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host, defCtx(rwTenant), db.Document{"tenant_id": "default", "name": "root", "method": "local", "enabled": true, "roles": []any{"platform_admin"}})
	root := uids[0]
	other := snoozetypes.Claims{Subject: "ops", Method: "local", TenantID: "default", Permissions: []string{"rw_tenant"}}
	require.Error(t, p.GuardWrite(defCtx(other), root, map[string]any{"roles": []any{}}))
}

func TestGuardWrite_DisableLastPlatformAdmin_Blocked(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host, defCtx(rwTenant), db.Document{"tenant_id": "default", "name": "root", "method": "local", "enabled": true, "roles": []any{"platform_admin"}})
	root := uids[0]
	require.Error(t, p.GuardWrite(defCtx(rwTenant), root, map[string]any{"enabled": false}))
}

func TestGuardWrite_NonPlatformWritesUnaffected(t *testing.T) {
	p, host := newGuardPlugin(t)
	uids := seedUsers(t, host, defCtx(rwAllOnly), db.Document{"tenant_id": "default", "name": "bob", "method": "local", "enabled": true, "roles": []any{"admin"}})
	bob := uids[0]
	require.NoError(t, p.GuardWrite(defCtx(rwAllOnly), bob, map[string]any{"email": "b@x.io"}))
}

func TestGuardWrite_PlatformScopeExempt(t *testing.T) {
	p, _ := newGuardPlugin(t)
	ctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, p.GuardWrite(ctx, "", map[string]any{"roles": []any{"platform_admin"}}))
}
