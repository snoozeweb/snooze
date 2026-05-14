package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/japannext/snooze/internal/db"
)

func TestEnsureRoot_FirstBoot(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	pwd, err := EnsureRoot(context.Background(), fdb)
	require.NoError(t, err)
	require.NotEmpty(t, pwd, "first boot must return a non-empty password")

	// The password must be base64url and >= 32 chars.
	require.GreaterOrEqual(t, len(pwd), 32)

	// The user document must exist with a verifiable bcrypt hash.
	doc, err := fdb.GetOne(context.Background(), LocalCollection, db.Document{
		"name":   RootUsername,
		"method": LocalMethod,
	})
	require.NoError(t, err)
	require.Equal(t, RootUsername, doc["name"])
	require.Equal(t, true, doc["enabled"])
	hash, _ := doc["password"].(string)
	require.NotEmpty(t, hash)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte(pwd)))
}

func TestEnsureRoot_AlreadyProvisioned(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	// Seed an existing root user.
	fdb.seed(LocalCollection, db.Document{
		"name":     RootUsername,
		"method":   LocalMethod,
		"enabled":  true,
		"password": "$2a$10$existinghashdoesnotmatter........................",
	})

	pwd, err := EnsureRoot(context.Background(), fdb)
	require.NoError(t, err)
	require.Empty(t, pwd, "must return empty password when root already exists")
}

func TestEnsureRoot_NilDriver(t *testing.T) {
	t.Parallel()
	_, err := EnsureRoot(context.Background(), nil)
	require.Error(t, err)
}

func TestEnsureRoot_AuthenticatesAfterBootstrap(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	pwd, err := EnsureRoot(context.Background(), fdb)
	require.NoError(t, err)

	// Round-trip: the generated password must authenticate via LocalProvider.
	p := NewLocalProvider(fdb)
	id, err := p.Authenticate(context.Background(), Credentials{Username: RootUsername, Password: pwd})
	require.NoError(t, err)
	require.Equal(t, RootUsername, id.Username)
	require.Equal(t, LocalMethod, id.Method)
}
