// Package smtp implements the snooze-smtp daemon: a small SMTP server that
// receives mail and converts every accepted message into a snoozetypes.Record
// posted to the Snooze v1 alert API via pkg/snoozeclient.
//
// The daemon is intentionally narrow in scope. It speaks just enough of
// RFC 5321 to accept a single envelope per connection (HELO/EHLO, MAIL FROM,
// RCPT TO, DATA, QUIT) and parses the DATA blob with net/mail. Optional
// extensions (STARTTLS, AUTH PLAIN) are advertised only when TLS material
// and credentials are configured, respectively.
//
// Config is a flat YAML file consumed by cmd/snooze-smtp; see Config below.
package smtp

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the YAML schema for /etc/snooze/smtp.yaml. It is intentionally
// minimal — the daemon is a single-purpose forwarder, not a full MTA.
type Config struct {
	// Server is the Snooze base URL ("https://snooze.example.com/"). Required.
	Server string `yaml:"server"`

	// Username and Password authenticate against the v1 /login endpoint. They
	// are also used to authenticate INBOUND SMTP clients when AuthRequired is
	// set (see below) — sharing the same credentials keeps deployment simple.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the auth backend. Empty defaults to "local".
	Method string `yaml:"method"`

	// Token, when set, short-circuits the login flow and is used as the bearer
	// token directly.
	Token string `yaml:"token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// Listen is the SMTP bind address. Defaults to "0.0.0.0:25".
	Listen string `yaml:"listen"`

	// TLSCert / TLSKey enable STARTTLS advertisement when both are set. The
	// daemon does NOT serve implicit TLS (SMTPS / port 465) — most relays use
	// STARTTLS on 25/587 anyway and the implementation is simpler for it.
	TLSCert string `yaml:"tls_cert"`
	TLSKey  string `yaml:"tls_key"`

	// AllowedSenders is an allowlist of glob-style sender patterns matched
	// against the MAIL FROM address. "*" (the default) accepts everything.
	// Patterns support a single trailing "*" wildcard for domain matches,
	// e.g. ["alerts@example.com", "*@monitoring.example.com"].
	AllowedSenders []string `yaml:"allowed_senders"`

	// AuthRequired forces inbound clients to present AUTH PLAIN credentials
	// that match Username/Password before MAIL FROM is accepted. Off by
	// default — most relays send unauthenticated mail to a private inbox.
	AuthRequired bool `yaml:"auth_required"`

	// LocalDomains is the list of "owned" mail domains. When MAIL FROM is
	// user@host.<localdomain>, the daemon strips the localdomain off and uses
	// the short hostname as Record.Host (FQDN is preserved in Raw). Empty
	// means "treat everything as fully-qualified".
	LocalDomains []string `yaml:"local_domains"`

	// MaxMessageBytes caps the DATA section size. Defaults to 10 MiB. Senders
	// trying to exceed the cap receive a 552 reply.
	MaxMessageBytes int64 `yaml:"max_message_bytes"`

	// RequestTimeout caps a single PostAlert HTTP request. Defaults to 10s.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// ReadTimeout is the per-client SMTP read deadline. Defaults to 60s.
	ReadTimeout time.Duration `yaml:"read_timeout"`

	// WriteTimeout is the per-client SMTP write deadline. Defaults to 60s.
	WriteTimeout time.Duration `yaml:"write_timeout"`

	// Hostname is announced in the SMTP banner and HELO/EHLO response.
	// Defaults to os.Hostname() with a "snooze-smtp" fallback.
	Hostname string `yaml:"hostname"`
}

// LoadConfig reads a YAML config file at path and returns the parsed Config
// with reasonable defaults applied.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("smtp: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("smtp: parse config %q: %w", path, err)
	}
	return cfg.WithDefaults()
}

// WithDefaults fills in zero-value fields with the documented defaults and
// validates required fields.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.Server) == "" {
		return c, fmt.Errorf("smtp: config.server is required")
	}
	if c.Listen == "" {
		c.Listen = "0.0.0.0:25"
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 10 * time.Second
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 60 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 60 * time.Second
	}
	if c.MaxMessageBytes <= 0 {
		c.MaxMessageBytes = 10 * 1024 * 1024 // 10 MiB
	}
	if len(c.AllowedSenders) == 0 {
		c.AllowedSenders = []string{"*"}
	}
	if c.Hostname == "" {
		if h, err := os.Hostname(); err == nil && h != "" {
			c.Hostname = h
		} else {
			c.Hostname = "snooze-smtp"
		}
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return c, fmt.Errorf("smtp: tls_cert and tls_key must both be set or both empty")
	}
	if c.AuthRequired && c.Username == "" {
		return c, fmt.Errorf("smtp: auth_required=true needs a username")
	}
	return c, nil
}

// senderAllowed reports whether MAIL FROM addr matches one of the patterns
// in AllowedSenders. The match is case-insensitive. Supported pattern forms:
//
//   - "*"                              — wildcard, accepts any sender
//   - "user@host"                      — exact match
//   - "*@host.domain"                  — any user on host.domain
//   - "*@*.domain"                     — any user on any host of domain
func (c Config) senderAllowed(addr string) bool {
	addr = strings.ToLower(strings.TrimSpace(addr))
	for _, pat := range c.AllowedSenders {
		if matchSenderPattern(strings.ToLower(strings.TrimSpace(pat)), addr) {
			return true
		}
	}
	return false
}

// matchSenderPattern implements the small glob grammar documented on
// Config.AllowedSenders. It is exported for unit tests in this package.
func matchSenderPattern(pat, addr string) bool {
	if pat == "" {
		return false
	}
	if pat == "*" {
		return true
	}
	if pat == addr {
		return true
	}
	// "*@something" — match any local-part against the right-hand-side.
	if strings.HasPrefix(pat, "*@") {
		rhs := pat[2:]
		_, addrDomain, ok := splitAddress(addr)
		if !ok {
			return false
		}
		return matchDomain(rhs, addrDomain)
	}
	return false
}

// splitAddress splits "local@domain" into its two parts. Returns ok=false
// when the address doesn't contain exactly one '@'.
func splitAddress(addr string) (local, domain string, ok bool) {
	i := strings.IndexByte(addr, '@')
	if i < 0 || i == len(addr)-1 {
		return "", "", false
	}
	return addr[:i], addr[i+1:], true
}

// matchDomain matches a domain pattern against a candidate. Patterns may
// start with "*." to mean "any subdomain of the rest" — e.g. "*.example.com"
// matches "host.example.com" and "a.b.example.com" but not "example.com".
func matchDomain(pat, domain string) bool {
	if pat == domain {
		return true
	}
	if strings.HasPrefix(pat, "*.") {
		suffix := pat[1:] // ".example.com"
		return strings.HasSuffix(domain, suffix) && len(domain) > len(suffix)
	}
	return false
}

// isLocalDomain reports whether domain is one of c.LocalDomains. Matching is
// case-insensitive and supports the "*.domain" wildcard form.
func (c Config) isLocalDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, ld := range c.LocalDomains {
		ld = strings.ToLower(strings.TrimSpace(ld))
		if matchDomain(ld, domain) || ld == domain {
			return true
		}
	}
	return false
}
