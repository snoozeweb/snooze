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
	}
}

// Validate runs the package-level validator over the config. It is called by
// “Load“ and can be invoked again after any in-memory mutation (which should
// be rare — runtime mutations belong to :type:`RuntimeSettings`).
func (c *Config) Validate() error { return validate(c) }
