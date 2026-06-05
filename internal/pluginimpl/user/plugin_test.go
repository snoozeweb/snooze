package user

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"

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
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("user-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "user"))
}

func TestPostInitRoundtrip(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "user"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, "user", p.Name())
}

func TestValidate(t *testing.T) {
	p := &Plugin{}
	t.Run("empty patch ok", func(t *testing.T) {
		require.NoError(t, p.Validate(nil))
		require.NoError(t, p.Validate(map[string]any{}))
	})
	t.Run("valid object", func(t *testing.T) {
		require.NoError(t, p.Validate(map[string]any{"name": "alice", "method": "local"}))
	})
	t.Run("empty name rejected", func(t *testing.T) {
		require.Error(t, p.Validate(map[string]any{"name": "", "method": "local"}))
	})
	t.Run("empty method rejected", func(t *testing.T) {
		require.Error(t, p.Validate(map[string]any{"name": "alice", "method": ""}))
	})
}

func TestTransformWrite_HashesPlaintextPassword(t *testing.T) {
	p := &Plugin{}
	doc := map[string]any{
		"name":     "alice",
		"method":   "local",
		"password": "s3cret",
	}
	require.NoError(t, p.TransformWrite(context.Background(), doc))
	hash, ok := doc["password"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(hash, "$2"), "expected bcrypt hash, got %q", hash)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("s3cret")))
}

func TestTransformWrite_EmptyPasswordIsDropped(t *testing.T) {
	// Mirrors the "admin clears the form" path: an empty string must not
	// overwrite the existing hash.
	p := &Plugin{}
	doc := map[string]any{"name": "alice", "password": ""}
	require.NoError(t, p.TransformWrite(context.Background(), doc))
	_, present := doc["password"]
	require.False(t, present, "empty password should be dropped from the doc")
}

func TestTransformWrite_AbsentPasswordIsNoOp(t *testing.T) {
	// PATCH partial updates that don't touch the password field must pass through.
	p := &Plugin{}
	doc := map[string]any{"name": "alice", "comment": "updated"}
	require.NoError(t, p.TransformWrite(context.Background(), doc))
	require.Equal(t, "alice", doc["name"])
	require.Equal(t, "updated", doc["comment"])
	_, present := doc["password"]
	require.False(t, present)
}

func TestTransformWrite_RejectsPasswordOnNonLocalMethod(t *testing.T) {
	p := &Plugin{}
	doc := map[string]any{
		"name":     "alice",
		"method":   "ldap",
		"password": "s3cret",
	}
	err := p.TransformWrite(context.Background(), doc)
	require.Error(t, err)
}

func TestSchemaShape(t *testing.T) {
	s := (&Plugin{}).Schema().(map[string]any)
	require.Equal(t, "object", s["type"])
	props := s["properties"].(map[string]any)
	require.Contains(t, props, "name")
	require.Contains(t, props, "method")
}

func TestUserPlugin_PrimaryKey(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	pk, ok := any(p).(interface{ PrimaryKey() []string })
	require.True(t, ok, "user.Plugin must implement PrimaryKey()")
	require.Equal(t, []string{"tenant_id", "name", "method"}, pk.PrimaryKey())
}

func TestUserPlugin_Validate_AcceptsValidDoc(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.NoError(t, p.Validate(map[string]any{
		"tenant_id": "acme",
		"name":      "alice",
		"method":    "local",
	}))
}

func TestUserPlugin_Validate_RejectsMissingName(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.Error(t, p.Validate(map[string]any{"name": "", "method": "local"}))
}

// TestUserPlugin_Validate_RejectsPlatformAdminRoleForTenant reproduces the
// user-side of C5: a tenant-scoped user document that references the
// platform_admin role (via roles or static_roles) must be rejected. Before the
// fix a tenant user with rw_user could self-assign platform_admin and escalate.
func TestUserPlugin_Validate_RejectsPlatformAdminRoleForTenant(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	t.Run("roles", func(t *testing.T) {
		err := p.Validate(map[string]any{
			"tenant_id": "acme",
			"name":      "mallory",
			"method":    "local",
			"roles":     []any{"platform_admin"},
		})
		require.Error(t, err)
	})
	t.Run("static_roles", func(t *testing.T) {
		err := p.Validate(map[string]any{
			"tenant_id":    "acme",
			"name":         "mallory",
			"method":       "local",
			"static_roles": []any{"platform_admin"},
		})
		require.Error(t, err)
	})
}

// TestUserPlugin_Validate_AllowsPlatformAdminRoleForDefaultTenant: the
// default-tenant path may assign platform_admin (this is how root is managed).
func TestUserPlugin_Validate_AllowsPlatformAdminRoleForDefaultTenant(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.NoError(t, p.Validate(map[string]any{
		"tenant_id": "default",
		"name":      "root",
		"method":    "local",
		"roles":     []any{"admin", "platform_admin"},
	}))
}

// TestUserPlugin_TransformWrite_RejectsPlatformAdminRoleInTenantCtx is the
// trusted runtime guard keyed off the request context.
func TestUserPlugin_TransformWrite_RejectsPlatformAdminRoleInTenantCtx(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	ctx := auth.WithTenant(context.Background(), "acme")
	doc := map[string]any{
		"name":   "mallory",
		"method": "local",
		"roles":  []any{"platform_admin"},
	}
	require.Error(t, p.TransformWrite(ctx, doc))
}

// TestUserPlugin_TransformWrite_AllowsPlatformAdminRoleInDefaultCtx: a
// default-tenant context permits assigning the platform_admin role.
func TestUserPlugin_TransformWrite_AllowsPlatformAdminRoleInDefaultCtx(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	doc := map[string]any{
		"name":   "root",
		"method": "local",
		"roles":  []any{"admin", "platform_admin"},
	}
	require.NoError(t, p.TransformWrite(ctx, doc))
}
