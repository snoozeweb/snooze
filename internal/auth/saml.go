package auth

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// parsedAssertion is the minimal, crypto-verified result the provider needs
// from an inbound SAMLResponse. It decouples SAMLProvider from *saml.Assertion
// so tests can inject a parser that never touches XML-DSig or a live IdP
// (mirrors oidc.go's verifiedToken).
type parsedAssertion struct {
	// NameID is the Subject NameID value (-> Identity.Username).
	NameID string
	// Attributes maps each assertion attribute Name to its string values.
	Attributes map[string][]string
}

// assertionParser validates a base64 SAMLResponse (signature, audience,
// conditions, NotOnOrAfter) and reduces it to a parsedAssertion. Production
// wraps *saml.ServiceProvider.ParseXMLResponse; tests inject a fake.
type assertionParser interface {
	Parse(ctx context.Context, samlResponse string) (*parsedAssertion, error)
}

// SAMLBackend is a SAML2 SP-initiated identity provider (it satisfies the
// auth.SAMLProvider interface). Unlike OIDC it is not a RedirectProvider (the
// OAuth-code contract does not fit SAML's POST-back ACS). Construction of the
// underlying *saml.ServiceProvider (loading the SP key/cert and the IdP
// metadata) is lazy and cached behind the config so the first /start or /acs
// request pays the cost, keeping NewSAMLProvider allocation-cheap and
// import-only.
type SAMLBackend struct {
	cfg schema.SAML

	mu     sync.Mutex
	inited bool
	sp     *saml.ServiceProvider // built lazily from cfg
	parser assertionParser       // production wraps sp.ParseXMLResponse; tests inject a fake
}

// NewSAMLProvider returns a provider that initialises its ServiceProvider
// lazily on first use.
func NewSAMLProvider(cfg schema.SAML) *SAMLBackend {
	return &SAMLBackend{cfg: cfg}
}

// Name returns the configured method (file-config; default "saml"). It is the
// login URL segment + JWT method claim, so it is deliberately fixed — changing
// it would orphan provisioned users and break the ACS route.
func (p *SAMLBackend) Name() string { return p.cfg.Method }

// IsEnabled implements auth.EnableChecker, reading the file config.
func (p *SAMLBackend) IsEnabled(context.Context) bool { return p.cfg.Enabled }

// DisplayName implements SAMLProvider; it is the login button label.
func (p *SAMLBackend) DisplayName() string { return p.cfg.DisplayName }

// Icon implements SAMLProvider; it is the login button icon key.
func (p *SAMLBackend) Icon() string { return p.cfg.Icon }

// Authenticate signals callers to use the redirect (SAML) flow.
func (p *SAMLBackend) Authenticate(context.Context, Credentials) (Identity, error) {
	return Identity{}, ErrRedirectProvider
}

// ensureInit builds the *saml.ServiceProvider (SP key/cert + IdP metadata) once
// and caches it. A test-injected provider (inited pinned true, parser set) is
// short-circuited so no key file is read and no IdP metadata is fetched.
func (p *SAMLBackend) ensureInit(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inited {
		return nil
	}
	sp, err := p.buildServiceProvider(ctx)
	if err != nil {
		return err
	}
	p.sp = sp
	p.parser = &spAssertionParser{sp: sp}
	p.inited = true
	return nil
}

