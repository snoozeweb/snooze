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
//   - rec.State == "close": look up the incident by correlation_id and
//     PATCH it to state 6 (Resolved). If no matching record exists the call
//     is a no-op.
//   - A re-escalation (payload.Escalation.IsRe()): look up the incident by
//     correlation_id and PATCH it — work notes, an urgency/impact bump when
//     severity rose, and a reopen when it had been resolved. Only when nothing
//     matches does it fall through to a create.
//   - Otherwise (first delivery): POST a new incident.
//
// The correlation_id has always made this plugin idempotent in principle — it
// is derived from the alert hash, so ServiceNow could correlate the duplicates
// itself. What was missing was using that on the way IN: before this, every
// escalation POSTed a brand-new incident and left an operator with a queue of
// identical tickets for one problem.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("servicenow: config: %w", err)
	}

	if rec.State == "close" {
		return p.resolve(ctx, cfg, rec)
	}
	if payload.Escalation.IsRe() {
		return p.escalate(ctx, cfg, rec, payload)
	}
	return p.create(ctx, cfg, rec)
}

// escalate updates the incident this alert already has instead of opening a
// second one.
//
// A lookup failure deliberately degrades to a create rather than dropping the
// notification: an operator would rather see a duplicate ticket than miss an
// escalating alert because ServiceNow's query API was briefly unavailable.
func (p *Plugin) escalate(ctx context.Context, cfg config, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	sysID, state, err := p.findIncident(ctx, cfg, correlationID(rec))
	if err != nil {
		p.logWarn("servicenow: escalate: lookup failed, falling back to a create",
			"correlation_id", correlationID(rec), "error", err)
		return p.create(ctx, cfg, rec)
	}
	if sysID == "" {
		// Nothing to update (the incident was deleted, or this alert's first
		// delivery never reached ServiceNow), so a create is correct.
		return p.create(ctx, cfg, rec)
	}

	patch := map[string]any{
		"work_notes": buildEscalationNote(rec, payload.Escalation),
	}
	// Only raise urgency/impact when severity actually rose — re-escalating at
	// the same severity must not silently promote a ticket someone downgraded.
	if payload.Escalation.SeverityRose() {
		level := severityToLevel(rec.Severity)
		if cfg.Urgency == "" || cfg.Urgency == "auto" {
			patch["urgency"] = level
		}
		if cfg.Impact == "" || cfg.Impact == "auto" {
			patch["impact"] = level
		}
	}
	// An alert that is escalating again is not resolved: pull a Resolved (6) or
	// Closed (7) incident back to In Progress (2).
	if state == "6" || state == "7" {
		patch["state"] = "2"
	}

	return p.patchIncident(ctx, cfg, sysID, patch)
}

// buildEscalationNote renders the work-note body appended to the existing
// incident on each re-escalation.
func buildEscalationNote(rec snoozetypes.Record, esc plugins.Escalation) string {
	var b strings.Builder
	b.WriteString("Re-escalated by Snooze")
	if o := esc.Ordinal(); o != "" {
		b.WriteString(" (" + o + ")")
	}
	if esc.Reason != "" {
		b.WriteString(", reason: " + esc.Reason)
	}
	if esc.Actor != "" {
		b.WriteString(", by: " + esc.Actor)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Severity: %s", rec.Severity)
	if esc.PreviousSeverity != "" && esc.PreviousSeverity != rec.Severity {
		fmt.Fprintf(&b, " (was %s)", esc.PreviousSeverity)
	}
	fmt.Fprintf(&b, "\nHost: %s\nMessage: %s", rec.Host, rec.Message)
	return b.String()
}

// correlationID is the stable per-alert key both the create and the lookup
// paths use: the aggregaterule hash, falling back to the record uid.
func correlationID(rec snoozetypes.Record) string {
	if rec.Hash != "" {
		return rec.Hash
	}
	return rec.UID
}

// findIncident resolves the incident carrying corrID, returning its sys_id and
// current state. An empty sys_id means "no such incident" and is not an error.
func (p *Plugin) findIncident(ctx context.Context, cfg config, corrID string) (sysID, state string, err error) {
	lookupURL := cfg.InstanceURL + "/api/now/table/" + cfg.Table +
		"?sysparm_query=correlation_id=" + url.QueryEscape(corrID) +
		"&sysparm_fields=sys_id,state&sysparm_limit=1"

	req, err := p.newRequest(ctx, cfg, http.MethodGet, lookupURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build lookup request: %w", err)
	}
	resp, err := p.newClient(cfg.Timeout).Do(req)
	if err != nil {
		return "", "", fmt.Errorf("lookup request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("lookup: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	var lookupResp struct {
		Result []struct {
			SysID string `json:"sys_id"`
			State string `json:"state"`
		} `json:"result"`
	}
	if err := json.Unmarshal(preview, &lookupResp); err != nil {
		return "", "", fmt.Errorf("decode lookup response: %w", err)
	}
	if len(lookupResp.Result) == 0 {
		return "", "", nil
	}
	return lookupResp.Result[0].SysID, lookupResp.Result[0].State, nil
}

// patchIncident PATCHes fields onto the incident identified by sysID.
func (p *Plugin) patchIncident(ctx context.Context, cfg config, sysID string, fields map[string]any) error {
	body, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("servicenow: marshal patch body: %w", err)
	}
	patchURL := cfg.InstanceURL + "/api/now/table/" + cfg.Table + "/" + sysID
	req, err := p.newRequest(ctx, cfg, http.MethodPatch, patchURL, body)
	if err != nil {
		return fmt.Errorf("servicenow: build patch request: %w", err)
	}
	resp, err := p.newClient(cfg.Timeout).Do(req)
	if err != nil {
		return fmt.Errorf("servicenow: patch request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("servicenow: patch: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// logWarn logs at warn level when the host exposes a logger.
func (p *Plugin) logWarn(msg string, args ...any) {
	if p.host == nil {
		return
	}
	if lg := p.host.Logger(); lg != nil {
		lg.Warn(msg, args...)
	}
}

// create POSTs a new incident to the ServiceNow Table API.
func (p *Plugin) create(ctx context.Context, cfg config, rec snoozetypes.Record) error {
	corrID := correlationID(rec)

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
// (Resolved). A missing incident is a no-op.
func (p *Plugin) resolve(ctx context.Context, cfg config, rec snoozetypes.Record) error {
	corrID := correlationID(rec)
	sysID, _, err := p.findIncident(ctx, cfg, corrID)
	if err != nil {
		return fmt.Errorf("servicenow: %w", err)
	}
	if sysID == "" {
		if p.host != nil {
			if lg := p.host.Logger(); lg != nil {
				lg.Info("servicenow: resolve: no incident found for correlation_id, skipping",
					"correlation_id", corrID)
			}
		}
		return nil
	}
	return p.patchIncident(ctx, cfg, sysID, map[string]any{
		"state":       "6",
		"close_code":  "Resolved",
		"close_notes": fmt.Sprintf("Resolved via Snooze (record %s)", rec.UID),
	})
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
