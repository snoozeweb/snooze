// Package opsgenie implements the "opsgenie" Notifier plugin: it creates or
// closes an Opsgenie alert via the Alert API v2.
//
// Create path (rec.State != "close"):
//
//	POST {api_base}/v2/alerts
//	Authorization: GenieKey <api_key>
//	Content-Type: application/json
//	{"message": ..., "alias": ..., "description": ..., "priority": ...,
//	 "source": ..., "tags": [...], "details": {"severity": ..., "host": ...}}
//	→ HTTP 202 Accepted
//
// Close path (rec.State == "close"):
//
//	POST {api_base}/v2/alerts/{alias}/close?identifierType=alias
//	Authorization: GenieKey <api_key>
//	Content-Type: application/json
//	{"source": ..., "note": "Closed by Snooze"}
//	→ HTTP 202 Accepted
//
// alias is rec.Hash when non-empty, otherwise rec.UID — used for both dedup
// and the close correlation.
//
// Region selector: "us" → https://api.opsgenie.com,
// "eu" → https://api.eu.opsgenie.com. An explicit api_base overrides both.
//
// Priority mapping (when priority == "auto"):
//
//	emergency, critical → P1
//	error, err          → P2
//	warning             → P3
//	notice              → P4
//	info, debug, ""     → P5
//
// The plugin owns no database collection. PostInit stores the host; Reload
// is a no-op. newClient is overridable from tests.
package opsgenie

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("opsgenie", metaYAML, factory)
}

// defaultTimeout matches the webhook plugin baseline.
const defaultTimeout = 10 * time.Second

// apiBaseUS and apiBaseEU are the canonical Opsgenie Alert API base URLs.
const (
	apiBaseUS = "https://api.opsgenie.com"
	apiBaseEU = "https://api.eu.opsgenie.com"
)

// maxResponseBytes caps the response body we read for error messages.
const maxResponseBytes = 64 << 10

// factory builds the Plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the Opsgenie Notifier.
//
// Concurrency: Send is safe for concurrent calls. The http.Client is built
// per-call so per-action timeout knobs take effect correctly.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is overridable from tests so httptest can intercept calls.
	newClient func(timeout time.Duration) *http.Client
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "opsgenie" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host. There is no DB collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin has no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send creates or closes an Opsgenie alert depending on rec.State.
//
// When rec.State == "close", a close request is POSTed to
// /v2/alerts/{alias}/close?identifierType=alias.
// For all other states, an alert-create request is POSTed to /v2/alerts.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("opsgenie: config: %w", err)
	}

	alias := rec.Hash
	if alias == "" {
		alias = rec.UID
	}

	client := p.newClient(cfg.Timeout)

	if rec.State == "close" {
		return p.sendClose(ctx, client, cfg, alias, rec)
	}
	return p.sendCreate(ctx, client, cfg, alias, rec)
}

// sendCreate POSTs a new alert to /v2/alerts.
func (p *Plugin) sendCreate(ctx context.Context, client *http.Client, cfg config, alias string, rec snoozetypes.Record) error {
	priority := cfg.Priority
	if priority == "auto" || priority == "" {
		priority = severityToPriority(rec.Severity)
	}

	body := createRequest{
		Message:     rec.Message,
		Alias:       alias,
		Description: fmt.Sprintf("[%s] %s on %s: %s", rec.Severity, rec.Source, rec.Host, rec.Message),
		Priority:    priority,
		Source:      cfg.Source,
		Details: map[string]string{
			"severity": rec.Severity,
			"host":     rec.Host,
			"uid":      rec.UID,
		},
	}
	if len(cfg.Tags) > 0 {
		body.Tags = cfg.Tags
	}

	return p.doPost(ctx, client, cfg.APIBase+"/v2/alerts", cfg.APIKey, body)
}

