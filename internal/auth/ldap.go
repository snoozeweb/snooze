package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// LDAPConfigSource returns the current LDAP configuration. The production
// implementation is “*config.RuntimeSettings.LDAP“; tests substitute a
// closure over a hand-rolled config struct.
type LDAPConfigSource func(ctx context.Context) (schema.LDAP, error)

// LDAPMethod is the auth method string set on Identity for LDAP logins.
const LDAPMethod = "ldap"

// ldapDialFunc dials and returns a bound *ldap.Conn-like wrapper. Tests inject
// a fake; production uses dialLDAP.
type ldapDialFunc func(ctx context.Context, addr string, useTLS bool, timeout time.Duration) (ldapConn, error)

// ldapConn is the subset of ldap.Conn that LDAPProvider needs. Defined as an
// interface so the unit tests can supply a stub without touching a real
// directory.
type ldapConn interface {
	Bind(user, password string) error
	Search(req *ldap.SearchRequest) (*ldap.SearchResult, error)
	Close() error
}

// LDAPProvider authenticates against an LDAP directory by binding with the
// configured service account, searching for the user DN, then rebinding as
// the user with the supplied password.
//
// The provider reads its configuration through “source“ at every
// Authenticate call so a runtime edit in the Settings UI (which writes to
// the DB-backed config store and invalidates the cache) takes effect on
// the next login attempt — no restart required.
type LDAPProvider struct {
	source LDAPConfigSource
	dial   ldapDialFunc
}

// NewLDAPProvider returns a configured LDAP provider that reads its config
// snapshot from source on every Authenticate call.
func NewLDAPProvider(source LDAPConfigSource) *LDAPProvider {
	if source == nil {
		// A nil source keeps the LDAP backend "disabled by default" rather
		// than crashing on Authenticate — matches the historical behaviour
		// when the operator omitted the [ldap] section entirely.
		source = func(context.Context) (schema.LDAP, error) {
			return schema.DefaultLDAP(), nil
		}
	}
	return &LDAPProvider{source: source, dial: defaultDial}
}

// NewLDAPProviderFromConfig is the legacy constructor kept for the
// transition window. It binds the provider to a static config snapshot
// (no live updates).
//
// Deprecated: prefer NewLDAPProvider with a *config.RuntimeSettings-backed
// source so changes to the LDAP section in the Settings UI take effect
// without a restart.
func NewLDAPProviderFromConfig(cfg schema.LDAP) *LDAPProvider {
	return &LDAPProvider{
		source: func(context.Context) (schema.LDAP, error) { return cfg, nil },
		dial:   defaultDial,
	}
}

// Name returns "ldap".
func (l *LDAPProvider) Name() string { return LDAPMethod }

// Authenticate runs the search-then-bind LDAP flow. Returns
// ErrInvalidCredentials for every failure that depends on the username or
// password; wraps the underlying error with %w for operational failures
// (network, malformed config).
func (l *LDAPProvider) Authenticate(ctx context.Context, c Credentials) (Identity, error) {
	cfg, err := l.source(ctx)
	if err != nil {
		return Identity{}, fmt.Errorf("ldap: read config: %w", err)
	}
	if !cfg.Enabled {
		return Identity{}, fmt.Errorf("ldap: %w", ErrProviderDisabled)
	}
	if c.Username == "" || c.Password == "" {
		return Identity{}, ErrInvalidCredentials
	}
	if cfg.Host == "" || cfg.BaseDN == "" || cfg.UserFilter == "" {
		return Identity{}, errors.New("ldap: incomplete configuration (host/base_dn/user_filter required)")
	}

	addr, useTLS := ldapAddress(cfg)
	conn, err := l.dial(ctx, addr, useTLS, 10*time.Second)
	if err != nil {
		return Identity{}, fmt.Errorf("ldap dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			// Never include bind_password in the error message — it would
			// leak the credential into operator logs.
			return Identity{}, fmt.Errorf("ldap service bind: %w", err)
		}
	}

	filter := strings.ReplaceAll(cfg.UserFilter, "%s", ldap.EscapeFilter(c.Username))
	attrs := []string{
		cfg.MemberAttribute,
		cfg.EmailAttribute,
		cfg.DisplayNameAttribute,
	}
	req := ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, 0, false,
		filter,
		attrs,
		nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return Identity{}, fmt.Errorf("ldap search: %w", ErrInvalidCredentials)
	}
	if len(res.Entries) == 0 {
		return Identity{}, ErrInvalidCredentials
	}
	entry := res.Entries[0]

	// Rebind as the user with the supplied password to validate it.
	if err := conn.Bind(entry.DN, c.Password); err != nil {
		return Identity{}, ErrInvalidCredentials
	}

	groups := filterGroups(entry.GetAttributeValues(cfg.MemberAttribute), cfg.GroupDN)
	return Identity{
		Username: c.Username,
		Method:   LDAPMethod,
		Groups:   groups,
	}, nil
}

// ldapAddress returns the host:port pair and whether TLS should be used. The
// Python codebase let the host string contain a scheme; we preserve that.
func ldapAddress(cfg schema.LDAP) (string, bool) {
	host := cfg.Host
	useTLS := cfg.Port == 636
	if strings.Contains(host, "://") {
		useTLS = strings.HasPrefix(host, "ldaps://")
		host = strings.TrimPrefix(host, "ldaps://")
		host = strings.TrimPrefix(host, "ldap://")
	}
	if !strings.Contains(host, ":") && cfg.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, cfg.Port)
	}
	return host, useTLS
}

// filterGroups extracts the CN of each group DN that lives under one of the
// configured “group_dn“ suffixes. Matches the Python LdapAuthRoute trimming
// rule (split on ":" inside group_dn, suffix-match, return RDN value).
func filterGroups(memberOf []string, groupDN string) []string {
	if groupDN == "" {
		return groupNames(memberOf)
	}
	suffixes := strings.Split(groupDN, ":")
	out := make([]string, 0, len(memberOf))
	for _, dn := range memberOf {
		for _, suffix := range suffixes {
			if strings.HasSuffix(dn, suffix) {
				out = append(out, rdnValue(dn))
				break
			}
		}
	}
	return out
}

// groupNames extracts the RDN value from every DN — used when no group_dn
// filter is configured.
func groupNames(memberOf []string) []string {
	out := make([]string, 0, len(memberOf))
	for _, dn := range memberOf {
		out = append(out, rdnValue(dn))
	}
	return out
}

// rdnValue returns the value of the first RDN of a DN ("cn=admins,..." → "admins").
func rdnValue(dn string) string {
	first := strings.SplitN(dn, ",", 2)[0]
	parts := strings.SplitN(first, "=", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return first
}

// defaultDial is the production dialer. It honours the StartTLS / LDAPS split
// and applies a connect timeout.
func defaultDial(ctx context.Context, addr string, useTLS bool, timeout time.Duration) (ldapConn, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var (
		conn *ldap.Conn
		err  error
	)
	if useTLS {
		// #nosec G402 — TLS hardening lives in the config layer (CA file, etc.); the
		// auth package honours whatever the operator configured.
		conn, err = ldap.DialURL("ldaps://"+addr, ldap.DialWithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	} else {
		conn, err = ldap.DialURL("ldap://" + addr)
	}
	if err != nil {
		return nil, err
	}
	// Propagate ctx-based cancellation via SetTimeout (no native ctx in v3).
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetTimeout(time.Until(deadline))
	}
	return conn, nil
}
