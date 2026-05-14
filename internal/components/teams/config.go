// Package teams implements the snooze-teams daemon: a Microsoft Teams bridge
// that forwards Snooze alerts to a Teams channel and turns @mentions in that
// channel into Snooze ack/close/snooze/comment actions.
//
// The package exposes a Daemon owning a Microsoft Graph poller and a Snooze
// REST client. The bridge talks to Graph over plain net/http using the
// OAuth2 client_credentials grant (no msgraph-sdk-go dependency) — only two
// endpoints are needed (POST channel message, GET channel messages) and the
// stdlib version is ~150 LoC.
package teams

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the YAML schema for /etc/snooze/teams.yaml. It is intentionally
// flat — the daemon needs a handful of knobs plus the Snooze credentials.
type Config struct {
	// Server is the Snooze base URL ("https://snooze.example.com/").
	Server string `yaml:"server"`

	// Username and Password authenticate against the v1 /login endpoint.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the auth backend. Empty defaults to "local".
	Method string `yaml:"method"`

	// Token, when set, short-circuits the login flow and is used as the
	// bearer token directly.
	Token string `yaml:"token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// TenantID is the Azure AD tenant (GUID or domain). Required.
	TenantID string `yaml:"tenant_id"`

	// ClientID is the Azure AD application (client) ID. Required.
	ClientID string `yaml:"client_id"`

	// ClientSecret is the application secret used by the client_credentials
	// grant. Required.
	ClientSecret string `yaml:"client_secret"`

	// TeamID is the Microsoft Graph team identifier (a GUID). Required —
	// channel messages are scoped under /teams/{TeamID}/channels/{ChannelID}.
	TeamID string `yaml:"team_id"`

	// ChannelID is the Graph channel identifier (`19:xxxx@thread.tacv2`).
	// Required.
	ChannelID string `yaml:"channel_id"`

	// GraphBase overrides the Graph API root. Defaults to
	// "https://graph.microsoft.com/v1.0". Mainly useful for tests.
	GraphBase string `yaml:"graph_base"`

	// LoginBase overrides the OAuth2 authority. Defaults to
	// "https://login.microsoftonline.com". Mainly useful for tests.
	LoginBase string `yaml:"login_base"`

	// Scope overrides the OAuth2 scope. Defaults to
	// "https://graph.microsoft.com/.default" — the client_credentials grant
	// requires a single resource scope.
	Scope string `yaml:"scope"`

	// PollInterval controls how often the daemon fetches new channel
	// messages. Defaults to 10s.
	PollInterval time.Duration `yaml:"poll_interval"`

	// PollLookback is the initial "since" window the first poll uses to
	// avoid replaying historical messages. Defaults to 1 minute.
	PollLookback time.Duration `yaml:"poll_lookback"`

	// RequestTimeout caps a single HTTP request. Defaults to 15s.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// BotName is the @mention name the daemon listens for in channel
	// messages. Defaults to "SnoozeBot".
	BotName string `yaml:"bot_name"`
}

// ErrMissingConfig is the sentinel returned for missing required fields. The
// concrete fields are surfaced in the wrapped error message.
var ErrMissingConfig = errors.New("teams: required configuration missing")

// LoadConfig reads a YAML file at path and returns a fully-defaulted Config.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied
	if err != nil {
		return Config{}, fmt.Errorf("teams: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("teams: parse config %q: %w", path, err)
	}
	return cfg.WithDefaults()
}

// WithDefaults fills in zero values with documented defaults and validates the
// required fields. It returns a copy so callers can keep the original for
// diagnostics.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.Server) == "" {
		return c, fmt.Errorf("%w: server", ErrMissingConfig)
	}
	if strings.TrimSpace(c.TenantID) == "" {
		return c, fmt.Errorf("%w: tenant_id", ErrMissingConfig)
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return c, fmt.Errorf("%w: client_id", ErrMissingConfig)
	}
	if strings.TrimSpace(c.ClientSecret) == "" {
		return c, fmt.Errorf("%w: client_secret", ErrMissingConfig)
	}
	if strings.TrimSpace(c.TeamID) == "" {
		return c, fmt.Errorf("%w: team_id", ErrMissingConfig)
	}
	if strings.TrimSpace(c.ChannelID) == "" {
		return c, fmt.Errorf("%w: channel_id", ErrMissingConfig)
	}
	if c.GraphBase == "" {
		c.GraphBase = "https://graph.microsoft.com/v1.0"
	}
	c.GraphBase = strings.TrimRight(c.GraphBase, "/")
	if c.LoginBase == "" {
		c.LoginBase = "https://login.microsoftonline.com"
	}
	c.LoginBase = strings.TrimRight(c.LoginBase, "/")
	if c.Scope == "" {
		c.Scope = "https://graph.microsoft.com/.default"
	}
	if c.Method == "" {
		c.Method = "local"
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 10 * time.Second
	}
	if c.PollLookback <= 0 {
		c.PollLookback = time.Minute
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 15 * time.Second
	}
	if c.BotName == "" {
		c.BotName = "SnoozeBot"
	}
	return c, nil
}