// sendClose POSTs a close request to /v2/alerts/{alias}/close.
func (p *Plugin) sendClose(ctx context.Context, client *http.Client, cfg config, alias string, rec snoozetypes.Record) error {
	closeBody := closeRequest{
		Source: cfg.Source,
		Note:   fmt.Sprintf("Closed by Snooze (uid=%s)", rec.UID),
	}
	u := cfg.APIBase + "/v2/alerts/" + alias + "/close?identifierType=alias"
	return p.doPost(ctx, client, u, cfg.APIKey, closeBody)
}

// doPost marshals body to JSON and POSTs it to url with a GenieKey auth
// header. Returns nil on HTTP 2xx; an error with the status code and
// truncated body otherwise.
func (p *Plugin) doPost(ctx context.Context, client *http.Client, url, apiKey string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("opsgenie: marshal request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("opsgenie: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "GenieKey "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("opsgenie: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("opsgenie: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// createRequest is the JSON body for POST /v2/alerts.
type createRequest struct {
	Message     string            `json:"message"`
	Alias       string            `json:"alias"`
	Description string            `json:"description,omitempty"`
	Priority    string            `json:"priority,omitempty"`
	Source      string            `json:"source,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Details     map[string]string `json:"details,omitempty"`
}

// closeRequest is the JSON body for POST /v2/alerts/{alias}/close.
type closeRequest struct {
	Source string `json:"source,omitempty"`
	Note   string `json:"note,omitempty"`
}

// config holds the per-action knobs decoded from payload.Meta.
type config struct {
	APIKey   string
	APIBase  string
	Source   string
	Tags     []string
	Priority string
	Timeout  time.Duration
}

// configFromMeta decodes a config from the NotificationPayload.Meta map.
// Fails if api_key is missing.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		Source:   "Snooze",
		Priority: "auto",
		Timeout:  defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("api_key is required")
	}

	apiKey, _ := meta["api_key"].(string)
	if strings.TrimSpace(apiKey) == "" {
		return cfg, fmt.Errorf("api_key is required")
	}
	cfg.APIKey = apiKey

	if v, ok := meta["source"].(string); ok && v != "" {
		cfg.Source = v
	}

	if v, ok := meta["priority"].(string); ok && v != "" {
		cfg.Priority = v
	}

	// tags: optional comma-separated string
	if v, ok := meta["tags"].(string); ok && strings.TrimSpace(v) != "" {
		for _, tag := range strings.Split(v, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				cfg.Tags = append(cfg.Tags, tag)
			}
		}
	}

	// api_base overrides region
	if v, ok := meta["api_base"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.APIBase = strings.TrimRight(strings.TrimSpace(v), "/")
	} else {
		region, _ := meta["region"].(string)
		switch strings.ToLower(region) {
		case "eu":
			cfg.APIBase = apiBaseEU
		default:
			cfg.APIBase = apiBaseUS
		}
	}

	if t, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = t
	}

	return cfg, nil
}

// severityToPriority maps Snooze severity strings to Opsgenie priorities.
func severityToPriority(severity string) string {
	switch strings.ToLower(severity) {
	case "emergency", "critical":
		return "P1"
	case "error", "err":
		return "P2"
	case "warning":
		return "P3"
	case "notice":
		return "P4"
	default: // info, debug, ""
		return "P5"
	}
}

// defaultClient returns an http.Client honouring timeout.
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// parseTimeout accepts a duration string, a number of seconds (int or
// float64), or a time.Duration. Anything else yields (0, false).
func parseTimeout(v any) (time.Duration, bool) {
	switch x := v.(type) {
	case time.Duration:
		if x > 0 {
			return x, true
		}
	case string:
		d, err := time.ParseDuration(x)
		if err == nil && d > 0 {
			return d, true
		}
	case int:
		if x > 0 {
			return time.Duration(x) * time.Second, true
		}
	case int64:
		if x > 0 {
			return time.Duration(x) * time.Second, true
		}
	case float64:
		if x > 0 {
			return time.Duration(x * float64(time.Second)), true
		}
	}
	return 0, false
}

// truncate returns at most n bytes of b as a string, with an ellipsis appended
// if the input was longer.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// Compile-time proof that Plugin satisfies the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)
