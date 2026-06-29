package group

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
	"github.com/snoozeweb/snooze/internal/condition"
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
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("group-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestGroupPlugin_Metadata(t *testing.T) {
	t.Parallel()
	p := &Plugin{meta: plugins.Metadata{Name: "group"}}
	require.Equal(t, "group", p.Name())
	require.Equal(t, "group", p.Metadata().Name)
}

func TestGroupPlugin_Validate_RequiresName(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	// Empty patch (PATCH partial) is tolerated.
	require.NoError(t, p.Validate(map[string]any{}))
	require.NoError(t, p.Validate(nil))
	// A present-but-empty name is rejected.
	require.Error(t, p.Validate(map[string]any{"name": ""}))
}

func TestGroupPlugin_Validate_AcceptsValidDoc(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.NoError(t, p.Validate(map[string]any{"name": "sre"}))
	require.NoError(t, p.Validate(map[string]any{
		"name":        "sre",
		"description": "SRE team",
		"members": []any{
			map[string]any{"username": "alice", "method": "local"},
		},
	}))
}

func TestGroupPlugin_PrimaryKey(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.Equal(t, []string{"tenant_id", "name"}, p.PrimaryKey())
}

func TestGroupPlugin_Schema_HasRequiredFields(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	schema, ok := p.Schema().(map[string]any)
	require.True(t, ok)
	required, ok := schema["required"].([]any)
	require.True(t, ok)
	require.Contains(t, required, "name")
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, props, "name")
	require.Contains(t, props, "description")
	require.Contains(t, props, "members")
}

func TestGroupPlugin_Registration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "group"))
}

func TestGroupPlugin_CRUD_RoundTrip(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "group"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	require.NoError(t, p.Reload(context.Background()))

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	primary := []string{"tenant_id", "name"}

	// Create.
	res, err := host.drv.Write(ctx, "group", []db.Document{
		{
			"name":        "sre",
			"description": "SRE team",
			"members": []any{
				map[string]any{"username": "alice", "method": "local"},
				map[string]any{"username": "bob", "method": "ldap"},
			},
		},
	}, db.WriteOptions{Primary: primary, DuplicatePolicy: "reject"})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	uid := res.Added[0]

	// Fetch.
	got, err := host.drv.GetOne(ctx, "group", db.Document{"uid": uid})
	require.NoError(t, err)
	require.Equal(t, "sre", got["name"])
	require.Equal(t, "SRE team", got["description"])
	members, ok := got["members"].([]any)
	require.True(t, ok)
	require.Len(t, members, 2)
	m0, ok := members[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "alice", m0["username"])
	require.Equal(t, "local", m0["method"])

	// Update the description (upsert on the same primary key).
	_, err = host.drv.Write(ctx, "group", []db.Document{
		{"name": "sre", "description": "Site Reliability Engineering"},
	}, db.WriteOptions{Primary: primary})
	require.NoError(t, err)
	got, err = host.drv.GetOne(ctx, "group", db.Document{"uid": uid})
	require.NoError(t, err)
	require.Equal(t, "Site Reliability Engineering", got["description"])

	// Delete and confirm it is gone.
	n, err := host.drv.Delete(ctx, "group", condition.Equals("uid", uid), false)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = host.drv.GetOne(ctx, "group", db.Document{"uid": uid})
	require.ErrorIs(t, err, db.ErrNotFound)
}
