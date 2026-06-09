package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOIDCState_RoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	in := oidcState{State: "s1", Nonce: "n1", Verifier: "v1", ReturnTo: "/web/alerts", Org: "acme", Exp: 9999999999}
	raw := encodeOIDCState(key, in)
	out, err := decodeOIDCState(key, raw)
	require.NoError(t, err)
	require.Equal(t, in.State, out.State)
	require.Equal(t, in.Nonce, out.Nonce)
	require.Equal(t, in.Verifier, out.Verifier)
	require.Equal(t, in.ReturnTo, out.ReturnTo)
	require.Equal(t, in.Org, out.Org)
}

func TestOIDCState_TamperRejected(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	raw := encodeOIDCState(key, oidcState{State: "s1", Exp: 9999999999})
	_, err := decodeOIDCState(key, raw+"x")
	require.Error(t, err)
	_, err = decodeOIDCState([]byte("DIFFERENT_KEY_DIFFERENT_KEY_1234"), raw)
	require.Error(t, err)
}

func TestOIDCState_Expired(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	raw := encodeOIDCState(key, oidcState{State: "s1", Exp: 1}) // 1970
	_, err := decodeOIDCState(key, raw)
	require.Error(t, err)
}

func TestRandURLToken(t *testing.T) {
	a, err := randURLToken(24)
	require.NoError(t, err)
	b, err := randURLToken(24)
	require.NoError(t, err)
	require.NotEmpty(t, a)
	require.NotEqual(t, a, b)
}
