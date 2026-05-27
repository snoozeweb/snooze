// Package otlp implements the snooze-otlp daemon: an OpenTelemetry OTLP/HTTP
// (JSON) receiver that converts OTLP log records into Snooze v1 alerts posted
// to snooze-server via pkg/snoozeclient.
//
// The daemon exposes a tiny HTTP server that speaks the OTLP/HTTP protocol for
// the logs signal only:
//
//   - POST /v1/logs accepts an ExportLogsServiceRequest encoded as
//     JSON-over-Protobuf (Content-Type application/json), optionally
//     gzip-compressed (Content-Encoding gzip). Every logRecord becomes a
//     snoozetypes.Record.
//   - POST /v1/metrics is a documented no-op stub: it returns 200 with an empty
//     ExportMetricsServiceResponse but maps nothing. Metrics and traces are not
//     mapped in this version.
//
// gRPC OTLP and binary-Protobuf encoding are intentionally NOT supported — the
// receiver is HTTP + JSON only (stdlib, no otel/grpc/protobuf dependency). A
// Protobuf Content-Type therefore yields 415 Unsupported Media Type.
package otlp

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the YAML schema for /etc/snooze/otlp.yaml. It bundles the standard
// Snooze-client block (used to forward alerts) with the receiver's own knobs.
type Config struct {
	// Server is the Snooze base URL ("https://snooze.example.com"). Required —
	// the daemon forwards every mapped record to its alert API.
	Server string `yaml:"server"`

	// Username / Password authenticate against the Snooze v1 /login endpoint.
	// Required unless Token is supplied.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the auth backend on Snooze. Defaults to "local".
	Method string `yaml:"method"`

	// Token, when set, short-circuits the Snooze login flow and is used as the
	// bearer token directly.
	Token string `yaml:"token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// RequestTimeout caps a single Snooze alert POST. Defaults to 30s.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// Listen is the OTLP/HTTP bind address. Defaults to ":4318" — the IANA /
	// OpenTelemetry default port for OTLP/HTTP.
	Listen string `yaml:"listen"`

	// Debug enables debug-level logging.
	Debug bool `yaml:"debug"`
}

// LoadConfig reads a YAML file at path and returns a fully-defaulted Config.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied
	if err != nil {
		return Config{}, fmt.Errorf("otlp: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("otlp: parse config %q: %w", path, err)
	}
	return cfg.WithDefaults()
}

// WithDefaults fills in zero-value fields with documented defaults and
// validates the required fields. It returns a copy so callers can keep the
// original for diagnostics.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.Server) == "" {
		return c, fmt.Errorf("otlp: config.server is required")
	}
	if c.Method == "" {
		c.Method = "local"
	}
	if c.Listen == "" {
		c.Listen = ":4318"
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 30 * time.Second
	}
	return c, nil
}
