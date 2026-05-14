package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/japannext/snooze/internal/db"
)

func mustHash(t *testing.T, password string) string {
	t.Helper()
	// MinCost to keep the test suite fast.
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

func TestLocalProvider_Authenticate_Success(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":     "alice",
		"method":   LocalMethod,
		"enabled":  true,
		"password": mustHash(t, "s3cret"),
		"groups":   []string{"ops", "devs"},
	})

	p := NewLocalProvider(fdb)
	id, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "s3cret"})
	require.NoError(t, err)
	require.Equal(t, "alice", id.Username)
	require.Equal(t, LocalMethod, id.Method)
	require.ElementsMatch(t, []string{"ops", "devs"}, id.Groups)
}

func TestLocalProvider_Name(t *testing.T) {
	t.Parallel()
	require.Equal(t, "local", (&LocalProvider{}).Name())
}

func TestLocalProvider_Authenticate_BadPassword(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":     "alice",
		"method":   LocalMethod,
		"enabled":  true,
		"password": mustHash(t, "s3cret"),
	})

	p := NewLocalProvider(fdb)
	_, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "WRONG"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidCredentials), "got: %v", err)
}

func TestLocalProvider_Authenticate_UnknownUser(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	p := NewLocalProvider(fdb)
	_, err := p.Authenticate(context.Background(), Credentials{Username: "ghost", Password: "any"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidCredentials), "got: %v", err)
}

func TestLocalProvider_Authenticate_Disabled(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":     "alice",
		"method":   LocalMethod,
		"enabled":  false,
		"password": mustHash(t, "s3cret"),
	})
	p := NewLocalProvider(fdb)
	_, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "s3cret"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUserDisabled), "got: %v", err)
}

func TestLocalProvider_Authenticate_EmptyCreds(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	p := NewLocalProvider(fdb)
	_, err := p.Authenticate(context.Background(), Credentials{})
	require.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestLocalProvider_Authenticate_MissingHash(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":    "alice",
		"method":  LocalMethod,
		"enabled": true,
		// no password field at all.
	})
	p := NewLocalProvider(fdb)
	_, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "anything"})
	require.True(t, errors.Is(err, ErrInvalidCredentials))
}

// TestLocalProvider_AuthenticatePortOf_TestBasicAuth ports tests/test_auth.py::test_basic_auth
// to bcrypt semantics — sha256("root") is gone but the spirit (root + password
// round-trips through the local provider) survives.
func TestLocalProvider_AuthenticatePortOf_TestBasicAuth(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":     "root",
		"method":   "local",
		"enabled":  true,
		"password": mustHash(t, "root"),
	})
	p := NewLocalProvider(fdb)
	id, err := p.Authenticate(context.Background(), Credentials{Username: "root", Password: "root"})
	require.NoError(t, err)
	require.Equal(t, "root", id.Username)
	require.Equal(t, "local", id.Method)
}
