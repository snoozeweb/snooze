package tenant_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/pluginimpl/tenant"
	"github.com/snoozeweb/snooze/internal/plugins"
)

func TestPluginConstants(t *testing.T) {
	require.Equal(t, "tenant", tenant.Collection)
	require.Equal(t, "active", tenant.StatusActive)
	require.Equal(t, "suspended", tenant.StatusSuspended)
}

func TestPluginImplementsInterfaces(t *testing.T) {
	p := tenant.New()
	var _ plugins.Plugin = p
	var _ plugins.DataModel = p
	var _ plugins.PrimaryKeyer = p
	var _ plugins.CreateHook = p
}

func TestPluginName(t *testing.T) {
	p := tenant.New()
	require.Equal(t, "tenant", p.Name())
}

func TestPrimaryKey(t *testing.T) {
	p := tenant.New()
	require.Equal(t, []string{"id"}, p.PrimaryKey())
}

func TestValidate_MissingID(t *testing.T) {
	// A body whose id is present but empty is rejected with an id error.
	// (An absent id key is a PATCH partial and is tolerated — see
	// TestValidate_PatchPartial; absent-id-on-create is enforced at the
	// PrimaryKey/driver layer, not in Validate.)
	p := tenant.New()
	err := p.Validate(map[string]any{"id": "", "display_name": "Acme"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "id")
}

func TestValidate_EmptyID(t *testing.T) {
	p := tenant.New()
	err := p.Validate(map[string]any{"id": ""})
	require.Error(t, err)
}

func TestValidate_ValidDoc(t *testing.T) {
	p := tenant.New()
	err := p.Validate(map[string]any{"id": "acme", "display_name": "Acme Corp"})
	require.NoError(t, err)
}

func TestValidate_PatchPartial(t *testing.T) {
	// PATCH bodies without id must be accepted (partial update).
	p := tenant.New()
	err := p.Validate(map[string]any{"display_name": "New Name"})
	// only id:""  errors; absent id is allowed for PATCH partials
	require.NoError(t, err)
}

func TestValidate_InvalidSlug(t *testing.T) {
	p := tenant.New()
	err := p.Validate(map[string]any{"id": "Bad Slug!"})
	require.Error(t, err)
}

func TestValidate_ReservedDefault(t *testing.T) {
	// The "default" slug is valid on create (seeded at boot), but deletion
	// is blocked at the handler layer (not Validate). Creating via Validate is OK.
	p := tenant.New()
	err := p.Validate(map[string]any{"id": "default", "display_name": "Default"})
	require.NoError(t, err)
}

func TestPostInit_NilHostOK(t *testing.T) {
	p := tenant.New()
	err := p.PostInit(context.Background(), nil)
	require.NoError(t, err)
}

func TestTenantCollectionIsGlobal(t *testing.T) {
	// The tenant collection must be registered as global so the driver
	// skips tenant_id injection for registry queries.
	require.True(t, db.IsGlobalCollection(tenant.Collection),
		"tenant collection must be in the global set")
}
