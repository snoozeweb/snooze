package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"

	"github.com/japannext/snooze/internal/config/schema"
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
// ``Database`` is shaped as a flat struct with a type discriminator.
func validateDatabase(d *schema.Database) error {
	switch d.Type {
	case "mongo":
		// Host can be a string or a list; only check that something was set.
		// An empty host falls back to localhost in pymongo so we don't reject it.
		return nil
	case "file":
		if d.Path == "" {
			return errors.New("file backend requires path")
		}
		return nil
	case "postgres":
		if d.DSN == "" && d.Host == nil {
			return errors.New("postgres backend requires either dsn or host")
		}
		return nil
	default:
		return fmt.Errorf("unknown database type %q", d.Type)
	}
}

// ipv4Loose is a custom validator that accepts IPv4 addresses or empty
// strings.  ``0.0.0.0`` is the legacy default and ``net.ParseIP`` honours it.
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
