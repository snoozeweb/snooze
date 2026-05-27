// Package mcp implements the snooze-mcp daemon: a Model Context Protocol
// (MCP) server that exposes Snooze alerts and record actions as callable
// tools to AI assistants (Claude Desktop, Cursor, etc.).
//
// Unlike the other Snooze daemons this one is NOT a long-running network
// service. It speaks JSON-RPC 2.0 over stdio (newline-delimited messages on
// stdin/stdout) and is spawned on-demand by the AI client. All logging goes
// to stderr because stdout is the protocol channel.
//
// The package is intentionally split so the protocol logic is unit-testable
// without a real Snooze server:
//
//   - config.go    Config + LoadConfig + WithDefaults + environment overlay.
//   - snoozeapi.go The narrow snoozeAPI interface the Server depends on,
//     plus an adapter over pkg/snoozeclient. Tests inject a fake.
//   - server.go    Server with Handle(ctx, requestBytes) []byte — the pure
//     JSON-RPC engine, no I/O of its own.
//   - tools.go     The tool catalog + tools/call dispatch.
//   - daemon.go    Daemon wiring + Run(ctx) stdio loop.
package mcp

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the YAML schema for /etc/snooze/mcp.yaml. It is the standard
// Snooze-client block plus a debug toggle — the MCP server has no knobs of
// its own beyond how it reaches snooze-server.
//
// Because MCP servers are typically launched by an AI client that passes
// configuration through environment variables, every field can also be
// supplied via the environment (see applyEnv). The environment takes
// precedence over the file so a client-managed launch can override a
// system-installed mcp.yaml.
type Config struct {
	// Server is the Snooze base URL ("https://snooze.example.com"). Required
	// (file or SNOOZE_SERVER env).
	Server string `yaml:"server"`

	// Username / Password authenticate against the v1 /login endpoint.
	// Required when Token is empty.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the auth backend on Snooze. Empty defaults to "local".
	Method string `yaml:"method"`

	// Token, when set, short-circuits the login flow and is used as the
	// bearer token directly.
	Token string `yaml:"token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// RequestTimeout caps a single Snooze HTTP request. Defaults to 30s.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// Debug enables debug-level logging (to stderr).
	Debug bool `yaml:"debug"`
}

// ErrMissingConfig is the sentinel returned for missing required fields. The
// concrete field is surfaced in the wrapped error message.
var ErrMissingConfig = errors.New("mcp: required configuration missing")

// LoadConfig reads a YAML file at path, overlays environment variables, and
// returns a fully-defaulted Config.
//
// A missing file is tolerated when the required fields are supplied entirely
// through the environment — this is the common case for an MCP client that
// launches `snooze-mcp` with SNOOZE_SERVER / SNOOZE_TOKEN in its env block
// and no config file installed.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied
	switch {
	case err == nil:
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("mcp: parse config %q: %w", path, err)
		}
	case os.IsNotExist(err):
		// No file — fall back entirely to the environment. WithDefaults will
		// fail with a clear message if the required fields aren't present.
	default:
		return Config{}, fmt.Errorf("mcp: read config %q: %w", path, err)
	}
	cfg.applyEnv(os.Getenv)
	return cfg.WithDefaults()
}

// applyEnv overlays environment variables on top of the file-derived config.
// Environment wins because an MCP client launch is the authoritative source
// of configuration in the stdio model. getenv is injectable for tests.
func (c *Config) applyEnv(getenv func(string) string) {
	if v := getenv("SNOOZE_SERVER"); v != "" {
		c.Server = v
	}
	if v := getenv("SNOOZE_USERNAME"); v != "" {
		c.Username = v
	}
	if v := getenv("SNOOZE_PASSWORD"); v != "" {
		c.Password = v
	}
	if v := getenv("SNOOZE_METHOD"); v != "" {
		c.Method = v
	}
	if v := getenv("SNOOZE_TOKEN"); v != "" {
		c.Token = v
	}
	if v := getenv("SNOOZE_INSECURE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Insecure = b
		}
	}
	if v := getenv("SNOOZE_REQUEST_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.RequestTimeout = d
		}
	}
	if v := getenv("SNOOZE_DEBUG"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Debug = b
		}
	}
}

// WithDefaults fills in zero values with documented defaults and validates
// the required fields. It returns a copy so callers can keep the original
// for diagnostics.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.Server) == "" {
		return c, fmt.Errorf("%w: server (set it in mcp.yaml or via SNOOZE_SERVER)", ErrMissingConfig)
	}
	c.Server = strings.TrimRight(c.Server, "/")
	if c.Token == "" && c.Username == "" && c.Method != "anonymous" {
		return c, fmt.Errorf("%w: token or username (set SNOOZE_TOKEN or SNOOZE_USERNAME/SNOOZE_PASSWORD)", ErrMissingConfig)
	}
	if c.Method == "" {
		c.Method = "local"
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 30 * time.Second
	}
	return c, nil
}
