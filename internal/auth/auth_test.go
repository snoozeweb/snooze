package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// mustBcrypt is a test helper that bcrypt-hashes plaintext or panics.
func mustBcrypt(plaintext string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return string(hash)
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(NewAnonymousProvider(true))
	p, err := r.Get("anonymous")
	require.NoError(t, err)
	require.Equal(t, "anonymous", p.Name())
}

func TestRegistry_GetUnknown(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	_, err := r.Get("missing")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnknownProvider))
}

func TestRegistry_RegisterNil(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(nil) // must not panic
	require.Empty(t, r.Names())
}

func TestRegistry_Names(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(NewAnonymousProvider(true))
	require.ElementsMatch(t, []string{"anonymous"}, r.Names())
}

func TestAnonymousProvider_Authenticate(t *testing.T) {
	t.Parallel()
	p := NewAnonymousProvider(true)
	id, err := p.Authenticate(context.Background(), Credentials{})
	require.NoError(t, err)
	require.Equal(t, AnonymousUsername, id.Username)
	require.Equal(t, AnonymousMethod, id.Method)
	require.Nil(t, id.Groups)
}

func TestAnonymousProvider_Disabled(t *testing.T) {
	t.Parallel()
	p := NewAnonymousProvider(false)
	_, err := p.Authenticate(context.Background(), Credentials{})
	require.True(t, errors.Is(err, ErrProviderDisabled))
}

func TestCtx_WithAndFromClaims(t *testing.T) {
	t.Parallel()
	in := snoozetypes.Claims{Subject: "alice", Method: "local"}
	ctx := WithClaims(context.Background(), in)
	out, ok := ClaimsFrom(ctx)
	require.True(t, ok)
	require.Equal(t, in.Subject, out.Subject)
}

func TestCtx_AbsentClaims(t *testing.T) {
	t.Parallel()
	_, ok := ClaimsFrom(context.Background())
	require.False(t, ok)
}

func TestIdentity_HasTenantID_Field(t *testing.T) {
	t.Parallel()
	id := Identity{
		Username: "alice",
		Method:   "local",
		TenantID: "acme",
		Groups:   []string{"ops"},
	}
	require.Equal(t, "acme", id.TenantID)
}

func TestLocalProvider_Authenticate_CarriesTenantID(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":      "alice",
		"method":    LocalMethod,
		"enabled":   true,
		"password":  mustBcrypt("secret"),
		"tenant_id": "acme",
	})
	p := NewLocalProvider(fdb)
	ctx := WithTenant(context.Background(), "acme")
	id, err := p.Authenticate(ctx, Credentials{Username: "alice", Password: "secret"})
	require.NoError(t, err)
	require.Equal(t, "acme", id.TenantID)
}

func TestAnonymousProvider_Authenticate_CarriesTenantID(t *testing.T) {
	t.Parallel()
	p := NewAnonymousProvider(true)
	ctx := WithTenant(context.Background(), "default")
	id, err := p.Authenticate(ctx, Credentials{})
	require.NoError(t, err)
	require.Equal(t, "default", id.TenantID)
}
