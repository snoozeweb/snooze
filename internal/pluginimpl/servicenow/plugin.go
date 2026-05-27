// Package servicenow implements the "servicenow" Notifier plugin: it opens
// (and optionally resolves) a ServiceNow incident via the Table REST API
// (https://developer.servicenow.com/dev.do#!/reference/api/sandiego/rest/c_TableAPI).
//
// Authentication: HTTP Basic (username:password).
// Create:  POST   {instance_url}/api/now/table/{table}           → HTTP 201
// Lookup:  GET    {instance_url}/api/now/table/{table}?sysparm_query=correlation_id={id}&sysparm_limit=1
// Resolve: PATCH  {instance_url}/api/now/table/{table}/{sys_id}  → HTTP 200
//
// When rec.State == "close" the plugin attempts to resolve the matching
// incident. If no incident is found for the given correlation_id the call
// is treated as a no-op (logged at info level) rather than an error.
//
// The plugin owns no database collection. PostInit stores the host;
// Reload is a no-op.
package servicenow

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	_ "embed"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("servicenow", metaYAML, factory)
}

// defaultTimeout is the per-request HTTP timeout when the action_form does
// not supply one — mirrors the webhook plugin baseline.
const defaultTimeout = 10 * time.Second

// maxResponseBytes caps how many bytes of an error response body we read for
// diagnostics.
const maxResponseBytes = 4 << 10

// factory builds the Plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the ServiceNow notifier.
//
// Concurrency: Send is safe for concurrent calls; the HTTP client is built
// per-call (same pattern as the webhook/slack plugins).
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is overridable from tests so httptest servers can intercept
	// outbound calls without proxy/TLS configuration.
	newClient func(timeout time.Duration) *http.Client
}

// Name returns the registry key. Hardcoded (like mail/slack) because the
// metadata.yaml `name:` carries the human display label, not the slug.
func (p *Plugin) Name() string { return "servicenow" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit stores the host reference. There is no DB collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin carries no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send creates or resolves a ServiceNow incident depending on rec.State.
//
//   - Default (firing): POST a new incident.
//   - rec.State == "close": look up the incident by correlation_id and
//     PATCH it to state 6 (Resolved). If no matching record exists the call
//     is a no-op.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("servicenow: config: %w", err)
	}

	if rec.State == "close" {
		return p.resolve(ctx, cfg, rec)
	}
	return p.create(ctx, cfg, rec)
}

