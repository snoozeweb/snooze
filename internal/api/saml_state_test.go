package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSAMLStateRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	in := samlState{
		State:    "rs-abc123",
		ReturnTo: "/web/alerts",
		Org:      "acme",
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	enc := encodeSAMLState(key, in)
	require.NotEmpty(t, enc)

	out, err := decodeSAMLState(key, enc)
	require.NoError(t, err)
	require.Equal(t, in.State, out.State)
	require.Equal(t, in.ReturnTo, out.ReturnTo)
	require.Equal(t, in.Org, out.Org)
	require.Equal(t, in.Exp, out.Exp)
}

func TestSAMLStateBadSig(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	enc := encodeSAMLState(key, samlState{State: "rs", Exp: time.Now().Add(time.Minute).Unix()})

	// A different key must reject the tag.
	_, err := decodeSAMLState([]byte("ffffffffffffffffffffffffffffffff"), enc)
	require.Error(t, err)

	// A tampered payload must reject too.
	_, err = decodeSAMLState(key, enc+"x")
	require.Error(t, err)

	// Malformed (no separator) must reject.
	_, err = decodeSAMLState(key, "not-a-cookie")
	require.Error(t, err)
}

func TestSAMLStateExpired(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	enc := encodeSAMLState(key, samlState{State: "rs", Exp: time.Now().Add(-time.Second).Unix()})
	_, err := decodeSAMLState(key, enc)
	require.Error(t, err)
}
