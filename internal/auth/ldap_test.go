package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/config/schema"
)

// fakeLDAP is an injectable ldapConn for unit testing the search-then-bind
// dance without a real directory.
type fakeLDAP struct {
	bindErrFn   func(user, pass string) error
	searchEntry *ldap.Entry
	searchErr   error
	binds       []bindCall
	closed      bool
}

type bindCall struct{ user, pass string }

func (f *fakeLDAP) Bind(user, pass string) error {
	f.binds = append(f.binds, bindCall{user, pass})
	if f.bindErrFn != nil {
		return f.bindErrFn(user, pass)
	}
	return nil
}
func (f *fakeLDAP) Search(_ *ldap.SearchRequest) (*ldap.SearchResult, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	if f.searchEntry == nil {
		return &ldap.SearchResult{}, nil
	}
	return &ldap.SearchResult{Entries: []*ldap.Entry{f.searchEntry}}, nil
}
func (f *fakeLDAP) Close() error { f.closed = true; return nil }

func cfgEnabled() schema.LDAP {
	c := schema.DefaultLDAP()
	c.Enabled = true
	c.Host = "ldap.example.com"
	c.BaseDN = "dc=example,dc=com"
	c.UserFilter = "(uid=%s)"
	c.BindDN = "cn=svc,dc=example,dc=com"
	c.BindPassword = "svc-pwd"
	c.GroupDN = "ou=groups,dc=example,dc=com"
	return c
}

// staticSource returns an LDAPConfigSource that always reports cfg. Tests
// that need to flip the live config mid-test use a closure-captured
// pointer instead.
func staticSource(cfg schema.LDAP) LDAPConfigSource {
	return func(context.Context) (schema.LDAP, error) { return cfg, nil }
}

func TestLDAPProvider_Disabled(t *testing.T) {
	t.Parallel()
	cfg := schema.DefaultLDAP() // not enabled
	p := NewLDAPProvider(staticSource(cfg))
	_, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "x"})
	require.True(t, errors.Is(err, ErrProviderDisabled))
}

func TestLDAPProvider_EmptyCreds(t *testing.T) {
	t.Parallel()
	p := NewLDAPProvider(staticSource(cfgEnabled()))
	_, err := p.Authenticate(context.Background(), Credentials{})
	require.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestLDAPProvider_Success(t *testing.T) {
	t.Parallel()
	cfg := cfgEnabled()
	conn := &fakeLDAP{
		searchEntry: &ldap.Entry{
			DN: "uid=alice,ou=users,dc=example,dc=com",
			Attributes: []*ldap.EntryAttribute{
				{Name: cfg.MemberAttribute, Values: []string{
					"cn=admins,ou=groups,dc=example,dc=com",
					"cn=other,ou=external,dc=example,dc=com",
				}},
			},
		},
	}
	p := &LDAPProvider{source: staticSource(cfg), dial: func(context.Context, string, bool, time.Duration) (ldapConn, error) { return conn, nil }}
	id, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "user-pwd"})
	require.NoError(t, err)
	require.Equal(t, "alice", id.Username)
	require.Equal(t, LDAPMethod, id.Method)
	require.Equal(t, []string{"admins"}, id.Groups)
	// Service bind then user bind.
	require.Len(t, conn.binds, 2)
	require.Equal(t, cfg.BindDN, conn.binds[0].user)
	require.Equal(t, "uid=alice,ou=users,dc=example,dc=com", conn.binds[1].user)
	require.Equal(t, "user-pwd", conn.binds[1].pass)
}

func TestLDAPProvider_UserNotFound(t *testing.T) {
	t.Parallel()
	conn := &fakeLDAP{} // no entries
	p := &LDAPProvider{source: staticSource(cfgEnabled()), dial: func(context.Context, string, bool, time.Duration) (ldapConn, error) { return conn, nil }}
	_, err := p.Authenticate(context.Background(), Credentials{Username: "ghost", Password: "x"})
	require.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestLDAPProvider_BadUserPassword(t *testing.T) {
	t.Parallel()
	cfg := cfgEnabled()
	calls := 0
	conn := &fakeLDAP{
		bindErrFn: func(_, _ string) error {
			calls++
			if calls == 1 {
				return nil // service bind ok
			}
			return errors.New("invalid credentials")
		},
		searchEntry: &ldap.Entry{DN: "uid=alice,ou=users,dc=example,dc=com"},
	}
	p := &LDAPProvider{source: staticSource(cfg), dial: func(context.Context, string, bool, time.Duration) (ldapConn, error) { return conn, nil }}
	_, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "WRONG"})
	require.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestLDAPProvider_ServiceBindFails(t *testing.T) {
	t.Parallel()
	conn := &fakeLDAP{bindErrFn: func(string, string) error { return errors.New("service bind failed") }}
	p := &LDAPProvider{source: staticSource(cfgEnabled()), dial: func(context.Context, string, bool, time.Duration) (ldapConn, error) { return conn, nil }}
	_, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "x"})
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrInvalidCredentials))
}

// TestLDAPProvider_LiveConfigUpdate is the regression test for "edit a
// setting in the UI and the LDAP backend picks it up without a restart":
// the source closure flips Enabled between calls, and the provider must
// honour the new value on the second Authenticate.
func TestLDAPProvider_LiveConfigUpdate(t *testing.T) {
	t.Parallel()
	cfg := schema.DefaultLDAP() // disabled
	src := func(context.Context) (schema.LDAP, error) { return cfg, nil }
	conn := &fakeLDAP{
		searchEntry: &ldap.Entry{DN: "uid=alice,ou=users,dc=example,dc=com"},
	}
	p := &LDAPProvider{source: src, dial: func(context.Context, string, bool, time.Duration) (ldapConn, error) { return conn, nil }}

	// First call: disabled.
	_, err := p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "x"})
	require.True(t, errors.Is(err, ErrProviderDisabled))

	// Operator edits the UI; the cache invalidates; the source now returns
	// the enabled snapshot.
	cfg = cfgEnabled()
	_, err = p.Authenticate(context.Background(), Credentials{Username: "alice", Password: "user-pwd"})
	require.NoError(t, err)
}

func TestFilterGroups(t *testing.T) {
	t.Parallel()
	groups := filterGroups(
		[]string{
			"cn=admins,ou=groups,dc=example,dc=com",
			"cn=outside,ou=other,dc=example,dc=com",
		},
		"ou=groups,dc=example,dc=com",
	)
	require.Equal(t, []string{"admins"}, groups)
}

// TestLDAPProvider_NilSourceDisabled ensures the historical zero-value
// behaviour (no source wired up → backend is disabled) is preserved.
func TestLDAPProvider_NilSourceDisabled(t *testing.T) {
	t.Parallel()
	p := NewLDAPProvider(nil)
	_, err := p.Authenticate(context.Background(), Credentials{Username: "a", Password: "b"})
	require.True(t, errors.Is(err, ErrProviderDisabled))
}

func TestLDAPAddress(t *testing.T) {
	t.Parallel()
	cfg := schema.DefaultLDAP()
	cfg.Host = "ldap://example.com"
	cfg.Port = 389
	addr, useTLS := ldapAddress(cfg)
	require.Equal(t, "example.com:389", addr)
	require.False(t, useTLS)

	cfg2 := schema.DefaultLDAP()
	cfg2.Host = "example.com"
	cfg2.Port = 636
	addr, useTLS = ldapAddress(cfg2)
	require.Equal(t, "example.com:636", addr)
	require.True(t, useTLS)
}
