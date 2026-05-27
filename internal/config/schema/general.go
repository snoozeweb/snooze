package schema

import "strings"

// General is the bootstrap mirror of the Python “GeneralConfig“ section. The
// values can still be overridden at runtime via the DB-backed settings store.
type General struct {
	DefaultAuthBackend string `koanf:"default_auth_backend" validate:"oneof=local ldap anonymous"`
	LocalEnabled       bool   `koanf:"local_enabled"`
	LocalUsersEnabled  bool   `koanf:"local_users_enabled"`
	MetricsEnabled     bool   `koanf:"metrics_enabled"`
	AnonymousEnabled   bool   `koanf:"anonymous_enabled"`
	// AnonymousAdmin grants the synthetic "admin" role + AllPermission
	// wildcard to anonymous logins. Used for try / demo deploys where every
	// anonymous visitor needs full access.
	AnonymousAdmin bool     `koanf:"anonymous_admin"`
	OKSeverities   []string `koanf:"ok_severities"`
}

// DefaultGeneral returns the canonical defaults.
func DefaultGeneral() General {
	return General{
		DefaultAuthBackend: "local",
		LocalEnabled:       true,
		LocalUsersEnabled:  true,
		MetricsEnabled:     true,
		AnonymousEnabled:   false,
		AnonymousAdmin:     false,
		OKSeverities:       []string{"ok", "success"},
	}
}

// Normalize folds the OK severity list to its case-folded form, matching the
// Python “ok_severities“ validator.
func (g *General) Normalize() {
	for i, s := range g.OKSeverities {
		g.OKSeverities[i] = strings.ToLower(strings.TrimSpace(s))
	}
}
