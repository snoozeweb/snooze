package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sort"
	"strings"
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

// OIDCConfigSource returns the current OIDC configuration. The production
// implementation reads RuntimeSettings.OIDC (DB-backed, live-editable) layered
// on the file-config baseline. The client_secret and method are deliberately
// never taken from the DB. A nil source makes the provider read its static
// baseline (and pins the cached discovery, which the unit tests rely on).
type OIDCConfigSource func(ctx context.Context) (schema.OIDC, error)

// OIDCProvider is a generic OpenID Connect identity provider (configured for
// Microsoft Entra by default). It is a RedirectProvider: it never authenticates
// a password. Discovery + verifier construction is lazy and cached, keyed by a
// config signature so a live edit (issuer/client/scopes/secret/redirect) is
// picked up on the next use without a restart.
type OIDCProvider struct {
	cfg    schema.OIDC      // file-config baseline: method/display_name/icon/client_secret
	source OIDCConfigSource // live config: enabled/issuer/client_id/redirect_url/scopes/claims

	mu        sync.Mutex
	inited    bool
	cachedSig string // signature of the config the cached oauth/verifier were built from
	oauth     *oauth2.Config
	exchanger tokenExchanger
	verifier  idTokenVerifier
}

// NewOIDCProvider returns a provider that initialises lazily on first use.
// baseline carries the file-config values (method/display_name/icon/client_secret);
// source supplies the live, DB-overridable fields. A nil source falls back to
// the baseline for everything.
func NewOIDCProvider(baseline schema.OIDC, source OIDCConfigSource) *OIDCProvider {
	return &OIDCProvider{cfg: baseline, source: source}
}

// effective returns the live config: source(ctx) when wired, else the baseline.
// On a source error it falls back to the baseline so a transient settings-read
// failure never hard-breaks sign-in.
func (p *OIDCProvider) effective(ctx context.Context) schema.OIDC {
	if p.source == nil {
		return p.cfg
	}
	cfg, err := p.source(ctx)
	if err != nil {
		return p.cfg
	}
	return cfg
}

// Name returns the configured method (file-config; default "microsoft"). It is
// the login URL segment + JWT method claim, so it is deliberately NOT runtime-
// editable — changing it would orphan provisioned users and break the callback.
func (p *OIDCProvider) Name() string { return p.cfg.Method }

// IsEnabled implements auth.EnableChecker, reading the live config.
func (p *OIDCProvider) IsEnabled(ctx context.Context) bool { return p.effective(ctx).Enabled }

// DisplayName implements RedirectProvider; it is the login button label.
func (p *OIDCProvider) DisplayName() string { return p.cfg.DisplayName }

// Icon implements RedirectProvider; it is the login button icon key.
func (p *OIDCProvider) Icon() string { return p.cfg.Icon }

// Authenticate signals callers to use the redirect flow.
func (p *OIDCProvider) Authenticate(context.Context, Credentials) (Identity, error) {
	return Identity{}, ErrRedirectProvider
}

// oidcInit is the discovery-derived snapshot the request methods use. It is
// captured under the lock so a concurrent rebuild (after a live config change)
// never races the readers.
type oidcInit struct {
	oauth     *oauth2.Config
	exchanger tokenExchanger
	verifier  idTokenVerifier
}

// oidcConfigSignature fingerprints the fields that feed OIDC discovery + the
// oauth2/verifier setup. When it changes (operator edited issuer/client_id/
// scopes/redirect/secret) the cached discovery is rebuilt on the next use.
func oidcConfigSignature(c schema.OIDC) string {
	parts := append([]string{c.Issuer, c.ClientID, c.ClientSecret, c.RedirectURL}, c.Scopes...)
	return strings.Join(parts, "\x00")
}

// ensureInit performs OIDC discovery + builds the oauth2 config + verifier,
// caching them keyed by the config signature so a live config change triggers a
// rebuild on the next use. It returns the cached snapshot and the effective
// config. It deliberately holds p.mu across the oidc.NewProvider network call so
// concurrent callers serialise on a single discovery. A test-injected provider
// (nil source, inited pinned true) short-circuits with no network discovery.
func (p *OIDCProvider) ensureInit(ctx context.Context) (oidcInit, schema.OIDC, error) {
	cfg := p.effective(ctx)
	sig := oidcConfigSignature(cfg)
	p.mu.Lock()
	defer p.mu.Unlock()
	// Rebuild when never inited, or (for a live source) when the config that the
	// cache was built from has changed. A test-injected provider has no source
	// and stays pinned to its hand-built oauth/verifier (no network discovery).
	needsInit := !p.inited || (p.source != nil && p.cachedSig != sig)
	if needsInit {
		prov, err := oidc.NewProvider(ctx, cfg.Issuer)
		if err != nil {
			return oidcInit{}, schema.OIDC{}, fmt.Errorf("oidc discovery (%s): %w", cfg.Issuer, err)
		}
		p.oauth = &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     prov.Endpoint(),
			Scopes:       cfg.Scopes,
		}
		p.exchanger = p.oauth
		p.verifier = &oidcVerifier{v: prov.Verifier(&oidc.Config{ClientID: cfg.ClientID})}
		p.inited = true
		p.cachedSig = sig
	}
	return oidcInit{oauth: p.oauth, exchanger: p.exchanger, verifier: p.verifier}, cfg, nil
}

// AuthCodeURL implements RedirectProvider.
func (p *OIDCProvider) AuthCodeURL(ctx context.Context, state, nonce, pkceVerifier string) (string, error) {
	init, _, err := p.ensureInit(ctx)
	if err != nil {
		return "", err
	}
	return init.oauth.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(pkceVerifier),
	), nil
}

// ExchangeAndVerify implements RedirectProvider.
func (p *OIDCProvider) ExchangeAndVerify(ctx context.Context, code, nonce, pkceVerifier string) (Identity, error) {
	init, cfg, err := p.ensureInit(ctx)
	if err != nil {
		return Identity{}, err
	}
	tok, err := init.exchanger.Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return Identity{}, fmt.Errorf("oidc: code exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return Identity{}, errors.New("oidc: no id_token in token response")
	}
	vt, err := init.verifier.Verify(ctx, rawID)
	if err != nil {
		return Identity{}, fmt.Errorf("oidc: verify id_token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(vt.Nonce), []byte(nonce)) != 1 {
		return Identity{}, errors.New("oidc: nonce mismatch")
	}
	return p.identityFromClaims(ctx, cfg, vt.Claims), nil
}

// identityFromClaims maps verified claims to an Identity using cfg's claim
// names. Username precedence: preferred_username -> email -> sub. Groups =
// dedup union of the configured roles_claim and groups_claim slices.
func (p *OIDCProvider) identityFromClaims(ctx context.Context, cfg schema.OIDC, m map[string]any) Identity {
	username := firstClaimString(m, "preferred_username", "email", "sub")
	set := map[string]struct{}{}
	for _, g := range claimStringSlice(m[cfg.RolesClaim]) {
		set[g] = struct{}{}
	}
	for _, g := range claimStringSlice(m[cfg.GroupsClaim]) {
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
		Method:   cfg.Method,
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
