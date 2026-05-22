// Package snmptrap implements the SNMP trap receiver daemon: it listens for
// v1/v2c/v3 traps, maps the varbinds into a snoozetypes.Record, and forwards
// the record to snooze-server via pkg/snoozeclient.
package snmptrap

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Default values applied when the config file omits the corresponding key.
const (
	defaultListen     = "0.0.0.0:162"
	defaultCommunity  = "public"
	defaultAuthMethod = "local"
	defaultTimeout    = 30 * time.Second
)

// Config is the on-disk YAML shape consumed by the snmptrap daemon. Keys that
// match the canonical snooze.yaml are kept verbatim; SNMP-specific fields live
// under the `v3:` block.
type Config struct {
	// Server is the Snooze v1 base URL (scheme + host[:port]). Required.
	Server string `yaml:"server"`

	// Username / Password / Method authenticate the embedded snoozeclient.
	// Method defaults to "local".
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Method   string `yaml:"method"`

	// Insecure disables TLS verification when calling the Snooze API.
	Insecure bool `yaml:"insecure"`

	// Listen is the UDP listen address. Defaults to 0.0.0.0:162.
	Listen string `yaml:"listen"`

	// Community is the v1/v2c community string accepted by the listener. The
	// special token "*" means "accept any community" (useful with mixed
	// fleets). Defaults to "public" when the field is omitted.
	Community string `yaml:"community"`

	// V3 carries the (optional) USM credentials used to accept SNMPv3 traps.
	// When nil/zero, only v1/v2c traps are accepted.
	V3 *V3Config `yaml:"v3,omitempty"`

	// Timeout caps each HTTP forward call. Defaults to 30s.
	Timeout time.Duration `yaml:"timeout"`

	// MIBDirs are filesystem paths gosmi scans for MIB module files.
	// Mirrors the `mib_dirs` field from the Python snmptrap component.
	// When empty the daemon skips MIB loading and stores raw dotted OIDs
	// (with dots sanitized to underscores) as record-field keys.
	MIBDirs []string `yaml:"mib_dirs"`

	// MIBList names the MIB modules to load from the configured MIBDirs.
	// e.g. ["SNMPv2-MIB", "IF-MIB"]. Module names are case-sensitive and
	// must match the MIB file's MODULE-IDENTITY declaration.
	MIBList []string `yaml:"mib_list"`
}

// V3Config bundles the USM parameters needed to authenticate SNMPv3 traps.
// auth_proto / priv_proto accept case-insensitive shorthand: "none"|"md5"|
// "sha"|"sha224"|"sha256"|"sha384"|"sha512" for auth and "none"|"des"|"aes"|
// "aes192"|"aes256" for priv.
type V3Config struct {
	User           string `yaml:"user"`
	AuthProto      string `yaml:"auth_proto"`
	AuthPassphrase string `yaml:"auth_passphrase"`
	PrivProto      string `yaml:"priv_proto"`
	PrivPassphrase string `yaml:"priv_passphrase"`
}

// LoadConfig reads, parses and defaults a YAML config from path. It performs
// minimal validation so the daemon fails fast on a misconfigured file.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator-provided
	if err != nil {
		return Config{}, fmt.Errorf("snmptrap: read config: %w", err)
	}
	return parseConfig(raw)
}

// parseConfig is split out for unit tests; it never touches the filesystem.
func parseConfig(raw []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("snmptrap: parse yaml: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyDefaults fills in zero-valued fields with their documented defaults.
func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = defaultListen
	}
	if c.Community == "" {
		c.Community = defaultCommunity
	}
	if c.Method == "" {
		c.Method = defaultAuthMethod
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
}

// validate checks for the smallest set of fields without which the daemon
// cannot start. We deliberately allow empty community (= accept any).
func (c *Config) validate() error {
	if c.Server == "" {
		return errors.New("snmptrap: config: `server` is required")
	}
	if c.V3 != nil && c.V3.User == "" {
		return errors.New("snmptrap: config: `v3.user` is required when `v3` is set")
	}
	return nil
}
