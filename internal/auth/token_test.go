package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/config/schema"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// testSecret returns a 32-byte key suitable for HS256.
func testSecret() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func testCfg() schema.Auth {
	cfg := schema.DefaultAuth()
	cfg.TokenSecret = string(testSecret())
	return cfg
}

func TestNewTokenEngine_RejectsShortSecret(t *testing.T) {
	t.Parallel()
	_, err := NewTokenEngine([]byte("too-short"), testCfg())
	require.Error(t, err)
	require.Contains(t, err.Error(), "32 bytes")
}

func TestNewTokenEngine_RejectsBadAlgorithm(t *testing.T) {
	t.Parallel()
	cfg := testCfg()
	cfg.TokenAlgorithm = "RS256"
	_, err := NewTokenEngine(testSecret(), cfg)
	require.Error(t, err)
}

func TestTokenEngine_SignVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	eng, err := NewTokenEngine(testSecret(), testCfg())
	require.NoError(t, err)

	in := snoozetypes.Claims{
		Subject:     "alice",
		Method:      "local",
		Roles:       []string{"admin"},
		Permissions: []string{"rw_all"},
		Groups:      []string{"ops"},
	}
	tok, exp, err := eng.Sign(in)
	require.NoError(t, err)
	require.NotEmpty(t, tok)
	require.WithinDuration(t, time.Now().Add(DefaultLease), exp, 5*time.Second)

	out, err := eng.Verify(tok)
	require.NoError(t, err)
	require.Equal(t, "alice", out.Subject)
	require.Equal(t, "local", out.Method)
	require.Equal(t, []string{"admin"}, out.Roles)
	require.Equal(t, []string{"rw_all"}, out.Permissions)
	require.Equal(t, []string{"ops"}, out.Groups)
	require.Equal(t, "snooze", out.Issuer)
	require.Equal(t, []string{"snooze"}, out.Audience)
	require.NotZero(t, out.ExpiresAt)
	require.NotZero(t, out.IssuedAt)
}

func TestTokenEngine_Verify_Expired(t *testing.T) {
	t.Parallel()
	eng, err := NewTokenEngine(testSecret(), testCfg())
	require.NoError(t, err)

	// Hand-mint a token whose exp is already in the past so we don't have to
	// sleep in-test. Same secret, same alg.
	past := time.Now().Add(-2 * time.Hour)
	claims := jwt.MapClaims{
		"sub": "bob",
		"iss": "snooze",
		"aud": "snooze",
		"exp": past.Add(time.Hour).Unix(), // 1h ago
		"nbf": past.Unix(),
		"iat": past.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(testSecret())
	require.NoError(t, err)

	_, err = eng.Verify(signed)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTokenExpired), "expected ErrTokenExpired, got %v", err)
}

func TestTokenEngine_Verify_SignatureMismatch(t *testing.T) {
	t.Parallel()
	eng1, err := NewTokenEngine(testSecret(), testCfg())
	require.NoError(t, err)
	other := []byte("ffffffffffffffffffffffffffffffff")
	eng2, err := NewTokenEngine(other, testCfg())
	require.NoError(t, err)

	tok, _, err := eng1.Sign(snoozetypes.Claims{Subject: "carol", Method: "local"})
	require.NoError(t, err)

	_, err = eng2.Verify(tok)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTokenSignature), "expected ErrTokenSignature, got %v", err)
}

func TestTokenEngine_Verify_Malformed(t *testing.T) {
	t.Parallel()
	eng, err := NewTokenEngine(testSecret(), testCfg())
	require.NoError(t, err)
	_, err = eng.Verify("definitely.not.a.jwt")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTokenInvalid), "expected ErrTokenInvalid, got %v", err)
}

func TestTokenEngine_Verify_RejectsNoneAlg(t *testing.T) {
	t.Parallel()
	eng, err := NewTokenEngine(testSecret(), testCfg())
	require.NoError(t, err)

	// Hand-craft a `none` token by signing with the unsafe none method.
	claims := jwt.MapClaims{
		"sub": "evil",
		"iss": "snooze",
		"aud": "snooze",
		"exp": time.Now().Add(time.Hour).Unix(),
		"nbf": time.Now().Unix(),
		"iat": time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = eng.Verify(signed)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrTokenExpired))
}

func TestTokenEngine_Verify_WrongIssuer(t *testing.T) {
	t.Parallel()
	eng, err := NewTokenEngine(testSecret(), testCfg())
	require.NoError(t, err)

	// Mint a token with a *different* issuer using the same secret to isolate
	// the issuer check.
	other := testCfg()
	other.TokenIssuer = "evil"
	otherEng, err := NewTokenEngine(testSecret(), other)
	require.NoError(t, err)
	tok, _, err := otherEng.Sign(snoozetypes.Claims{Subject: "dave", Method: "local"})
	require.NoError(t, err)

	_, err = eng.Verify(tok)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTokenInvalid), "expected ErrTokenInvalid, got %v", err)
}

func TestTokenEngine_Verify_WrongAudience(t *testing.T) {
	t.Parallel()
	eng, err := NewTokenEngine(testSecret(), testCfg())
	require.NoError(t, err)

	other := testCfg()
	other.TokenAudience = "different"
	otherEng, err := NewTokenEngine(testSecret(), other)
	require.NoError(t, err)
	tok, _, err := otherEng.Sign(snoozetypes.Claims{Subject: "eve", Method: "local"})
	require.NoError(t, err)

	_, err = eng.Verify(tok)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTokenInvalid))
	require.Contains(t, err.Error(), "audience")
}

func TestTokenEngine_RootClaims(t *testing.T) {
	t.Parallel()
	eng, err := NewTokenEngine(testSecret(), testCfg())
	require.NoError(t, err)
	c := eng.RootClaims()
	require.Equal(t, "root", c.Subject)
	require.Equal(t, "root", c.Method)
	require.Contains(t, c.Permissions, "rw_all")

	// Round-trip the root claims through Sign/Verify.
	tok, _, err := eng.Sign(c)
	require.NoError(t, err)
	got, err := eng.Verify(tok)
	require.NoError(t, err)
	require.Equal(t, "root", got.Subject)
	require.Equal(t, "root", got.Method)
	require.Contains(t, got.Permissions, "rw_all")
}

func TestTokenEngine_Verify_MultiAudienceMatches(t *testing.T) {
	t.Parallel()
	cfg := testCfg()
	cfg.TokenAudience = "a,b,c"
	eng, err := NewTokenEngine(testSecret(), cfg)
	require.NoError(t, err)
	tok, _, err := eng.Sign(snoozetypes.Claims{Subject: "frank", Method: "local"})
	require.NoError(t, err)
	require.True(t, strings.Count(tok, ".") == 2)

	out, err := eng.Verify(tok)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a", "b", "c"}, out.Audience)
}
