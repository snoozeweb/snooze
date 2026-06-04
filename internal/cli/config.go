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
//	method: local         # optional (1.x called this `auth_method` — also accepted)
//	insecure: false       # optional (1.x used `ca_bundle: false` — also accepted)
//	timeout: 30s          # optional
//
// Every field is optional. Missing fields fall through to the CLI's own
// defaults (env vars first, then the hard-coded "http://localhost:5200",
// etc.). Precedence in the CLI is: flag > env var > config file > built-in.
type ClientConfig struct {
	Server      string                  `yaml:"server"`
	Credentials ClientConfigCredentials `yaml:"credentials"`
	Method      string                  `yaml:"method,omitempty"`
	Insecure    bool                    `yaml:"insecure,omitempty"`
	Timeout     time.Duration           `yaml:"timeout,omitempty"`

	// Snooze 1.x key aliases. Held in their own fields so we can detect
	// presence and let the canonical keys win on collision; normalised
	// into Method / Insecure by normalize() right after Unmarshal.
	//
	//   auth_method   ↔ method
	//   ca_bundle:    ↔ insecure (Python's `ca_bundle: false` meant
	//                  "skip TLS verify"; any string value is a CA-path
	//                  the Go CLI cannot honour, so we ignore those.)
	AuthMethod string `yaml:"auth_method,omitempty"`
	CABundle   any    `yaml:"ca_bundle,omitempty"`
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
	var candidates []string
	if home != "" {
		// Only consider the user-local candidate when UserHomeDir succeeded;
		// when it fails (sandbox / no $HOME) the /etc fallback still applies.
		candidates = append(candidates, filepath.Join(home, ".config", "snooze", "client.yaml"))
	}
	candidates = append(candidates, "/etc/snooze/client.yaml")
	for _, p := range candidates {
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
// otherwise supplies everything via flags / env). It then normalises the
// Snooze 1.x key aliases so the rest of the CLI can read just Method /
// Insecure / Timeout without caring which spelling the file used.
func parseClientConfig(data []byte) ClientConfig {
	var cfg ClientConfig
	_ = yaml.Unmarshal(data, &cfg)
	cfg.normalize()
	return cfg
}

// normalize folds the Snooze 1.x key aliases into the canonical fields.
// Canonical keys win on collision (an operator who set both `method:` and
// `auth_method:` meant `method:`).
func (c *ClientConfig) normalize() {
	if c.Method == "" && c.AuthMethod != "" {
		c.Method = c.AuthMethod
	}
	if !c.Insecure {
		// Python 1.x convention: ca_bundle=false means "do not verify TLS".
		// Any other value (true, a string path) is silently ignored — the
		// Go CLI doesn't have a custom-CA-bundle knob yet, so it uses the
		// system trust store. Operators who set `ca_bundle: /path/...` on
		// 1.x should pass --insecure or add `insecure: true` for parity.
		if b, ok := c.CABundle.(bool); ok && !b {
			c.Insecure = true
		}
	}
}
