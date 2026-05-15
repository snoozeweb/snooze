// Package syslog implements the snooze-syslog daemon: a syslog (RFC3164 +
// RFC5424) ingestor that forwards parsed messages to the Snooze v1 alert API.
//
// The package exposes a Daemon that owns one UDP listener and one TCP listener,
// parses every received line with leodido/go-syslog/v4, maps the result to a
// snoozetypes.Record and POSTs it via pkg/snoozeclient.
package syslog

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the YAML schema for /etc/snooze/syslog.yaml. It is intentionally
// tiny — the server uses koanf for layered config, but a forwarder daemon only
// needs a handful of knobs and a flat file keeps packaging simple.
type Config struct {
	// Server is the Snooze base URL ("https://snooze.example.com/").
	Server string `yaml:"server"`

	// Username and Password authenticate against the v1 /login endpoint.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the auth backend. Empty defaults to "local".
	Method string `yaml:"method"`

	// Token, when set, short-circuits the login flow and is used as the bearer
	// token directly. Useful for service accounts with long-lived tokens.
	Token string `yaml:"token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// ListenUDP is the UDP bind address (e.g. "0.0.0.0:514"). Empty disables
	// the UDP listener.
	ListenUDP string `yaml:"listen_udp"`

	// ListenTCP is the TCP bind address (e.g. "0.0.0.0:6514"). Empty disables
	// the TCP listener.
	ListenTCP string `yaml:"listen_tcp"`

	// Parser selects the message format: "auto" (default), "rfc3164" or "rfc5424".
	Parser string `yaml:"parser"`

	// RequestTimeout caps a single PostAlert HTTP request. Defaults to 10s.
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// LoadConfig reads a YAML config file at path and returns the parsed Config
// with reasonable defaults applied.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return Config{}, fmt.Errorf("syslog: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("syslog: parse config %q: %w", path, err)
	}
	return cfg.WithDefaults()
}

// WithDefaults fills in zero-value fields with the documented defaults and
// validates that at least one listener and a Server URL are configured.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.Server) == "" {
		return c, fmt.Errorf("syslog: config.server is required")
	}
	if c.Parser == "" {
		c.Parser = "auto"
	}
	switch c.Parser {
	case "auto", "rfc3164", "rfc5424":
	default:
		return c, fmt.Errorf("syslog: invalid parser %q (auto|rfc3164|rfc5424)", c.Parser)
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 10 * time.Second
	}
	if c.ListenUDP == "" && c.ListenTCP == "" {
		return c, fmt.Errorf("syslog: at least one of listen_udp / listen_tcp must be set")
	}
	return c, nil
}
