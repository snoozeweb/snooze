package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// verifiedToken is the minimal result of ID-token verification the provider
// needs. It decouples OIDCProvider from *oidc.IDToken so tests can inject a
// fake verifier without a live IdP.
type verifiedToken struct {
	Nonce  string
	Claims map[string]any
}

// idTokenVerifier verifies a raw ID token (signature/iss/aud/exp) and returns
// the nonce + full claim set. Production wraps *oidc.IDTokenVerifier.
type idTokenVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (*verifiedToken, error)
}

// tokenExchanger exchanges an authorization code for tokens. *oauth2.Config
// satisfies it; tests inject a fake.
type tokenExchanger interface {
	Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error)
}

// OIDCProvider is a generic OpenID Connect identity provider (configured for
// Microsoft Entra by default). It is a RedirectProvider: it never authenticates
// a password. Discovery + verifier construction is lazy and cached so boot does
// not fail if the IdP is briefly unreachable.
type OIDCProvider struct {
	cfg schema.OIDC

	mu        sync.Mutex
	inited    bool
	oauth     *oauth2.Config
	exchanger tokenExchanger
	verifier  idTokenVerifier
}

// NewOIDCProvider returns a provider that initialises lazily on first use.
func NewOIDCProvider(cfg schema.OIDC) *OIDCProvider { return &OIDCProvider{cfg: cfg} }

// Name returns the configured method (default "microsoft").
func (p *OIDCProvider) Name() string { return p.cfg.Method }

// IsEnabled implements auth.EnableChecker.
func (p *OIDCProvider) IsEnabled(_ context.Context) bool { return p.cfg.Enabled }

// DisplayName / Icon implement RedirectProvider.
func (p *OIDCProvider) DisplayName() string { return p.cfg.DisplayName }
func (p *OIDCProvider) Icon() string        { return p.cfg.Icon }

// Authenticate signals callers to use the redirect flow.
func (p *OIDCProvider) Authenticate(context.Context, Credentials) (Identity, error) {
	return Identity{}, ErrRedirectProvider
}

// ensureInit performs OIDC discovery and builds the oauth2 config + verifier
// once. It deliberately holds p.mu across the oidc.NewProvider network call so
// concurrent first-callers serialise on a single discovery. sync.Once is NOT
// used because we want a failed discovery (IdP briefly unreachable at boot) to
// be retried on the next call rather than cached as a permanent failure; the
// inited bool gates that. Fields set here are written exactly once under the
// lock before inited=true, so the post-init lock-free reads in AuthCodeURL /
// ExchangeAndVerify are safe.
func (p *OIDCProvider) ensureInit(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inited {
		return nil
	}
	prov, err := oidc.NewProvider(ctx, p.cfg.Issuer)
	if err != nil {
		return fmt.Errorf("oidc discovery (%s): %w", p.cfg.Issuer, err)
	}
	p.oauth = &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		RedirectURL:  p.cfg.RedirectURL,
		Endpoint:     prov.Endpoint(),
		Scopes:       p.cfg.Scopes,
	}
	p.exchanger = p.oauth
	p.verifier = &oidcVerifier{v: prov.Verifier(&oidc.Config{ClientID: p.cfg.ClientID})}
	p.inited = true
	return nil
}

// AuthCodeURL implements RedirectProvider.
func (p *OIDCProvider) AuthCodeURL(ctx context.Context, state, nonce, pkceVerifier string) (string, error) {
	if err := p.ensureInit(ctx); err != nil {
		return "", err
	}
	return p.oauth.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(pkceVerifier),
	), nil
}

// ExchangeAndVerify implements RedirectProvider.
func (p *OIDCProvider) ExchangeAndVerify(ctx context.Context, code, nonce, pkceVerifier string) (Identity, error) {
	if err := p.ensureInit(ctx); err != nil {
		return Identity{}, err
	}
	tok, err := p.exchanger.Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return Identity{}, fmt.Errorf("oidc: code exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return Identity{}, errors.New("oidc: no id_token in token response")
	}
	vt, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return Identity{}, fmt.Errorf("oidc: verify id_token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(vt.Nonce), []byte(nonce)) != 1 {
		return Identity{}, errors.New("oidc: nonce mismatch")
	}
	return p.identityFromClaims(ctx, vt.Claims), nil
}

// identityFromClaims maps verified claims to an Identity. Username precedence:
// preferred_username -> email -> sub. Groups = dedup union of the configured
// roles_claim and groups_claim slices.
func (p *OIDCProvider) identityFromClaims(ctx context.Context, m map[string]any) Identity {
	username := firstClaimString(m, "preferred_username", "email", "sub")
	set := map[string]struct{}{}
	for _, g := range claimStringSlice(m[p.cfg.RolesClaim]) {
		set[g] = struct{}{}
	}
	for _, g := range claimStringSlice(m[p.cfg.GroupsClaim]) {
		set[g] = struct{}{}
	}
	groups := make([]string, 0, len(set))
	for g := range set {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	tenantID, _ := TenantFrom(ctx)
	return Identity{
		Username: username,
		Method:   p.cfg.Method,
		TenantID: tenantID,
		Groups:   groups,
	}
}

// oidcVerifier wraps *oidc.IDTokenVerifier to satisfy idTokenVerifier.
type oidcVerifier struct{ v *oidc.IDTokenVerifier }

func (o *oidcVerifier) Verify(ctx context.Context, raw string) (*verifiedToken, error) {
	tok, err := o.v.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := tok.Claims(&m); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	return &verifiedToken{Nonce: tok.Nonce, Claims: m}, nil
}

// firstClaimString returns the first non-empty string claim among keys.
func firstClaimString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// claimStringSlice coerces a claim value to []string, tolerating []any / []string.
func claimStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t != "" {
			return []string{t}
		}
	}
	return nil
}

// compile-time assertion that OIDCProvider satisfies the interfaces.
var (
	_ Provider         = (*OIDCProvider)(nil)
	_ EnableChecker    = (*OIDCProvider)(nil)
	_ RedirectProvider = (*OIDCProvider)(nil)
)
