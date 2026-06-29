package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// fakeAssertionParser is an assertionParser seam stub: it returns a canned
// parsedAssertion (or error) so the mapping tests never touch XML-DSig or a
// live IdP.
type fakeAssertionParser struct {
	res *parsedAssertion
	err error
}

func (f *fakeAssertionParser) Parse(_ context.Context, _ string) (*parsedAssertion, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

// newTestSAMLProvider builds a SAMLProvider wired to a fake parser, so
// ParseAssertion exercises identityFromAssertion without any crypto.
func newTestSAMLProvider(cfg schema.SAML, parser assertionParser) *SAMLBackend {
	p := NewSAMLProvider(cfg)
	p.parser = parser
	p.inited = true // pin: never build a real ServiceProvider in unit tests
	return p
}

func TestSAMLIdentityFromAssertion(t *testing.T) {
	cfg := schema.SAML{
		Method:          "saml",
		GroupsAttribute: "groups",
		RolesAttribute:  "Role",
	}
	parser := &fakeAssertionParser{res: &parsedAssertion{
		NameID: "alice@egerie.eu",
		Attributes: map[string][]string{
			"groups": {"ops", "viewers", "ops"}, // duplicate within attr
			"Role":   {"admin", "ops"},          // "ops" overlaps with groups
		},
	}}
	p := newTestSAMLProvider(cfg, parser)

	ctx := WithTenant(context.Background(), "acme")
	id, err := p.ParseAssertion(ctx, "<ignored/>")
	require.NoError(t, err)
	require.Equal(t, "alice@egerie.eu", id.Username)
	require.Equal(t, "saml", id.Method)
	require.Equal(t, "acme", id.TenantID)
	// Union of groups+roles attrs, deduped + sorted.
	require.Equal(t, []string{"admin", "ops", "viewers"}, id.Groups)
}

func TestSAMLIdentityFromAssertion_GroupsOnly(t *testing.T) {
	cfg := schema.SAML{Method: "saml", GroupsAttribute: "groups"} // no RolesAttribute
	parser := &fakeAssertionParser{res: &parsedAssertion{
		NameID:     "bob@egerie.eu",
		Attributes: map[string][]string{"groups": {"team-b", "team-a"}},
	}}
	p := newTestSAMLProvider(cfg, parser)

	id, err := p.ParseAssertion(context.Background(), "<ignored/>")
	require.NoError(t, err)
	require.Equal(t, "bob@egerie.eu", id.Username)
	require.Equal(t, []string{"team-a", "team-b"}, id.Groups)
}

func TestSAMLNameIDEmpty(t *testing.T) {
	cfg := schema.SAML{Method: "saml", GroupsAttribute: "groups"}
	parser := &fakeAssertionParser{res: &parsedAssertion{NameID: ""}}
	p := newTestSAMLProvider(cfg, parser)

	_, err := p.ParseAssertion(context.Background(), "<ignored/>")
	require.Error(t, err)
}

func TestSAMLParseAssertion_ParserError(t *testing.T) {
	cfg := schema.SAML{Method: "saml", GroupsAttribute: "groups"}
	parser := &fakeAssertionParser{err: errors.New("bad signature")}
	p := newTestSAMLProvider(cfg, parser)

	_, err := p.ParseAssertion(context.Background(), "<ignored/>")
	require.Error(t, err)
}

func TestSAMLProviderInterface(t *testing.T) {
	cfg := schema.SAML{Method: "saml"}
	p := newTestSAMLProvider(cfg, &fakeAssertionParser{})

	// Provider basics.
	require.Equal(t, "saml", p.Name())
	require.Equal(t, "SAML", func() string { p.cfg.DisplayName = "SAML"; return p.DisplayName() }())

	// Authenticate signals the redirect flow.
	_, err := p.Authenticate(context.Background(), Credentials{})
	require.True(t, errors.Is(err, ErrRedirectProvider))

	// Registry round-trip returns the same provider.
	reg := NewRegistry()
	reg.Register(p)
	got, err := reg.Get("saml")
	require.NoError(t, err)
	_, err = got.Authenticate(context.Background(), Credentials{})
	require.True(t, errors.Is(err, ErrRedirectProvider))
}
