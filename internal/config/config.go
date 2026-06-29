// Package config exposes the bootstrap configuration of snooze-server. The
// layout is a two-tier split: this package owns the immutable YAML/env-driven
// bootstrap data, while the live-editable counterpart sits behind the
// :type:`RuntimeSettings` interface and is backed by the “settings“ plugin.
package config

import (
	"github.com/snoozeweb/snooze/internal/config/schema"
)

// Config is the top-level bootstrap configuration object.
type Config struct {
	BaseDir string `koanf:"-"`

	Core         schema.Core         `koanf:"core"`
	General      schema.General      `koanf:"general"`
	Housekeeper  schema.Housekeeper  `koanf:"housekeeping"`
	Notification schema.Notification `koanf:"notification"`
	LDAP         schema.LDAP         `koanf:"ldap"`
	Web          schema.Web          `koanf:"web"`
	Auth         schema.Auth         `koanf:"auth"`
	Syncer       schema.Syncer       `koanf:"syncer"`
	Ingest       schema.Ingest       `koanf:"ingest"`
	OIDC         schema.OIDC         `koanf:"oidc"`
	SAML         schema.SAML         `koanf:"saml"`
	AuthProxy    schema.AuthProxy    `koanf:"auth_proxy"`

	// OIDCProviders is the OPTIONAL multi-IdP list, loaded from
	// oidc_providers.yaml. It is independent of the legacy scalar OIDC above:
	// when this slice is non-empty buildAuthProviders registers one provider per
	// entry; when empty it falls back to the single OIDC. See
	// EffectiveOIDCProviders. Kept separate (not a refactor of OIDC into a slice)
	// so every existing single-`oidc:` deployment loads byte-identically.
	OIDCProviders []schema.OIDC `koanf:"oidc_providers"`
}

// Default returns a Config populated with the canonical default values for
// each section. The result is what “Load“ produces when the basedir is
// empty.
func Default() *Config {
	return &Config{
		Core:         schema.DefaultCore(),
		General:      schema.DefaultGeneral(),
		Housekeeper:  schema.DefaultHousekeeper(),
		Notification: schema.DefaultNotification(),
		LDAP:         schema.DefaultLDAP(),
		Web:          schema.DefaultWeb(),
		Auth:         schema.DefaultAuth(),
		Syncer:       schema.DefaultSyncer(),
		Ingest:       schema.DefaultIngest(),
		OIDC:         schema.DefaultOIDC(),
		SAML:         schema.DefaultSAML(),
		AuthProxy:    schema.DefaultAuthProxy(),
	}
}

// Validate runs the package-level validator over the config. It is called by
// “Load“ and can be invoked again after any in-memory mutation (which should
// be rare — runtime mutations belong to :type:`RuntimeSettings`).
func (c *Config) Validate() error { return validate(c) }

// EffectiveOIDCProviders returns the operative list of OIDC identity providers:
// the explicit multi-IdP slice (OIDCProviders) when it is non-empty, otherwise
// a single-element slice wrapping the legacy scalar OIDC — but only when that
// legacy entry is actually configured (Enabled or a non-empty Method). When
// neither is set it returns nil. This is the single seam that preserves
// backward compatibility: existing single-`oidc:` deployments take the fallback
// branch and behave exactly as before. Both buildAuthProviders (provider
// registration) and core.bootstrap (admin-group seeding) consume it.
func (c *Config) EffectiveOIDCProviders() []schema.OIDC {
	if len(c.OIDCProviders) > 0 {
		return c.OIDCProviders
	}
	if c.OIDC.Enabled || c.OIDC.Method != "" {
		return []schema.OIDC{c.OIDC}
	}
	return nil
}
