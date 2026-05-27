// Package pagerduty implements the "pagerduty" Notifier plugin: it forwards
// Snooze alerts to PagerDuty via the Events API v2 (POST /v2/enqueue).
//
// Trigger/resolve lifecycle: when rec.State == "close" the plugin sends an
// "resolve" event action; otherwise it sends "trigger". The dedup_key uses
// rec.Hash when populated (set by aggregaterule), falling back to rec.UID, so
// a later resolve correlates correctly to the original trigger in PagerDuty.
//
// Severity mapping (Snooze → PagerDuty):
//
//	emergency, critical → critical
//	error, err          → error
//	warning             → warning
//	notice, info, debug → info
//	(unknown)           → critical for trigger, info for resolve
//
// The plugin owns no database collection. PostInit stores the host; Reload is
// a no-op.
package pagerduty

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
	"unicode/utf8"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("pagerduty", metaYAML, factory)
}

// defaultTimeout matches the Go port baseline used by the webhook plugin.
const defaultTimeout = 10 * time.Second

// maxSummaryRunes is the PagerDuty Events API v2 limit for payload.summary.
const maxSummaryRunes = 1024

// maxResponseBytes caps how many bytes we read from PagerDuty for diagnostics.
const maxResponseBytes = 8 << 10

// Plugin is the PagerDuty notifier.
//
// Concurrency: Send is safe for concurrent calls. A fresh http.Client is
// built per call because per-action timeout knobs may differ between
// invocations.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. Tests replace it with a function
	// that returns a client aimed at an httptest server.
	newClient func(timeout time.Duration) *http.Client
}

// factory builds the plugin instance with the production http.Client builder.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "pagerduty" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit stores the host. There is no DB collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin has no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send dispatches one trigger or resolve event to PagerDuty. The per-action
// knobs are read from payload.Meta (populated from the action_form values by
// the notification dispatcher).
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("pagerduty: config: %w", err)
	}

	body, err := buildEvent(cfg, rec)
	if err != nil {
		return fmt.Errorf("pagerduty: build event: %w", err)
	}

	endpoint := strings.TrimRight(cfg.APIBase, "/") + "/v2/enqueue"

	reqCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := p.newClient(cfg.Timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	// Events API v2 returns 202 Accepted on success.
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("pagerduty: HTTP %d: %s", resp.StatusCode, truncate(preview, 400))
	}
	return nil
}

// defaultClient returns an http.Client with the given timeout. It uses the
// default transport (TLS-verified, standard dial).
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// Compile-time proof we satisfy the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)

// ---------------------------------------------------------------------------
// Event construction
// ---------------------------------------------------------------------------

// pdEvent is the JSON body sent to POST /v2/enqueue.
type pdEvent struct {
	RoutingKey  string    `json:"routing_key"`
	EventAction string    `json:"event_action"`
	DedupKey    string    `json:"dedup_key,omitempty"`
	Payload     pdPayload `json:"payload"`
	Client      string    `json:"client,omitempty"`
	ClientURL   string    `json:"client_url,omitempty"`
}

// pdPayload is the nested payload object inside a PagerDuty event.
type pdPayload struct {
	Summary       string         `json:"summary"`
	Source        string         `json:"source"`
	Severity      string         `json:"severity"`
	Timestamp     string         `json:"timestamp,omitempty"`
	Component     string         `json:"component,omitempty"`
	Group         string         `json:"group,omitempty"`
	Class         string         `json:"class,omitempty"`
	CustomDetails map[string]any `json:"custom_details,omitempty"`
}