// buildServiceProvider assembles a *saml.ServiceProvider from the file config:
// it parses the absolute ACS/metadata URLs, optionally loads the SP signing
// key/cert, and loads the IdP metadata (remote URL or inline XML/file).
func (p *SAMLBackend) buildServiceProvider(ctx context.Context) (*saml.ServiceProvider, error) {
	if p.cfg.ACSURL == "" {
		return nil, errors.New("saml: acs_url is required")
	}
	acsURL, err := url.Parse(p.cfg.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("saml: parse acs_url: %w", err)
	}
	// MetadataURL defaults to the configured EntityID (or the ACS URL) so the
	// published EntityDescriptor carries a stable entityID.
	metaRaw := p.cfg.EntityID
	if metaRaw == "" {
		metaRaw = p.cfg.ACSURL
	}
	metaURL, err := url.Parse(metaRaw)
	if err != nil {
		return nil, fmt.Errorf("saml: parse entity_id: %w", err)
	}

	idpMeta, err := p.loadIDPMetadata(ctx)
	if err != nil {
		return nil, err
	}

	opts := samlsp.Options{
		EntityID:    p.cfg.EntityID,
		URL:         *acsURL,
		IDPMetadata: idpMeta,
		SignRequest: p.cfg.SignRequests,
	}
	if p.cfg.SPKeyFile != "" || p.cfg.SPCertFile != "" {
		key, cert, err := loadKeyPair(p.cfg.SPKeyFile, p.cfg.SPCertFile)
		if err != nil {
			return nil, err
		}
		opts.Key = key
		opts.Certificate = cert
	}

	sp := samlsp.DefaultServiceProvider(opts)
	// DefaultServiceProvider derives ACS/metadata URLs from URL by appending
	// "saml/acs" etc.; override with our explicit, IdP-registered values.
	sp.AcsURL = *acsURL
	sp.MetadataURL = *metaURL
	sp.AllowIDPInitiated = p.cfg.AllowUnsolicited
	return &sp, nil
}

// loadIDPMetadata fetches the IdP EntityDescriptor from the remote URL, or
// parses inline metadata (a file path or literal XML).
func (p *SAMLBackend) loadIDPMetadata(ctx context.Context) (*saml.EntityDescriptor, error) {
	switch {
	case p.cfg.IDPMetadataURL != "":
		u, err := url.Parse(p.cfg.IDPMetadataURL)
		if err != nil {
			return nil, fmt.Errorf("saml: parse idp_metadata_url: %w", err)
		}
		meta, err := samlsp.FetchMetadata(ctx, http.DefaultClient, *u)
		if err != nil {
			return nil, fmt.Errorf("saml: fetch idp metadata: %w", err)
		}
		return meta, nil
	case p.cfg.IDPMetadataXML != "":
		data := []byte(p.cfg.IDPMetadataXML)
		// Treat a value that is not XML as a file path.
		if !strings.Contains(p.cfg.IDPMetadataXML, "<") {
			b, err := os.ReadFile(p.cfg.IDPMetadataXML)
			if err != nil {
				return nil, fmt.Errorf("saml: read idp_metadata_xml file: %w", err)
			}
			data = b
		}
		meta, err := samlsp.ParseMetadata(data)
		if err != nil {
			return nil, fmt.Errorf("saml: parse idp metadata xml: %w", err)
		}
		return meta, nil
	default:
		return nil, errors.New("saml: no IdP metadata source configured")
	}
}

// AuthnRequestURL builds the SP-initiated HTTP-Redirect URL to the IdP SSO
// endpoint, embedding relayState. Implements auth.SAMLProvider.
func (p *SAMLBackend) AuthnRequestURL(ctx context.Context, relayState string) (string, error) {
	if err := p.ensureInit(ctx); err != nil {
		return "", err
	}
	u, err := p.sp.MakeRedirectAuthenticationRequest(relayState)
	if err != nil {
		return "", fmt.Errorf("saml: build authn request: %w", err)
	}
	return u.String(), nil
}

// ParseAssertion validates the POSTed (base64) SAMLResponse via the parser seam
// and maps it to an Identity. Implements auth.SAMLProvider.
func (p *SAMLBackend) ParseAssertion(ctx context.Context, samlResponse string) (Identity, error) {
	if err := p.ensureInit(ctx); err != nil {
		return Identity{}, err
	}
	pa, err := p.parser.Parse(ctx, samlResponse)
	if err != nil {
		return Identity{}, fmt.Errorf("saml: parse assertion: %w", err)
	}
	return p.identityFromAssertion(ctx, pa)
}

