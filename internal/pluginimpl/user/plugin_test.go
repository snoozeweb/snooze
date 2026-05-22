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

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
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
