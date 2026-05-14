package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/pkg/snoozetypes"
)

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
