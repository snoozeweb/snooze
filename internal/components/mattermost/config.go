// Package mattermost is the Go implementation of the snooze-mattermost
// daemon. It connects to a Mattermost server over WebSocket, listens for
// slash-command / mention events, and forwards user intent
// (ack / close / reopen / snooze / comment) to a Snooze v1 REST API via
// pkg/snoozeclient.
//
// The package is split into:
//
//   - config.go : YAML config loader + validation.
//   - daemon.go : top-level Daemon orchestrator with Run(ctx).
//   - api.go    : minimal Mattermost REST client (login, post message,
//     get-team-by-name, lookup channel).
//   - ws.go     : WebSocket client with reconnect/backoff.
//   - forward.go: slash-command → snooze action mapper.
//
// The daemon does NOT use the upstream Mattermost SDK
// (github.com/mattermost/mattermost/server/public/model) because it pulls in
// the entire server module (hundreds of MB of transitive deps). All wire
// shapes used here are hand-rolled against the public v4 REST/WS contract.
package mattermost

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk YAML shape consumed by the daemon. Only a small
// subset of the legacy Python config is mapped — anything not represented
// here is intentionally omitted from the Go rewrite.
type Config struct {
	// Server is the Snooze server origin (e.g. https://snooze.example/).
	Server string `yaml:"server"`
	// Username and Password authenticate against Snooze (snoozeclient).
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// Method selects the Snooze auth backend (default "local").
	Method string `yaml:"method"`
	// Insecure disables TLS verification for the Snooze client. Off by default.
	Insecure bool `yaml:"insecure"`

	// MattermostURL is the Mattermost site origin (e.g. https://mm.example/).
	MattermostURL string `yaml:"mattermost_url"`
	// MattermostToken is a personal access token used as the bearer for
	// every REST + WS frame sent to Mattermost.
	MattermostToken string `yaml:"mattermost_token"`
	// MattermostTeam is the team name (slug) the bot operates within.
	MattermostTeam string `yaml:"mattermost_team"`
	// Channels restricts the bot to messages posted in these channel names.
	// Empty means "all channels the bot can see".
	Channels []string `yaml:"channels"`

	// BotName is the at-mention name announced in help replies. Defaults to "snooze".
	BotName string `yaml:"bot_name"`

	// PingInterval controls the WebSocket keepalive cadence. Defaults to 30s.
	PingInterval time.Duration `yaml:"ping_interval"`
	// ReconnectInitialBackoff is the first delay before reconnecting on
	// WebSocket disconnect. Doubles each attempt, capped at ReconnectMaxBackoff.
	ReconnectInitialBackoff time.Duration `yaml:"reconnect_initial_backoff"`
	// ReconnectMaxBackoff caps the reconnect backoff.
	ReconnectMaxBackoff time.Duration `yaml:"reconnect_max_backoff"`
}

// LoadConfig reads path, parses YAML and validates required fields.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("mattermost: read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("mattermost: parse config %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// applyDefaults fills zero-valued fields with sensible defaults.
func (c *Config) applyDefaults() {
	if c.Method == "" {
		c.Method = "local"
	}
	if c.BotName == "" {
		c.BotName = "snooze"
	}
	if c.PingInterval == 0 {
		c.PingInterval = 30 * time.Second
	}
	if c.ReconnectInitialBackoff == 0 {
		c.ReconnectInitialBackoff = time.Second
	}
	if c.ReconnectMaxBackoff == 0 {
		c.ReconnectMaxBackoff = time.Minute
	}
	c.Server = strings.TrimRight(c.Server, "/")
	c.MattermostURL = strings.TrimRight(c.MattermostURL, "/")
}

// Validate returns the first missing required field, or nil if ok.
func (c *Config) Validate() error {
	switch {
	case c.Server == "":
		return errors.New("mattermost: config.server is required")
	case c.MattermostURL == "":
		return errors.New("mattermost: config.mattermost_url is required")
	case c.MattermostToken == "":
		return errors.New("mattermost: config.mattermost_token is required")
	case c.MattermostTeam == "":
		return errors.New("mattermost: config.mattermost_team is required")
	}
	return nil
}