// buildEvent constructs and marshals the PagerDuty JSON body from cfg and rec.
func buildEvent(cfg config, rec snoozetypes.Record) ([]byte, error) {
	action := "trigger"
	if rec.State == "close" {
		action = "resolve"
	}

	dedupKey := rec.Hash
	if dedupKey == "" {
		dedupKey = rec.UID
	}

	sev := cfg.Severity
	if sev == "" || sev == "auto" {
		sev = mapSeverity(rec.Severity, action)
	}

	source := rec.Host
	if source == "" {
		source = rec.Source
	}

	summary := truncateRunes(
		fmt.Sprintf("%s on %s: %s", rec.Severity, source, rec.Message),
		maxSummaryRunes,
	)

	evt := pdEvent{
		RoutingKey:  cfg.RoutingKey,
		EventAction: action,
		DedupKey:    dedupKey,
		Client:      cfg.Client,
		ClientURL:   cfg.ClientURL,
		Payload: pdPayload{
			Summary:       summary,
			Source:        source,
			Severity:      sev,
			CustomDetails: customDetails(rec),
		},
	}
	if !rec.Timestamp.IsZero() {
		evt.Payload.Timestamp = rec.Timestamp.UTC().Format(time.RFC3339)
	}
	if rec.Process != "" {
		evt.Payload.Component = rec.Process
	}
	if rec.Environment != "" {
		evt.Payload.Group = rec.Environment
	}

	return json.Marshal(evt)
}

// mapSeverity maps a Snooze syslog-style severity to a PagerDuty severity.
// PagerDuty accepts exactly: critical | error | warning | info.
func mapSeverity(snoozeSev, action string) string {
	switch strings.ToLower(snoozeSev) {
	case "emergency", "critical":
		return "critical"
	case "error", "err":
		return "error"
	case "warning":
		return "warning"
	case "notice", "info", "debug":
		return "info"
	default:
		// Unknown severity: default to critical for a trigger (call for
		// attention), info for a resolve (benign close event).
		if action == "resolve" {
			return "info"
		}
		return "critical"
	}
}

// customDetails builds a compact map of the record's notable fields so PagerDuty
// rules and responders can filter on them. We omit large or redundant fields
// (message/host/severity are already in payload.summary) and skip zero values.
func customDetails(rec snoozetypes.Record) map[string]any {
	m := make(map[string]any, 8)
	if rec.UID != "" {
		m["uid"] = rec.UID
	}
	if rec.Source != "" {
		m["source"] = rec.Source
	}
	if rec.Process != "" {
		m["process"] = rec.Process
	}
	if rec.Environment != "" {
		m["environment"] = rec.Environment
	}
	if rec.Hash != "" {
		m["hash"] = rec.Hash
	}
	if len(rec.Tags) > 0 {
		m["tags"] = rec.Tags
	}
	if len(rec.Raw) > 0 {
		m["raw"] = rec.Raw
	}
	return m
}

// truncateRunes truncates s to at most n Unicode code points, appending "…"
// when truncation occurs. PagerDuty rejects summaries longer than 1024 chars.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// truncate returns at most n bytes of b as a string, appending "..." when the
// source was longer. Used for embedding error bodies in error messages.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// config captures the per-action knobs from NotificationPayload.Meta.
type config struct {
	RoutingKey string
	Severity   string
	Client     string
	ClientURL  string
	APIBase    string
	Timeout    time.Duration
}

// configFromMeta decodes config from a NotificationPayload.Meta map. The map
// contains action_form values: strings, floats, bools. Missing optional fields
// fall back to defaults; routing_key is required.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		Client:  "Snooze",
		APIBase: "https://events.pagerduty.com",
		Timeout: defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("routing_key is required")
	}

	if v, ok := meta["routing_key"].(string); ok && v != "" {
		cfg.RoutingKey = v
	}
	if cfg.RoutingKey == "" {
		return cfg, fmt.Errorf("routing_key is required")
	}

	if v, ok := meta["severity"].(string); ok && v != "" {
		cfg.Severity = v
	}
	if v, ok := meta["client"].(string); ok && v != "" {
		cfg.Client = v
	}
	if v, ok := meta["client_url"].(string); ok {
		cfg.ClientURL = v
	}
	if v, ok := meta["api_base"].(string); ok && v != "" {
		cfg.APIBase = v
	}
	if d, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = d
	}

	return cfg, nil
}

// parseTimeout accepts a duration string ("10s"), an int/float64 number of
// seconds, or a time.Duration. Anything else yields (0, false).
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
