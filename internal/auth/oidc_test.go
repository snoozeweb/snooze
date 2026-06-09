package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// fakeVerifier returns a canned verifiedToken (or error) for tests.
type fakeVerifier struct {
	tok *verifiedToken
	err error
}

func (f *fakeVerifier) Verify(_ context.Context, _ string) (*verifiedToken, error) {
	return f.tok, f.err
}

// fakeExchanger returns a canned oauth2.Token carrying an id_token extra.
type fakeExchanger struct {
	idToken string
	err     error
}

func (f *fakeExchanger) Exchange(_ context.Context, _ string, _ ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	if f.err != nil {
		return nil, f.err
	}
	t := &oauth2.Token{AccessToken: "at"}
	return t.WithExtra(map[string]any{"id_token": f.idToken}), nil
}

func testOIDCCfg() schema.OIDC {
	c := schema.DefaultOIDC()
	c.Enabled = true
	c.ClientID = "cid"
	c.RedirectURL = "https://snooze/api/v1/login/microsoft/callback"
	return c
}

func TestOIDCProvider_IdentityFromClaims(t *testing.T) {
	p := &OIDCProvider{cfg: testOIDCCfg()}
	id := p.identityFromClaims(context.Background(), map[string]any{
		"sub":                "abc",
		"preferred_username": "alice@egerie.eu",
		"email":              "alice@egerie.eu",
		"roles":              []any{"Admin", "Editor"},
		"groups":             []any{"team-sre"},
	})
	require.Equal(t, "alice@egerie.eu", id.Username)
	require.Equal(t, "microsoft", id.Method)
	require.ElementsMatch(t, []string{"Admin", "Editor", "team-sre"}, id.Groups)
}

func TestOIDCProvider_UsernameFallback(t *testing.T) {
	p := &OIDCProvider{cfg: testOIDCCfg()}
	id := p.identityFromClaims(context.Background(), map[string]any{"sub": "only-sub"})
	require.Equal(t, "only-sub", id.Username)
}

func TestOIDCProvider_ExchangeAndVerify_Success(t *testing.T) {
	p := &OIDCProvider{
		cfg:       testOIDCCfg(),
		inited:    true,
		exchanger: &fakeExchanger{idToken: "raw"},
		verifier: &fakeVerifier{tok: &verifiedToken{
			Nonce:  "n1",
			Claims: map[string]any{"sub": "abc", "roles": []any{"Admin"}},
		}},
	}
	id, err := p.ExchangeAndVerify(context.Background(), "code", "n1", "verifier")
	require.NoError(t, err)
	require.Equal(t, "abc", id.Username)
	require.Equal(t, []string{"Admin"}, id.Groups)
}

func TestOIDCProvider_ExchangeAndVerify_NonceMismatch(t *testing.T) {
	p := &OIDCProvider{
		cfg:       testOIDCCfg(),
		inited:    true,
		exchanger: &fakeExchanger{idToken: "raw"},
		verifier:  &fakeVerifier{tok: &verifiedToken{Nonce: "WRONG", Claims: map[string]any{"sub": "x"}}},
	}
	_, err := p.ExchangeAndVerify(context.Background(), "code", "n1", "v")
	require.Error(t, err)
}

func TestOIDCProvider_ExchangeAndVerify_VerifyError(t *testing.T) {
	p := &OIDCProvider{
		cfg:       testOIDCCfg(),
		inited:    true,
		exchanger: &fakeExchanger{idToken: "raw"},
		verifier:  &fakeVerifier{err: errors.New("bad signature")},
	}
	_, err := p.ExchangeAndVerify(context.Background(), "code", "n1", "v")
	require.Error(t, err)
}

func TestOIDCProvider_AuthCodeURL(t *testing.T) {
	p := &OIDCProvider{
		cfg:    testOIDCCfg(),
		inited: true,
		oauth: &oauth2.Config{
			ClientID:    "cid",
			RedirectURL: "https://snooze/api/v1/login/microsoft/callback",
			Endpoint:    oauth2.Endpoint{AuthURL: "https://login.example/authorize"},
			Scopes:      []string{"openid", "profile", "email"},
		},
	}
	u, err := p.AuthCodeURL(context.Background(), "state123", "nonce123", "pkce-verifier-string-1234567890")
	require.NoError(t, err)
	for _, want := range []string{"state=state123", "nonce=nonce123", "code_challenge=", "code_challenge_method=S256", "client_id=cid"} {
		require.Contains(t, u, want)
	}
}

func TestOIDCProvider_ExchangeAndVerify_ExchangeError(t *testing.T) {
	p := &OIDCProvider{
		cfg:       testOIDCCfg(),
		inited:    true,
		exchanger: &fakeExchanger{err: errors.New("network down")},
		verifier:  &fakeVerifier{tok: &verifiedToken{Nonce: "n1"}},
	}
	_, err := p.ExchangeAndVerify(context.Background(), "code", "n1", "v")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exchange")
}

func TestOIDCProvider_ExchangeAndVerify_MissingIDToken(t *testing.T) {
	p := &OIDCProvider{
		cfg:       testOIDCCfg(),
		inited:    true,
		exchanger: &fakeExchanger{idToken: ""}, // token response carries no id_token
		verifier:  &fakeVerifier{tok: &verifiedToken{Nonce: "n1"}},
	}
	_, err := p.ExchangeAndVerify(context.Background(), "code", "n1", "v")
	require.Error(t, err)
	require.Contains(t, err.Error(), "id_token")
}
