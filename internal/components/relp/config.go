// Package relp implements the snooze-relp daemon: a RELP (Reliable Event
// Logging Protocol) ingestor that decodes RELP frames, parses the wrapped
// syslog payload, and forwards records to the Snooze v1 alert API.
//
// RELP is rsyslog's reliable-delivery transport. Each frame carries an
// integer txnr the server ACKs once the payload is durably accepted, which
// is the contract we honour here: we only emit `rsp 200 OK` after PostAlert
// returns.
//
// We reuse the leodido-based parser exposed by
// internal/components/syslog so the same RFC3164/RFC5424 semantics apply on
// both transports.
package relp

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the YAML schema for /etc/snooze/relp.yaml.
type Config struct {
	// Server is the Snooze base URL ("https://snooze.example.com/").
	Server string `yaml:"server"`

	// Username and Password authenticate against the v1 /login endpoint.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the auth backend. Empty defaults to "local".
	Method string `yaml:"method"`

	// Token short-circuits Login by using this bearer token directly.
	Token string `yaml:"token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// Listen is the TCP bind address. Defaults to "0.0.0.0:2514" which is the
	// rsyslog default RELP port.
	Listen string `yaml:"listen"`

	// Parser selects the syslog payload format: "auto" (default), "rfc3164"
	// or "rfc5424". Mirrors the snooze-syslog setting.
	Parser string `yaml:"parser"`

	// RequestTimeout caps a single PostAlert HTTP request. Defaults to 10s.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// MaxFrameBytes caps the largest RELP frame we accept (defense against
	// memory-exhaustion peers). Defaults to 1 MiB.
	MaxFrameBytes int `yaml:"max_frame_bytes"`

	// ReadTimeout caps per-session idle time between frames. Zero disables.
	ReadTimeout time.Duration `yaml:"read_timeout"`
}

// LoadConfig reads a YAML config file at path and returns the parsed Config
// with defaults applied.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return Config{}, fmt.Errorf("relp: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("relp: parse config %q: %w", path, err)
	}
	return cfg.WithDefaults()
}

// WithDefaults applies the documented defaults and validates required
// fields.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.Server) == "" {
		return c, fmt.Errorf("relp: config.server is required")
	}
	if c.Listen == "" {
		c.Listen = "0.0.0.0:2514"
	}
	if c.Parser == "" {
		c.Parser = "auto"
	}
	switch c.Parser {
	case "auto", "rfc3164", "rfc5424":
	default:
		return c, fmt.Errorf("relp: invalid parser %q (auto|rfc3164|rfc5424)", c.Parser)
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 10 * time.Second
	}
	if c.MaxFrameBytes <= 0 {
		c.MaxFrameBytes = 1 << 20 // 1 MiB
	}
	return c, nil
}
