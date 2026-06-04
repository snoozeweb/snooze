// Package googlechat implements the snooze-googlechat daemon: a Google Chat
// bridge that consumes Chat events from a Pub/Sub subscription, applies the
// user's command (ack / close / comment / re-escalate / re-open / snooze)
// against the Snooze v1 API, and posts a reply back to the originating thread
// via the Chat REST API.
//
// NOTE: this rewrite is in progress. cmd/snooze-googlechat is still a stub
// (version subcommand only) and nothing imports this package outside its
// tests, so none of this code ships in a binary yet.
//
// Current pieces:
//
//   - config.go   — YAML config schema + defaults/validation.
//   - forward.go  — command parsing + dispatch against the Snooze v1 API
//     (Forwarder). Unit-tested; see forward_test.go.
//   - pubsub.go   — EventHandler callback type (Pub/Sub subscriber not yet
//     implemented).
//   - chat.go     — ChatSender interface (Chat REST sender not yet implemented).
//
// Still missing before the daemon can run: a daemon.go with New/Run wiring
// config → GCP clients → loop, mirroring internal/components/mattermost, and
// wiring cmd/snooze-googlechat to daemon.Main.
package googlechat

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the YAML schema for /etc/snooze/googlechat.yaml. Mirrors the Python
// component's knobs but only retains the fields the Go daemon actually consumes.
type Config struct {
	// Server is the Snooze base URL ("https://snooze.example.com/").
	Server string `yaml:"server"`

	// Username / Password authenticate against the v1 /login endpoint.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the auth backend. Empty defaults to "local".
	Method string `yaml:"method"`

	// Token, when set, short-circuits the login flow.
	Token string `yaml:"token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// GCPProject is the Google Cloud project that hosts the Pub/Sub topic.
	GCPProject string `yaml:"gcp_project"`

	// PubSubSubscription is the bare subscription ID (no projects/.../subscriptions/
	// prefix). The daemon prepends the project automatically.
	PubSubSubscription string `yaml:"pubsub_subscription"`

	// ServiceAccountKey is the absolute path to a GCP service-account JSON key.
	// When empty the GCP libs fall back to Application Default Credentials.
	ServiceAccountKey string `yaml:"service_account_key"`

	// BotName is the bot's display name. Used in help-text rendering.
	BotName string `yaml:"bot_name"`

	// SnoozeURL is the public URL embedded in reply links. Defaults to Server.
	SnoozeURL string `yaml:"snooze_url"`

	// RequestTimeout caps an outbound Snooze HTTP request. Defaults to 10s.
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// LoadConfig reads a YAML config file at path and returns the parsed Config
// with defaults applied.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator-controlled
	if err != nil {
		return Config{}, fmt.Errorf("googlechat: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("googlechat: parse config %q: %w", path, err)
	}
	return cfg.WithDefaults()
}

// WithDefaults fills in zero-value fields with the documented defaults and
// validates the minimum-required set.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.Server) == "" {
		return c, fmt.Errorf("googlechat: config.server is required")
	}
	if strings.TrimSpace(c.GCPProject) == "" {
		return c, fmt.Errorf("googlechat: config.gcp_project is required")
	}
	if strings.TrimSpace(c.PubSubSubscription) == "" {
		return c, fmt.Errorf("googlechat: config.pubsub_subscription is required")
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 10 * time.Second
	}
	if c.BotName == "" {
		c.BotName = "Snooze"
	}
	if c.SnoozeURL == "" {
		c.SnoozeURL = strings.TrimRight(c.Server, "/")
	} else {
		c.SnoozeURL = strings.TrimRight(c.SnoozeURL, "/")
	}
	return c, nil
}
