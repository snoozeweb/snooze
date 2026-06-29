package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config/schema"
)

var (
	validatorOnce sync.Once
	v             *validator.Validate
)

// getValidator returns a process-wide validator instance with the custom
// snooze rules registered.
func getValidator() *validator.Validate {
	validatorOnce.Do(func() {
		v = validator.New(validator.WithRequiredStructEnabled())
		// `ipv4_loose` accepts both unspecified/loopback/regular IPv4 strings.
		_ = v.RegisterValidation("ipv4_loose", ipv4Loose)
		_ = v.RegisterValidation("dsn", dsnLooksSane)
	})
	return v
}

// validate runs the tag-based checks plus the structural rules that cannot be
// expressed as a single tag (LDAP cross-field requirements, postgres DSN
// sanity, etc.).
func validate(c *Config) error {
	if err := getValidator().Struct(c); err != nil {
		return fmt.Errorf("config: validation failed: %w", err)
	}
	if err := validateDatabase(&c.Core.Database); err != nil {
		return fmt.Errorf("config: core.database: %w", err)
	}
	if err := validateLDAP(&c.LDAP); err != nil {
		return fmt.Errorf("config: ldap: %w", err)
	}
	if err := validateOIDC(&c.OIDC); err != nil {
		return fmt.Errorf("config: oidc: %w", err)
	}
	if err := validateOIDCEntries(c.OIDCProviders); err != nil {
		return fmt.Errorf("config: oidc_providers: %w", err)
	}
	if err := validateSAML(&c.SAML); err != nil {
		return fmt.Errorf("config: saml: %w", err)
	}
	if err := validateAuthProxy(&c.AuthProxy); err != nil {
		return fmt.Errorf("config: auth_proxy: %w", err)
	}
	return nil
}

// validateAuthProxy enforces the enabled-mode requirements for the
// trusted-header auth mode. When disabled it is always valid. When enabled,
// user_header must be set and every trusted_proxies entry must parse as an IP
// or CIDR. An empty trusted_proxies list while enabled is intentionally NOT an
// error — it is a fail-open operator choice (e.g. the proxy terminates on the
// same pod/loopback); boot logs a loud WARN instead (see cmd/snooze-server).
func validateAuthProxy(a *schema.AuthProxy) error {
	if !a.Enabled {
		return nil
	}
	if a.UserHeader == "" {
		return errors.New("user_header is required when enabled")
	}
	for _, entry := range a.TrustedProxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(entry); err == nil {
			continue
		}
		if net.ParseIP(entry) != nil {
			continue
		}
		return fmt.Errorf("trusted_proxies entry %q is not a valid IP or CIDR", entry)
	}
	return nil
}

// validateOIDC enforces that, when OIDC is enabled, the discovery + client
// settings are present and the issuer is an https URL.
func validateOIDC(o *schema.OIDC) error {
	if !o.Enabled {
		return nil
	}
	var missing []string
	if o.Issuer == "" {
		missing = append(missing, "issuer")
	}
	if o.ClientID == "" {
		missing = append(missing, "client_id")
	}
	if o.ClientSecret == "" {
		missing = append(missing, "client_secret")
	}
	if o.RedirectURL == "" {
		missing = append(missing, "redirect_url")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields when enabled: %s", strings.Join(missing, ", "))
	}
	if !strings.HasPrefix(o.Issuer, "https://") {
		return fmt.Errorf("issuer must be an https URL, got %q", o.Issuer)
	}
	return nil
}

