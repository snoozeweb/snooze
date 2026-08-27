// Package statuspage implements the "statuspage" Notifier plugin: it creates
// and resolves Atlassian Statuspage incidents when Snooze alerts fire or
// close. It uses the Statuspage REST API v1 (net/http only; no SDK).
//
// # Create flow
//
// A firing record (rec.State == "") POSTs to
//
//	POST {api_base}/v1/pages/{page_id}/incidents
//
// with Authorization: OAuth {api_key} and a JSON body shaped as:
//
//	{"incident": {"name": ..., "status": ..., "body": ..., "component_ids": [...], "components": {...}}}
//
// A 201 response is required; anything else is an error.
//
// # Resolve flow
//
// A closing record (rec.State == "close") resolves the most-recently-opened
// unresolved incident whose name matches the rendered name template:
//
//  1. GET {api_base}/v1/pages/{page_id}/incidents/unresolved
//  2. Walk the list (last-in-array wins) and find the first incident whose
//     "name" field equals the rendered template string.
//  3. PATCH {api_base}/v1/pages/{page_id}/incidents/{id} with
//     {"incident": {"status": "resolved", "body": ...}}.
//
// If no match is found, the operation is a logged no-op (not an error). This
// is a known limitation: Statuspage itself has no structured correlation key,
// so matching is done by incident name. Rename the incident on the Statuspage
// UI and the auto-resolve will no longer find it.
package statuspage

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"text/template"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("statuspage", metaYAML, factory)
}

// defaultTimeout matches the webhook/slack plugin baseline.
const defaultTimeout = 10 * time.Second

// defaultNameTmpl is the incident title template when the operator omits name.
const defaultNameTmpl = "{{ .Severity }}: {{ .Host }}"

// defaultBodyTmpl is the incident body template when the operator omits body.
const defaultBodyTmpl = "{{ .Message }}"

// defaultAPIBase is the Statuspage API base URL for the hosted service.
const defaultAPIBase = "https://api.statuspage.io"

// factory builds the plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the Statuspage notifier.
//
// Concurrency: Send is safe for concurrent calls. The HTTP client is built
// per-call (same pattern as the webhook/slack plugins).
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is the http.Client builder. Overridable from tests so that
	// httptest servers can intercept outbound calls without TLS.
	newClient func(timeout time.Duration) *http.Client
}

// Name returns the registry key used in the action_form and all.go.
func (p *Plugin) Name() string { return "statuspage" }

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

// Reload is a no-op: the plugin carries no cached state between calls.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send dispatches a Statuspage incident create (firing) or resolve (close).
// It reads all configuration from payload.Meta (the action_form values).
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("statuspage: config: %w", err)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	client := p.newClient(timeout)

	if rec.State == "close" {
		return p.resolveIncident(ctx, client, cfg, rec, timeout)
	}
	if payload.Escalation.IsRe() {
		return p.escalateIncident(ctx, client, cfg, rec, payload, timeout)
	}
	return p.createIncident(ctx, client, cfg, rec, timeout)
}

// escalateIncident posts a new incident UPDATE onto the incident this alert
// already has, instead of opening a second incident for the same problem.
//
// A public status page is the one place a duplicate is most visible: two open
// incidents for one outage tells customers the wrong story. Statuspage creates
// an incident update automatically for any PATCH that carries a `body`, so the
// escalation is recorded as a timeline entry on the existing incident.
//
// Correlation is by rendered name — Statuspage has no external-reference or
// dedup-key concept — which is the same (documented) mechanism the close path
// has always used.
func (p *Plugin) escalateIncident(
	ctx context.Context,
	client *http.Client,
	cfg config,
	rec snoozetypes.Record,
	payload plugins.NotificationPayload,
	timeout time.Duration,
) error {
	name, err := renderTemplate("name", cfg.NameTmpl, rec)
	if err != nil {
		return fmt.Errorf("statuspage: render name: %w", err)
	}
	body, err := renderTemplate("body", cfg.BodyTmpl, rec)
	if err != nil {
		return fmt.Errorf("statuspage: render body: %w", err)
	}

	id, status, err := p.findUnresolvedIncident(ctx, client, cfg, name, timeout)
	if err != nil {
		// A broken list call must not drop an escalating alert: a duplicate
		// incident is recoverable, a missed outage update is not.
		if lg := p.logger(); lg != nil {
			lg.Info("statuspage: escalate: incident lookup failed, creating instead",
				"name", name, "error", err)
		}
		return p.createIncident(ctx, client, cfg, rec, timeout)
	}
	if id == "" {
		// Nothing open under this name (resolved in between, or the first
		// delivery never landed), so a create is correct.
		return p.createIncident(ctx, client, cfg, rec, timeout)
	}

	incident := map[string]any{"body": escalationBody(body, payload.Escalation)}
	// Advance the lifecycle at most one step, and never backwards: an operator
	// who moved an incident to "monitoring" must not be dragged back to
	// "investigating" because the underlying alert flapped.
	//
	// Deliberately NOT touching `components`. Statuspage's component statuses
	// are a different vocabulary from incident statuses — operational,
	// degraded_performance, partial_outage, major_outage — so sending an
	// incident status there is rejected, and the rejection would take this
	// PATCH's escalation body down with it. The component keeps whatever status
	// the create set. (The create path has the same confusion; see the note on
	// createIncident.)
	if next := nextStatus(status); next != "" {
		incident["status"] = next
	}
	return p.patchIncident(ctx, client, cfg, id, incident, timeout)
}