// create POSTs a new incident to the ServiceNow Table API.
func (p *Plugin) create(ctx context.Context, cfg config, rec snoozetypes.Record) error {
	corrID := rec.Hash
	if corrID == "" {
		corrID = rec.UID
	}

	urgency := cfg.Urgency
	if urgency == "" || urgency == "auto" {
		urgency = severityToLevel(rec.Severity)
	}
	impact := cfg.Impact
	if impact == "" || impact == "auto" {
		impact = severityToLevel(rec.Severity)
	}

	incident := map[string]any{
		"short_description": rec.Message,
		"description":       buildDescription(rec),
		"urgency":           urgency,
		"impact":            impact,
		"correlation_id":    corrID,
	}
	if cfg.Category != "" {
		incident["category"] = cfg.Category
	}
	if cfg.CallerID != "" {
		incident["caller_id"] = cfg.CallerID
	}

	body, err := json.Marshal(incident)
	if err != nil {
		return fmt.Errorf("servicenow: marshal body: %w", err)
	}

	tableURL := cfg.InstanceURL + "/api/now/table/" + cfg.Table
	req, err := p.newRequest(ctx, cfg, http.MethodPost, tableURL, body)
	if err != nil {
		return fmt.Errorf("servicenow: build create request: %w", err)
	}

	resp, err := p.newClient(cfg.Timeout).Do(req)
	if err != nil {
		return fmt.Errorf("servicenow: create request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("servicenow: create: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// resolve looks up the incident by correlation_id and patches its state to 6
// (Resolved). If the GET returns zero results the operation is a no-op.
func (p *Plugin) resolve(ctx context.Context, cfg config, rec snoozetypes.Record) error {
	corrID := rec.Hash
	if corrID == "" {
		corrID = rec.UID
	}

	// Step 1: look up the incident sys_id.
	lookupURL := cfg.InstanceURL + "/api/now/table/" + cfg.Table +
		"?sysparm_query=correlation_id=" + url.QueryEscape(corrID) +
		"&sysparm_limit=1"

	req, err := p.newRequest(ctx, cfg, http.MethodGet, lookupURL, nil)
	if err != nil {
		return fmt.Errorf("servicenow: build lookup request: %w", err)
	}

	resp, err := p.newClient(cfg.Timeout).Do(req)
	if err != nil {
		return fmt.Errorf("servicenow: lookup request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("servicenow: lookup: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}

	var lookupResp struct {
		Result []struct {
			SysID string `json:"sys_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(preview, &lookupResp); err != nil {
		return fmt.Errorf("servicenow: decode lookup response: %w", err)
	}
	if len(lookupResp.Result) == 0 {
		// No matching record — log and no-op.
		if p.host != nil {
			if lg := p.host.Logger(); lg != nil {
				lg.Info("servicenow: resolve: no incident found for correlation_id, skipping",
					"correlation_id", corrID)
			}
		}
		return nil
	}

	sysID := lookupResp.Result[0].SysID

	// Step 2: PATCH the incident to Resolved (state=6).
	closeNotes := fmt.Sprintf("Resolved via Snooze (record %s)", rec.UID)
	patch := map[string]any{
		"state":       "6",
		"close_code":  "Resolved",
		"close_notes": closeNotes,
	}
	patchBody, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("servicenow: marshal patch body: %w", err)
	}

	patchURL := cfg.InstanceURL + "/api/now/table/" + cfg.Table + "/" + sysID
	patchReq, err := p.newRequest(ctx, cfg, http.MethodPatch, patchURL, patchBody)
	if err != nil {
		return fmt.Errorf("servicenow: build patch request: %w", err)
	}

	patchResp, err := p.newClient(cfg.Timeout).Do(patchReq)
	if err != nil {
		return fmt.Errorf("servicenow: patch request: %w", err)
	}
	defer patchResp.Body.Close() //nolint:errcheck

	patchPreview, _ := io.ReadAll(io.LimitReader(patchResp.Body, maxResponseBytes))
	if patchResp.StatusCode < 200 || patchResp.StatusCode >= 300 {
		return fmt.Errorf("servicenow: patch: HTTP %d: %s", patchResp.StatusCode, truncate(patchPreview, 200))
	}
	return nil
}

// newRequest builds an authenticated HTTP request with the JSON accept/content
// headers pre-set. The request is bound to ctx; the per-request deadline is
// enforced by the http.Client timeout set in newClient.
func (p *Plugin) newRequest(ctx context.Context, cfg config, method, rawURL string, body []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}

	creds := cfg.Username + ":" + cfg.Password
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(creds)))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// defaultClient returns a plain http.Client with the given deadline.
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// Compile-time proof that *Plugin satisfies the Notifier contract.
var _ plugins.Notifier = (*Plugin)(nil)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// config holds the per-action knobs decoded from payload.Meta.
type config struct {
	InstanceURL string
	Username    string
	Password    string
	Table       string
	Urgency     string // "auto" | "1" | "2" | "3"
	Impact      string // "auto" | "1" | "2" | "3"
	Category    string
	CallerID    string
	Timeout     time.Duration
}

// configFromMeta decodes config from the action_form Meta map.
// It fails if instance_url, username, or password are absent.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		Table:   "incident",
		Urgency: "auto",
		Impact:  "auto",
		Timeout: defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("instance_url is required")
	}

	cfg.InstanceURL = metaString(meta, "instance_url")
	cfg.Username = metaString(meta, "username")
	cfg.Password = metaString(meta, "password")

	if cfg.InstanceURL == "" {
		return cfg, fmt.Errorf("instance_url is required")
	}
	if cfg.Username == "" {
		return cfg, fmt.Errorf("username is required")
	}

	// Trim trailing slash so URL construction is consistent.
	cfg.InstanceURL = strings.TrimRight(cfg.InstanceURL, "/")

	if t := metaString(meta, "table"); t != "" {
		cfg.Table = t
	}
	if u := metaString(meta, "urgency"); u != "" {
		cfg.Urgency = u
	}
	if i := metaString(meta, "impact"); i != "" {
		cfg.Impact = i
	}
	cfg.Category = metaString(meta, "category")
	cfg.CallerID = metaString(meta, "caller_id")

	if to, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = to
	}

	return cfg, nil
}

// metaString reads key from m as a string; returns "" when absent or wrong type.
func metaString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// parseTimeout accepts a duration string, int/float64 seconds, or
// time.Duration. Returns (0, false) on anything else.
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// severityToLevel maps a Snooze severity string to a ServiceNow urgency/impact
// level string ("1"=High, "2"=Medium, "3"=Low).
//
//	critical/emergency → 1
//	error/err/warning  → 2
//	anything else      → 3
func severityToLevel(severity string) string {
	switch strings.ToLower(severity) {
	case "emergency", "critical":
		return "1"
	case "error", "err", "warning", "warn":
		return "2"
	default:
		return "3"
	}
}

// buildDescription assembles a multi-line incident description from the record
// fields most useful for an operator responding to the ticket.
func buildDescription(rec snoozetypes.Record) string {
	var sb strings.Builder
	sb.WriteString("Host:     " + rec.Host + "\n")
	sb.WriteString("Source:   " + rec.Source + "\n")
	sb.WriteString("Severity: " + rec.Severity + "\n")
	if rec.Process != "" {
		sb.WriteString("Process:  " + rec.Process + "\n")
	}
	sb.WriteString("Message:  " + rec.Message + "\n")
	if len(rec.Tags) > 0 {
		sb.WriteString("Tags:     " + strings.Join(rec.Tags, ", ") + "\n")
	}
	sb.WriteString("UID:      " + rec.UID + "\n")
	return sb.String()
}

// truncate returns at most n bytes of b as a string, with an ellipsis if
// the input was longer.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
