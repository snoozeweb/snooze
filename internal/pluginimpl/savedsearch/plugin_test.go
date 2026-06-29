package savedsearch

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
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("savedsearch-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

// ctxAs returns a context carrying the given subject + roles, scoped to the
// default tenant, as the auth middleware would.
func ctxAs(subject string, roles ...string) context.Context {
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	return auth.WithClaims(ctx, snoozetypes.Claims{
		Subject:  subject,
		TenantID: snoozetypes.DefaultTenant,
		Roles:    roles,
	})
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "savedsearch"))
}

func TestSchemaAndValidate(t *testing.T) {
	p := &Plugin{}

	// A well-formed document with a non-empty name and query is accepted.
	require.NoError(t, p.Validate(map[string]any{
		"name":  "prod criticals",
		"query": "severity = critical",
	}))

	// An empty name is rejected.
	require.Error(t, p.Validate(map[string]any{"name": "", "query": "severity = critical"}))
	// An empty query is rejected.
	require.Error(t, p.Validate(map[string]any{"name": "prod criticals", "query": ""}))

	// Schema is a JSON-schema object requiring name + query.
	schema, ok := p.Schema().(map[string]any)
	require.True(t, ok)
	require.Equal(t, "object", schema["type"])
	req, _ := schema["required"].([]any)
	require.Contains(t, req, "name")
	require.Contains(t, req, "query")
}

func TestValidateNilDoc(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(nil))
}

func TestPostInitRoundtrip(t *testing.T) {
	host := newTestHost(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	p := &Plugin{meta: plugins.Metadata{Name: "savedsearch"}}
	require.NoError(t, p.PostInit(ctx, host))

	_, err := host.DB().Write(ctx, "savedsearch", []db.Document{
		{"name": "prod criticals", "query": "severity = critical", "owner": "alice"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	require.NoError(t, p.Reload(ctx))

	docs, _, err := host.DB().Search(ctx, "savedsearch", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "prod criticals", docs[0]["name"])
}

func TestTransformWriteStampsOwner(t *testing.T) {
	p := &Plugin{}

	doc := map[string]any{"name": "x", "query": "severity = critical"}
	require.NoError(t, p.TransformWrite(ctxAs("alice"), doc))
	require.Equal(t, "alice", doc["owner"])

	// A client-supplied owner is overwritten with the verified subject.
	doc2 := map[string]any{"name": "x", "query": "q", "owner": "bob"}
	require.NoError(t, p.TransformWrite(ctxAs("alice"), doc2))
	require.Equal(t, "alice", doc2["owner"])
}

func TestGuardWriteOwnership(t *testing.T) {
	p := &Plugin{}

	// Owner writing their own entry is allowed.
	require.NoError(t, p.GuardWrite(ctxAs("alice"), "", map[string]any{"owner": "alice"}, false))

	// A non-owner, non-admin writing someone else's entry is rejected.
	require.Error(t, p.GuardWrite(ctxAs("bob"), "uid-1", map[string]any{"owner": "alice"}, true))

	// An admin may write/replace another owner's entry.
	require.NoError(t, p.GuardWrite(ctxAs("carol", "admin"), "uid-1", map[string]any{"owner": "alice"}, true))
}

func TestGuardDeleteAdminCanDeleteOthers(t *testing.T) {
	host := newTestHost(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	res, err := host.DB().Write(ctx, "savedsearch", []db.Document{
		{"name": "alice search", "query": "q", "owner": "alice"},
	}, db.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	uid := res.Added[0]

	p := &Plugin{meta: plugins.Metadata{Name: "savedsearch"}}
	require.NoError(t, p.PostInit(ctx, host))

	// A non-owner, non-admin cannot delete another user's entry.
	bob := auth.WithClaims(ctx, snoozetypes.Claims{Subject: "bob", TenantID: snoozetypes.DefaultTenant})
	require.Error(t, p.GuardDelete(bob, []string{uid}))

	// An admin may delete another user's entry.
	admin := auth.WithClaims(ctx, snoozetypes.Claims{Subject: "carol", TenantID: snoozetypes.DefaultTenant, Roles: []string{"admin"}})
	require.NoError(t, p.GuardDelete(admin, []string{uid}))

	// The owner may delete their own entry.
	alice := auth.WithClaims(ctx, snoozetypes.Claims{Subject: "alice", TenantID: snoozetypes.DefaultTenant})
	require.NoError(t, p.GuardDelete(alice, []string{uid}))
}
