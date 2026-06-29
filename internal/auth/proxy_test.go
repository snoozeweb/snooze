package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func schemaAuthProxyForTest() schema.AuthProxy {
	a := schema.DefaultAuthProxy()
	a.Enabled = true
	return a
}

func newProxyRequest(headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// newTestProxyAuthenticator wires a ProxyAuthenticator over a fakeDB seeded
// with an "ops" -> "operator" group->role mapping carrying the ro_rule perm. A
// fixed clock makes created_at/updated_at deterministic.
func newTestProxyAuthenticator(t *testing.T) (*ProxyAuthenticator, *fakeDB) {
	t.Helper()
	f := newFakeDB()
	f.seed(RoleCollection, db.Document{
		"name":        "operator",
		"tenant_id":   snoozetypes.DefaultTenant,
		"permissions": []string{"ro_rule"},
		"groups":      []string{"ops"},
	})
	fixed := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	p := NewProxyAuthenticator(f, NewRoleResolver(f), "proxy")
	p.Clock = func() time.Time { return fixed }
	return p, f
}

func TestProxyAuthenticator_AutoSignupCreatesUser(t *testing.T) {
	p, f := newTestProxyAuthenticator(t)

	id := Identity{Username: "alice", Method: "proxy", TenantID: snoozetypes.DefaultTenant, Groups: []string{"ops"}}
	claims, err := p.Authenticate(context.Background(), id, true)
	require.NoError(t, err)

	// Claims carry the proxied identity and the group-resolved role/perm.
	require.Equal(t, "alice", claims.Subject)
	require.Equal(t, "proxy", claims.Method)
	require.Equal(t, snoozetypes.DefaultTenant, claims.TenantID)
	require.Equal(t, []string{"operator"}, claims.Roles)
	require.Equal(t, []string{"ro_rule"}, claims.Permissions)
	require.Equal(t, []string{"ops"}, claims.Groups)
	// Ephemeral per-request claims are never signed: no exp/iss.
	require.Zero(t, claims.ExpiresAt)
	require.Empty(t, claims.Issuer)

	// A user document now exists, method=proxy, no password field, enabled, groups synced.
	doc, err := f.GetOne(context.Background(), LocalCollection, db.Document{"name": "alice", "method": "proxy"})
	require.NoError(t, err)
	require.Equal(t, "alice", doc["name"])
	require.Equal(t, "proxy", doc["method"])
	require.Equal(t, true, doc["enabled"])
	require.Equal(t, snoozetypes.DefaultTenant, doc["tenant_id"])
	require.NotContains(t, doc, "password")
	require.Equal(t, []string{"ops"}, doc["groups"])
	require.NotEmpty(t, doc["created_at"])
}

func TestProxyAuthenticator_NoAutoSignupRejectsUnknown(t *testing.T) {
	p, f := newTestProxyAuthenticator(t)

	id := Identity{Username: "bob", Method: "proxy", TenantID: snoozetypes.DefaultTenant, Groups: []string{"ops"}}
	_, err := p.Authenticate(context.Background(), id, false)
	require.ErrorIs(t, err, ErrUserNotProvisioned)

	// No user doc was written.
	_, err = f.GetOne(context.Background(), LocalCollection, db.Document{"name": "bob", "method": "proxy"})
	require.ErrorIs(t, err, db.ErrNotFound)
}

func TestProxyAuthenticator_ResyncsGroups(t *testing.T) {
	p, f := newTestProxyAuthenticator(t)

	// Pre-seed an existing proxy user with stale groups (and NO password).
	f.seed(LocalCollection, db.Document{
		"tenant_id":  snoozetypes.DefaultTenant,
		"name":       "carol",
		"method":     "proxy",
		"enabled":    true,
		"groups":     []string{"old"},
		"created_at": "2020-01-01T00:00:00Z",
	})

	id := Identity{Username: "carol", Method: "proxy", TenantID: snoozetypes.DefaultTenant, Groups: []string{"new", "ops"}}
	claims, err := p.Authenticate(context.Background(), id, true)
	require.NoError(t, err)

	// Groups on the doc are re-synced to the latest IdP set.
	doc, err := f.GetOne(context.Background(), LocalCollection, db.Document{"name": "carol", "method": "proxy"})
	require.NoError(t, err)
	require.Equal(t, []string{"new", "ops"}, doc["groups"])
	// created_at is preserved (upsert only updates groups/updated_at), no password introduced.
	require.Equal(t, "2020-01-01T00:00:00Z", doc["created_at"])
	require.NotContains(t, doc, "password")
	// The freshly-synced "ops" group resolves to the operator role.
	require.Equal(t, []string{"operator"}, claims.Roles)
}

// TestProxyAuthenticator_NoAutoSignupAllowsKnown confirms a pre-provisioned
// user authenticates even with auto-signup off (only UNKNOWN users are
// rejected).
func TestProxyAuthenticator_NoAutoSignupAllowsKnown(t *testing.T) {
	p, f := newTestProxyAuthenticator(t)
	f.seed(LocalCollection, db.Document{
		"tenant_id": snoozetypes.DefaultTenant,
		"name":      "dave",
		"method":    "proxy",
		"enabled":   true,
		"groups":    []string{"ops"},
	})

	id := Identity{Username: "dave", Method: "proxy", TenantID: snoozetypes.DefaultTenant, Groups: []string{"ops"}}
	claims, err := p.Authenticate(context.Background(), id, false)
	require.NoError(t, err)
	require.Equal(t, "dave", claims.Subject)
	require.Equal(t, []string{"operator"}, claims.Roles)
}

// TestIdentityFromProxyHeaders covers the thin header parser: it trims and
// drops empty group entries and reports presence of the user header.
func TestIdentityFromProxyHeaders(t *testing.T) {
	cfg := schemaAuthProxyForTest()

	t.Run("happy path", func(t *testing.T) {
		r := newProxyRequest(map[string]string{
			"X-Forwarded-User":   "alice",
			"X-Forwarded-Groups": " ops , , admins ",
		})
		id, ok := IdentityFromProxyHeaders(r, cfg)
		require.True(t, ok)
		require.Equal(t, "alice", id.Username)
		require.Equal(t, "proxy", id.Method)
		require.Equal(t, snoozetypes.DefaultTenant, id.TenantID)
		require.Equal(t, []string{"ops", "admins"}, id.Groups)
	})

	t.Run("missing user header", func(t *testing.T) {
		r := newProxyRequest(map[string]string{"X-Forwarded-Groups": "ops"})
		_, ok := IdentityFromProxyHeaders(r, cfg)
		require.False(t, ok)
	})

	t.Run("blank user header trimmed to empty", func(t *testing.T) {
		r := newProxyRequest(map[string]string{"X-Forwarded-User": "   "})
		_, ok := IdentityFromProxyHeaders(r, cfg)
		require.False(t, ok)
	})

	t.Run("no groups header yields no groups", func(t *testing.T) {
		r := newProxyRequest(map[string]string{"X-Forwarded-User": "alice"})
		id, ok := IdentityFromProxyHeaders(r, cfg)
		require.True(t, ok)
		require.Empty(t, id.Groups)
	})
}

func TestProxyAuthenticator_NilDBErrors(t *testing.T) {
	p := NewProxyAuthenticator(nil, nil, "proxy")
	_, err := p.Authenticate(context.Background(), Identity{Username: "x", Method: "proxy"}, true)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrUserNotProvisioned))
}
