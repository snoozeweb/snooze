package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/snoozeweb/snooze/internal/db"
)

func TestEnsureTenantAdmin_CreatesWhenAbsent(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	res, err := EnsureTenantAdmin(context.Background(), fdb, "", false)
	require.NoError(t, err)
	require.True(t, res.Created)
	require.Equal(t, DefaultAdminUsername, res.Username)
	require.GreaterOrEqual(t, len(res.Password), 32)

	doc, err := fdb.GetOne(context.Background(), LocalCollection, db.Document{
		"name": "admin", "method": LocalMethod,
	})
	require.NoError(t, err)
	require.Equal(t, true, doc["enabled"])
	require.Equal(t, []string{"admin"}, doc["roles"])
	hash, _ := doc["password"].(string)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte(res.Password)))
}

func TestEnsureTenantAdmin_CustomUsername(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	res, err := EnsureTenantAdmin(context.Background(), fdb, "ops", false)
	require.NoError(t, err)
	require.Equal(t, "ops", res.Username)
	doc, err := fdb.GetOne(context.Background(), LocalCollection, db.Document{"name": "ops", "method": LocalMethod})
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestEnsureTenantAdmin_SkipsExistingWhenNoReset(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name": "admin", "method": LocalMethod, "enabled": true,
		"password": "$2a$10$old", "roles": []string{"admin", "custom"},
	})
	res, err := EnsureTenantAdmin(context.Background(), fdb, "admin", false)
	require.NoError(t, err)
	require.False(t, res.Created)
	require.Empty(t, res.Password, "no password when an existing admin is left untouched")
	doc, _ := fdb.GetOne(context.Background(), LocalCollection, db.Document{"name": "admin", "method": LocalMethod})
	require.Equal(t, "$2a$10$old", doc["password"], "password unchanged")
}

func TestEnsureTenantAdmin_ResetsExistingPreservingRoles(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name": "admin", "method": LocalMethod, "enabled": true,
		"password": "$2a$10$old", "roles": []string{"admin", "custom"},
	})
	res, err := EnsureTenantAdmin(context.Background(), fdb, "admin", true)
	require.NoError(t, err)
	require.False(t, res.Created, "reset of an existing user is not a create")
	require.GreaterOrEqual(t, len(res.Password), 32)

	doc, _ := fdb.GetOne(context.Background(), LocalCollection, db.Document{"name": "admin", "method": LocalMethod})
	hash, _ := doc["password"].(string)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte(res.Password)), "password is the new one")
	require.Equal(t, []string{"admin", "custom"}, doc["roles"], "roles preserved on reset")
}

func TestEnsureTenantAdmin_NilDriver(t *testing.T) {
	t.Parallel()
	_, err := EnsureTenantAdmin(context.Background(), nil, "", false)
	require.Error(t, err)
}

func TestEnsureTenantAdmin_ResetWhenAbsentCreates(t *testing.T) {
	t.Parallel()
	// resetIfExists=true on an empty DB still creates the admin (ensure semantics).
	fdb := newFakeDB()
	res, err := EnsureTenantAdmin(context.Background(), fdb, "admin", true)
	require.NoError(t, err)
	require.True(t, res.Created)
	require.GreaterOrEqual(t, len(res.Password), 32)
}