// escalationBody prefixes the rendered body so the public timeline entry reads
// as an ongoing incident rather than a restatement of the first one.
//
// Statuspage is the ONLY output here that faces customers rather than
// operators, so the escalation ordinal and reason are deliberately left OUT:
// "Re-escalated #4 (timeout)" tells the public about Snooze's internal ack
// deadlines, which is nobody's business outside the on-call rota. Operators get
// the full detail on the ticketing and chat outputs; the public gets the fact
// that the incident is still live, timestamped by Statuspage itself.
func escalationBody(body string, _ plugins.Escalation) string {
	const prefix = "Re-escalated"
	if body == "" {
		return prefix
	}
	return prefix + ": " + body
}

// statusProgression is Statuspage's incident lifecycle. An escalation advances
// one step from "investigating" to "identified" and then stops: "monitoring"
// means someone believes the fix is in, and only they should move it on.
var statusProgression = map[string]string{
	"investigating": "identified",
}

// nextStatus returns the status an escalation should advance to, or "" to leave
// the incident's status untouched.
func nextStatus(current string) string { return statusProgression[current] }

// createIncident POSTs a new incident to Statuspage.
func (p *Plugin) createIncident(ctx context.Context, client *http.Client, cfg config, rec snoozetypes.Record, timeout time.Duration) error {
	name, err := renderTemplate("name", cfg.NameTmpl, rec)
	if err != nil {
		return fmt.Errorf("statuspage: render name: %w", err)
	}
	body, err := renderTemplate("body", cfg.BodyTmpl, rec)
	if err != nil {
		return fmt.Errorf("statuspage: render body: %w", err)
	}

	incident := map[string]any{
		"name":   name,
		"status": cfg.InitialStatus,
		"body":   body,
	}
	if cfg.ComponentID != "" {
		incident["component_ids"] = []string{cfg.ComponentID}
		// NOTE: `components` maps a component id to a COMPONENT status
		// (operational / degraded_performance / partial_outage / major_outage),
		// which is a different vocabulary from the incident status this sends.
		// Statuspage is expected to reject it, so a create with component_id
		// configured likely fails. Left as-is rather than guessed at: choosing
		// which component status a given severity implies is an operator
		// decision, and it needs a separate config field. Filed as a follow-up.
		incident["components"] = map[string]string{
			cfg.ComponentID: cfg.InitialStatus,
		}
	}
	if cfg.Impact != "" {
		incident["impact_override"] = cfg.Impact
	}

	reqBody, err := json.Marshal(map[string]any{"incident": incident})
	if err != nil {
		return fmt.Errorf("statuspage: marshal create body: %w", err)
	}

	url := cfg.APIBase + "/v1/pages/" + cfg.PageID + "/incidents"
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("statuspage: build create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "OAuth "+cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("statuspage: create request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("statuspage: create HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// resolveIncident looks up the most-recently-opened unresolved incident
// matching the rendered name template, then PATCHes it to resolved.
//
// Name-matching is the only available correlation mechanism because Statuspage
// has no external reference / dedup key concept; this is a known limitation.
func (p *Plugin) resolveIncident(ctx context.Context, client *http.Client, cfg config, rec snoozetypes.Record, timeout time.Duration) error {
	name, err := renderTemplate("name", cfg.NameTmpl, rec)
	if err != nil {
		return fmt.Errorf("statuspage: render name: %w", err)
	}
	body, err := renderTemplate("body", cfg.BodyTmpl, rec)
	if err != nil {
		return fmt.Errorf("statuspage: render body: %w", err)
	}

	id, _, err := p.findUnresolvedIncident(ctx, client, cfg, name, timeout)
	if err != nil {
		return err
	}
	if id == "" {
		// No match — log and return a no-op (not an error).
		if lg := p.logger(); lg != nil {
			lg.Info("statuspage: no unresolved incident matched name; skipping resolve",
				"name", name)
		}
		return nil
	}
	return p.patchIncident(ctx, client, cfg, id, map[string]any{
		"status": "resolved",
		"body":   body,
	}, timeout)
}

// findUnresolvedIncident returns the id and current status of the
// most-recently-opened unresolved incident whose name matches, or an empty id
// when none does (which is not an error).
//
// The API returns newest-first; the slice is scanned from the end so the
// most-recently-created entry wins when several incidents share a name.
func (p *Plugin) findUnresolvedIncident(
	ctx context.Context,
	client *http.Client,
	cfg config,
	name string,
	timeout time.Duration,
) (id, status string, err error) {
	listURL := cfg.APIBase + "/v1/pages/" + cfg.PageID + "/incidents/unresolved"
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, listURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("statuspage: build list request: %w", err)
	}
	req.Header.Set("Authorization", "OAuth "+cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("statuspage: list request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	rawList, _ := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("statuspage: list HTTP %d: %s", resp.StatusCode, truncate(rawList, 200))
	}

	var incidents []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rawList, &incidents); err != nil {
		return "", "", fmt.Errorf("statuspage: decode incident list: %w", err)
	}
	for i := len(incidents) - 1; i >= 0; i-- {
		if incidents[i].Name == name {
			return incidents[i].ID, incidents[i].Status, nil
		}
	}
	return "", "", nil
}

// patchIncident PATCHes the given incident fields, wrapped in the `incident`
// envelope the API expects. A body field makes Statuspage record a new incident
// update on the public timeline.
func (p *Plugin) patchIncident(
	ctx context.Context,
	client *http.Client,
	cfg config,
	id string,
	incident map[string]any,
	timeout time.Duration,
) error {
	payload, err := json.Marshal(map[string]any{"incident": incident})
	if err != nil {
		return fmt.Errorf("statuspage: marshal patch body: %w", err)
	}

	patchURL := cfg.APIBase + "/v1/pages/" + cfg.PageID + "/incidents/" + id
	patchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(patchCtx, http.MethodPatch, patchURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("statuspage: build patch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "OAuth "+cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("statuspage: patch request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("statuspage: patch HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	return nil
}

// logger returns the host's slog.Logger, or nil when the host is not set
// (e.g. in unit tests that don't call PostInit).
func (p *Plugin) logger() interface {
	Info(msg string, args ...any)
} {
	if p.host == nil {
		return nil
	}
	return p.host.Logger()
}

// defaultClient returns a plain http.Client with the given timeout.
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
	APIKey        string
	PageID        string
	ComponentID   string
	InitialStatus string
	NameTmpl      string
	BodyTmpl      string
	Impact        string
	APIBase       string
	Timeout       time.Duration
}

// configFromMeta decodes config from the action_form Meta map.
// api_key and page_id are required; everything else has a sane default.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		InitialStatus: "investigating",
		NameTmpl:      defaultNameTmpl,
		BodyTmpl:      defaultBodyTmpl,
		APIBase:       defaultAPIBase,
		Timeout:       defaultTimeout,
	}
	if meta == nil {
		return cfg, fmt.Errorf("api_key is required")
	}

	cfg.APIKey = metaString(meta, "api_key")
	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("api_key is required")
	}
	cfg.PageID = metaString(meta, "page_id")
	if cfg.PageID == "" {
		return cfg, fmt.Errorf("page_id is required")
	}

	if v := metaString(meta, "component_id"); v != "" {
		cfg.ComponentID = v
	}
	if v := metaString(meta, "initial_status"); v != "" {
		cfg.InitialStatus = v
	}
	if v := metaString(meta, "name"); v != "" {
		cfg.NameTmpl = v
	}
	if v := metaString(meta, "body"); v != "" {
		cfg.BodyTmpl = v
	}
	if v := metaString(meta, "impact"); v != "" {
		cfg.Impact = v
	}
	if v := metaString(meta, "api_base"); v != "" {
		cfg.APIBase = v
	}
	if t, ok := parseTimeout(meta["timeout"]); ok {
		cfg.Timeout = t
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
// Template rendering
// ---------------------------------------------------------------------------

// renderTemplate executes a Go text/template over the record's fields.
// If tmpl contains no {{ directives it is returned verbatim (fast-path).
func renderTemplate(name, tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	// Fast-path: no template directives.
	if len(tmpl) < 2 {
		return tmpl, nil
	}
	found := false
	for i := 0; i < len(tmpl)-1; i++ {
		if tmpl[i] == '{' && tmpl[i+1] == '{' {
			found = true
			break
		}
	}
	if !found {
		return tmpl, nil
	}
	t, err := template.New(name).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// truncate returns at most n bytes of b as a string, appending "..." when
// the input was longer. Used for error message snippets.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