// identityFromAssertion maps a verified assertion to an Identity: NameID ->
// Username, and the dedup+sorted union of the configured groups + roles
// attributes -> Groups. It stamps Method and the tenant from ctx. An empty
// NameID is an error (a SAML user must have a stable subject).
func (p *SAMLBackend) identityFromAssertion(ctx context.Context, pa *parsedAssertion) (Identity, error) {
	if pa == nil || pa.NameID == "" {
		return Identity{}, errors.New("saml: assertion has no NameID")
	}
	set := map[string]struct{}{}
	for _, g := range pa.Attributes[p.cfg.GroupsAttribute] {
		if g != "" {
			set[g] = struct{}{}
		}
	}
	if p.cfg.RolesAttribute != "" {
		for _, g := range pa.Attributes[p.cfg.RolesAttribute] {
			if g != "" {
				set[g] = struct{}{}
			}
		}
	}
	groups := make([]string, 0, len(set))
	for g := range set {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	tenantID, _ := TenantFrom(ctx)
	// TODO(plan-29): populate Identity.Email from the assertion for attribute-
	// based tenant resolution (domain match). The SAML config has no configurable
	// email attribute name yet (schema.SAML carries only groups/roles attributes);
	// adding one is out of scope here. Until then, domain-type tenant_match rules
	// do not fire for SAML logins — a documented limitation. Group- and login-type
	// rules work unchanged.
	return Identity{
		Username: pa.NameID,
		Method:   p.cfg.Method,
		TenantID: tenantID,
		Groups:   groups,
	}, nil
}

// Metadata returns the SP EntityDescriptor XML. Implements auth.SAMLProvider.
func (p *SAMLBackend) Metadata(ctx context.Context) ([]byte, error) {
	if err := p.ensureInit(ctx); err != nil {
		return nil, err
	}
	md := p.sp.Metadata()
	out, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("saml: marshal metadata: %w", err)
	}
	return out, nil
}

// spAssertionParser is the production assertionParser: it wraps the crewjam
// ServiceProvider, which owns the XML-DSig signature, audience, condition and
// NotOnOrAfter verification (saml.MaxClockSkew governs skew).
type spAssertionParser struct {
	sp *saml.ServiceProvider
}

// Parse base64-decodes the form SAMLResponse, hands it to ParseXMLResponse for
// full crypto verification, and reduces the verified assertion to the fields
// the provider maps. possibleRequestIDs is empty: the RelayState cookie (an
// HMAC-signed CSRF token) is the SP-initiated tie-back, checked at the ACS
// handler, and AllowIDPInitiated covers the unsolicited case.
func (s *spAssertionParser) Parse(_ context.Context, samlResponse string) (*parsedAssertion, error) {
	decoded, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		return nil, fmt.Errorf("decode SAMLResponse: %w", err)
	}
	assertion, err := s.sp.ParseXMLResponse(decoded, []string{}, s.sp.AcsURL)
	if err != nil {
		return nil, err
	}
	return reduceAssertion(assertion), nil
}

// reduceAssertion flattens a verified *saml.Assertion to NameID + attributes.
func reduceAssertion(a *saml.Assertion) *parsedAssertion {
	pa := &parsedAssertion{Attributes: map[string][]string{}}
	if a == nil {
		return pa
	}
	if a.Subject != nil && a.Subject.NameID != nil {
		pa.NameID = a.Subject.NameID.Value
	}
	for _, stmt := range a.AttributeStatements {
		for _, attr := range stmt.Attributes {
			for _, v := range attr.Values {
				if v.Value != "" {
					pa.Attributes[attr.Name] = append(pa.Attributes[attr.Name], v.Value)
				}
			}
		}
	}
	return pa
}

// loadKeyPair loads the SP private key + certificate from PEM files.
func loadKeyPair(keyFile, certFile string) (*rsa.PrivateKey, *x509.Certificate, error) {
	if keyFile == "" || certFile == "" {
		return nil, nil, errors.New("saml: both sp_key_file and sp_cert_file are required when one is set")
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("saml: load sp key pair: %w", err)
	}
	key, ok := pair.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("saml: sp_key_file must be an RSA private key")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, fmt.Errorf("saml: parse sp certificate: %w", err)
	}
	return key, cert, nil
}

// Clock skew is owned entirely by the crewjam library (the package-level
// saml.MaxClockSkew / saml.MaxIssueDelay vars govern NotOnOrAfter and replay).
// Snooze provider code never calls time.Now in the SAML core paths, keeping it
// clock-clean and the crypto-time decisions in one place.

// compile-time assertions that SAMLBackend satisfies the interfaces.
var (
	_ Provider      = (*SAMLBackend)(nil)
	_ EnableChecker = (*SAMLBackend)(nil)
	_ SAMLProvider  = (*SAMLBackend)(nil)
)