// validateOIDCEntries validates the multi-IdP slice (Config.OIDCProviders). For
// each entry it resolves the provider preset first (so a bad provider/missing
// provider_params surfaces a clear error), then applies the same per-entry
// field checks as the single-IdP path, and finally ensures no two entries share
// the same method slug (which is the login URL segment + JWT method claim and
// must be unique across providers). An empty slice is valid — the legacy single
// `oidc:` path is used instead.
func validateOIDCEntries(oo []schema.OIDC) error {
	seen := make(map[string]struct{}, len(oo))
	for i := range oo {
		o := oo[i]
		// Resolve the preset on a copy so the issuer is filled before the
		// per-entry https/required-field checks run.
		if err := auth.ResolvePreset(&o); err != nil {
			return fmt.Errorf("entry %d (method %q): %w", i, o.Method, err)
		}
		if err := validateOIDC(&o); err != nil {
			return fmt.Errorf("entry %d (method %q): %w", i, o.Method, err)
		}
		if o.Method == "" {
			return fmt.Errorf("entry %d: method is required", i)
		}
		if _, dup := seen[o.Method]; dup {
			return fmt.Errorf("duplicate method %q", o.Method)
		}
		seen[o.Method] = struct{}{}
	}
	return nil
}

// validateSAML enforces that, when SAML is enabled, an IdP metadata source
// (URL or inline XML) and the ACS URL are present. The signature/audience
// checks belong to the crewjam library at runtime; here we only gate on the
// fields the SP cannot start without.
func validateSAML(s *schema.SAML) error {
	if !s.Enabled {
		return nil
	}
	var missing []string
	if s.IDPMetadataURL == "" && s.IDPMetadataXML == "" {
		missing = append(missing, "idp_metadata_url or idp_metadata_xml")
	}
	if s.ACSURL == "" {
		missing = append(missing, "acs_url")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields when enabled: %s", strings.Join(missing, ", "))
	}
	return nil
}

// validateLDAP enforces the Python rule that, when LDAP is enabled, the
// connection settings must all be present.
func validateLDAP(l *schema.LDAP) error {
	if !l.Enabled {
		return nil
	}
	var missing []string
	if l.BaseDN == "" {
		missing = append(missing, "base_dn")
	}
	if l.UserFilter == "" {
		missing = append(missing, "user_filter")
	}
	if l.BindDN == "" {
		missing = append(missing, "bind_dn")
	}
	if l.BindPassword == "" {
		missing = append(missing, "bind_password")
	}
	if l.Host == "" {
		missing = append(missing, "host")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields when enabled: %s", strings.Join(missing, ", "))
	}
	return nil
}

// validateDatabase covers the bits the tag system can't express because
// “Database“ is shaped as a flat struct with a type discriminator.
func validateDatabase(d *schema.Database) error {
	// Accept exactly the spellings the driver dispatch (openDB) understands.
	switch d.Type {
	case "mongo", "mongodb":
		// Host can be a string or a list; only check that something was set.
		// An empty host falls back to localhost in pymongo so we don't reject it.
		return nil
	case "file":
		if d.Path == "" {
			return errors.New("file backend requires path")
		}
		return nil
	case "sqlite", "":
		// SQLite (and the empty default). openDB defaults an empty path to
		// ./db.sqlite, so no field is strictly required here.
		return nil
	case "postgres", "pg":
		if d.DSN == "" && d.Host == nil {
			return errors.New("postgres backend requires either dsn or host")
		}
		return nil
	default:
		return fmt.Errorf("unknown database type %q", d.Type)
	}
}

// ipv4Loose is a custom validator that accepts IPv4 addresses or empty
// strings.  “0.0.0.0“ is the legacy default and “net.ParseIP“ honours it.
func ipv4Loose(fl validator.FieldLevel) bool {
	s := strings.TrimSpace(fl.Field().String())
	if s == "" {
		return true
	}
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

// dsnLooksSane is a lightweight DSN check: either empty (which is allowed when
// other fields supply the connection details) or a string that looks like a
// libpq/MongoDB URL or a key=value list.
func dsnLooksSane(fl validator.FieldLevel) bool {
	s := strings.TrimSpace(fl.Field().String())
	if s == "" {
		return true
	}
	switch {
	case strings.HasPrefix(s, "postgres://"),
		strings.HasPrefix(s, "postgresql://"),
		strings.HasPrefix(s, "mongodb://"),
		strings.HasPrefix(s, "mongodb+srv://"):
		return true
	case strings.Contains(s, "="):
		return true
	}
	return false
}
