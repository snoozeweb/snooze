package cli

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ClientConfig mirrors the on-disk shape of /etc/snooze/client.yaml so an
// operator can put server + credentials in one place instead of repeating
// --server / --user / --password on every invocation.
//
// The shape matches the Snooze 1.x Python client's client.yaml so existing
// installs keep working without an edit:
//
//	server: https://snooze.egerie.eu
//	credentials:
//	  username: snooze
//	  password: ziH6...
//	method: local         # optional
//	insecure: false       # optional
//	timeout: 30s          # optional
//
// Every field is optional. Missing fields fall through to the CLI's own
// defaults (env vars first, then the hard-coded "http://localhost:9001",
// etc.). Precedence in the CLI is: flag > env var > config file > built-in.
type ClientConfig struct {
	Server      string                  `yaml:"server"`
	Credentials ClientConfigCredentials `yaml:"credentials"`
	Method      string                  `yaml:"method,omitempty"`
	Insecure    bool                    `yaml:"insecure,omitempty"`
	Timeout     time.Duration           `yaml:"timeout,omitempty"`
}

// ClientConfigCredentials groups the username/password pair under the
// `credentials:` key so it mirrors the 1.x shape on disk.
type ClientConfigCredentials struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// LoadClientConfig reads the first client config it finds, in priority
// order:
//
//  1. $SNOOZE_CONFIG (if set and pointing to a readable file).
//  2. ~/.config/snooze/client.yaml.
//  3. /etc/snooze/client.yaml.
//
// A missing file is not an error; it returns the zero value and the CLI
// falls back to its built-in defaults. Parse errors are also swallowed so
// a malformed file doesn't block invocations that pass everything via
// flags or env vars.
func LoadClientConfig() ClientConfig {
	if p := os.Getenv("SNOOZE_CONFIG"); p != "" {
		return readClientConfig(p)
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".config", "snooze", "client.yaml"),
		"/etc/snooze/client.yaml",
	}
	for _, p := range candidates {
		if home == "" && filepath.HasPrefix(p, home) {
			// UserHomeDir failed (sandbox / no $HOME); skip the
			// user-local candidate but keep /etc.
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return readClientConfig(p)
		}
	}
	return ClientConfig{}
}

// readClientConfig is the file-system half of LoadClientConfig. Split for
// tests that want to feed in arbitrary YAML without touching disk.
func readClientConfig(path string) ClientConfig {
	data, err := os.ReadFile(path) //nolint:gosec // operator-controlled path
	if err != nil {
		return ClientConfig{}
	}
	return parseClientConfig(data)
}

// parseClientConfig decodes YAML bytes into a ClientConfig, swallowing
// parse errors (an unreadable config file should not block a run that
// otherwise supplies everything via flags / env).
func parseClientConfig(data []byte) ClientConfig {
	var cfg ClientConfig
	_ = yaml.Unmarshal(data, &cfg)
	return cfg
}
