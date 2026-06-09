package role

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

type testHost struct{ drv *sqlite.Driver }

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return &testHost{drv: drv}
}

func (h *testHost) DB() db.Driver                { return h.drv }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return slog.Default() }
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("role-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "role"))
}

func TestPostInitRoundtrip(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "role"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, "role", p.Name())
}

func TestValidate(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(map[string]any{"name": "admin"}))
	require.NoError(t, p.Validate(nil))
	require.Error(t, p.Validate(map[string]any{"name": ""}))
}

func TestRolePlugin_PrimaryKey(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.Equal(t, []string{"tenant_id", "name"}, p.PrimaryKey())
}

func TestRolePlugin_Validate_AcceptsValidDoc(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.NoError(t, p.Validate(map[string]any{
		"tenant_id": "acme",
		"name":      "admin",
	}))
}

// TestRolePlugin_Validate_RejectsReservedPermForTenant reproduces C5: a
// tenant-scoped role document that folds a reserved platform permission must be
// rejected by Validate. Before the fix Validate accepted any free-form
// permissions[] verbatim, so a tenant user with rw_role could mint a role
// {permissions:[rw_tenant]}, self-assign and escalate to platform admin.
func TestRolePlugin_Validate_RejectsReservedPermForTenant(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	for _, perm := range []string{"rw_tenant", "ro_tenant"} {
		err := p.Validate(map[string]any{
			"tenant_id":   "acme",
			"name":        "sneaky",
			"permissions": []any{perm},
		})
		require.Error(t, err, "tenant role carrying %q must be rejected", perm)
	}
}

// TestRolePlugin_Validate_RejectsPlatformAdminNameForTenant: a tenant cannot
// create or impersonate the platform_admin role.
func TestRolePlugin_Validate_RejectsPlatformAdminNameForTenant(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	err := p.Validate(map[string]any{
		"tenant_id": "acme",
		"name":      "platform_admin",
	})
	require.Error(t, err)
}

// Reserved perms are locked to the seeded platform_admin role: the API rejects
// them on ANY role, including default-tenant docs. (Bootstrap bypasses Validate.)
func TestRolePlugin_Validate_RejectsReservedPermEvenForDefaultTenant(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.Error(t, p.Validate(map[string]any{
		"tenant_id":   "default",
		"name":        "evil",
		"permissions": []any{"rw_tenant"},
	}))
	require.Error(t, p.Validate(map[string]any{
		"tenant_id": "default",
		"name":      "platform_admin",
	}))
}

func TestRolePlugin_GuardDelete_ProtectsPlatformAdminRole(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "role"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	res, err := host.drv.Write(ctx, "role", []db.Document{
		{"name": "platform_admin", "permissions": []any{"rw_tenant"}},
		{"name": "ops"},
	}, db.WriteOptions{Primary: []string{"tenant_id", "name"}})
	require.NoError(t, err)
	require.Len(t, res.Added, 2)

	uidPA := res.Added[0]
	uidOps := res.Added[1]

	require.Error(t, p.GuardDelete(ctx, []string{uidPA}), "platform_admin role must be undeletable")
	require.NoError(t, p.GuardDelete(ctx, []string{uidOps}), "ordinary roles delete normally")
}

// TestRolePlugin_TransformWrite_RejectsReservedPermInTenantCtx is the trusted
// runtime guard: the request context (not the client-supplied tenant_id field)
// decides. A tenant-scoped context must reject a reserved permission.
func TestRolePlugin_TransformWrite_RejectsReservedPermInTenantCtx(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	ctx := auth.WithTenant(context.Background(), "acme")
	doc := map[string]any{
		"name":        "sneaky",
		"permissions": []any{"rw_tenant"},
	}
	require.Error(t, p.TransformWrite(ctx, doc))
}

// TestRolePlugin_TransformWrite_AllowsReservedPermInDefaultCtx: a default-tenant
// context (platform admin) may write the reserved permission.
func TestRolePlugin_TransformWrite_AllowsReservedPermInDefaultCtx(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	doc := map[string]any{
		"name":        "platform_admin",
		"permissions": []any{"rw_tenant", "ro_tenant"},
	}
	require.NoError(t, p.TransformWrite(ctx, doc))
}

// TestRolePlugin_TransformWrite_AllowsReservedPermUnderPlatformScope: the
// platform-scope escape hatch is honored.
func TestRolePlugin_TransformWrite_AllowsReservedPermUnderPlatformScope(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	ctx := auth.WithPlatformScope(context.Background())
	doc := map[string]any{
		"name":        "platform_admin",
		"permissions": []any{"rw_tenant"},
	}
	require.NoError(t, p.TransformWrite(ctx, doc))
}

func TestRolePlugin_GuardWrite_PlatformAdminRoleImmutable(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "role"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	res, err := host.drv.Write(ctx, "role", []db.Document{
		{"tenant_id": "default", "name": "platform_admin", "permissions": []any{"rw_tenant"}},
		{"tenant_id": "default", "name": "ops"},
	}, db.WriteOptions{Primary: []string{"tenant_id", "name"}})
	require.NoError(t, err)
	paUID, opsUID := res.Added[0], res.Added[1]

	// Editing the platform_admin role (e.g. adding a group that would group-map
	// users into it) must be blocked.
	require.Error(t, p.GuardWrite(ctx, paUID, map[string]any{"groups": []any{"pwned"}}))
	// Creating/naming platform_admin via the body is blocked (belt-and-braces vs Validate).
	require.Error(t, p.GuardWrite(ctx, "", map[string]any{"name": "platform_admin", "groups": []any{"x"}}))
	// Ordinary roles remain editable.
	require.NoError(t, p.GuardWrite(ctx, opsUID, map[string]any{"groups": []any{"x"}}))
	// Platform scope (bootstrap) is exempt.
	require.NoError(t, p.GuardWrite(auth.WithPlatformScope(context.Background()), paUID, map[string]any{"groups": []any{"x"}}))
}
