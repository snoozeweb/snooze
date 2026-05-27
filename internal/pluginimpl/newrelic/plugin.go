// Package newrelic implements the `newrelic` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts both the
// recommended New Relic workflow/notification webhook shape and the legacy
// default condition webhook shape. Each incoming payload is mapped to a
// snoozetypes.Record and submitted to the host's processing pipeline.
//
// # Workflow webhook (recommended)
//
// New Relic's "Notifications" → "Workflows" feature lets operators configure
// webhook destinations with a fully-templated JSON body. The recommended
// template (documented in the integration's RST page) produces:
//
//	{
//	  "id": "...",
//	  "issueUrl": "...",
//	  "title": "...",
//	  "priority": "CRITICAL"|"HIGH"|"MEDIUM"|"LOW",
//	  "state": "ACTIVATED"|"CLOSED"|"CREATED",
//	  "trigger": "...",
//	  "timestamp": 1234567890000,
//	  "accountName": "...",
//	  "totalIncidents": 1,
//	  "owner": "...",
//	  "impactedEntities": ["entity-name", ...],
//	  "labels": { "key": "value", ... }
//	}
//
// # Legacy default condition webhook
//
// New Relic classic "Alerts" → "Notification channels" → "Webhook" produces:
//
//	{
//	  "incident_id": 12345,
//	  "condition_name": "...",
//	  "details": "...",
//	  "severity": "CRITICAL"|"HIGH"|"MEDIUM"|"LOW",
//	  "current_state": "open"|"acknowledged"|"closed",
//	  "policy_name": "...",
//	  "targets": [{"name": "...", "type": "...", "labels": {...}}],
//	  "incident_url": "...",
//	  "account_name": "..."
//	}
//
// # Pipeline-submission
//
// internal/plugins.Host does not expose ProcessRecord directly. The plugin
// runtime-asserts that the Host also satisfies a local recordProcessor
// interface — *core.Core satisfies this shape. If the assertion fails (e.g. a
// stripped-down test host) HandleWebhook logs once and degrades to a no-op,
// matching the pattern used by grafana/alertmanager.
package newrelic

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// metaYAML is the raw metadata.yaml content embedded at build time.
//
//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("newrelic", metaYAML, factory)
}

// factory is the plugins.Factory entry-point.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// recordProcessor is the slice of the alert pipeline this plugin needs. The
// concrete *core.Core satisfies this shape; the assertion sidesteps an import
// cycle through internal/plugins.Host.
type recordProcessor interface {
	ProcessRecord(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error)
}

// Plugin is the New Relic webhook receiver.
//
// Lifecycle: Register → factory → PostInit (captures the host) → HandleWebhook
// per incoming POST. There is no persistent state to load or reload.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// warnedNoProcessor tracks whether we have already logged the
	// "host does not satisfy recordProcessor" warning, so it fires once per
	// process even when many webhook calls flow through.
	warnedNoProcessor atomic.Bool
}

// Name returns the registry key.
func (p *Plugin) Name() string { return p.meta.Name }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host in. There is no initial state to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: the plugin has no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// WebhookPath returns the route fragment mounted under /api/v1/webhook/.
// The full external URL is therefore /api/v1/webhook/newrelic.
func (p *Plugin) WebhookPath() string { return "/newrelic" }

// isWorkflow reports whether the decoded map looks like a workflow payload
// (presence of the "state" key in its uppercased form, or "issueUrl").
// We use the raw map decoded by the dispatcher to pick the shape.
func isWorkflow(raw map[string]any) bool {
	_, hasState := raw["state"]
	_, hasIssueURL := raw["issueUrl"]
	_, hasTitle := raw["title"]
	return (hasState && hasTitle) || hasIssueURL
}

// HandleWebhook decodes the incoming New Relic webhook, detects whether it is
// a workflow or legacy payload, maps it to a snoozetypes.Record, and submits
// it to the pipeline. The reply is a small JSON envelope with {status,
// received, accepted}.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// First pass: decode into a raw map to detect the payload shape without
	// committing to a concrete struct. UseNumber preserves numeric precision.
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var rawMap map[string]any
	if err := dec.Decode(&rawMap); err != nil {
		http.Error(w, fmt.Sprintf("invalid New Relic payload: %v", err), http.StatusBadRequest)
		return
	}

	var rec snoozetypes.Record
	if isWorkflow(rawMap) {
		rec = buildWorkflowRecord(rawMap)
	} else {
		rec = buildLegacyRecord(rawMap)
	}

	proc := p.recordProcessor()
	if proc == nil {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("newrelic: host does not satisfy recordProcessor; webhook is a no-op",
					"plugin", p.Name())
			}
		}
	}

	accepted := 0
	if proc != nil {
		if _, _, err := proc.ProcessRecord(r.Context(), rec); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("newrelic: pipeline rejected record",
					"plugin", p.Name(),
					"host", rec.Host,
					"err", err)
			}
		} else {
			accepted++
		}
	} else {
		// No processor: degrade to no-op but count as accepted.
		accepted++
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"received": 1,
		"accepted": accepted,
	})
}

// buildWorkflowRecord maps a workflow/notification webhook payload to a Record.
//
// Mapping:
//   - Source: "newrelic"
//   - Host: impactedEntities[0] if present, else title
//   - Severity: priority → CRITICAL→critical, HIGH→error, MEDIUM→warning, LOW→info; default critical
//   - State: "close" when state == "CLOSED"
//   - Message: title
//   - Raw: issueUrl, priority, state, accountName, labels
func buildWorkflowRecord(raw map[string]any) snoozetypes.Record {
	title, _ := raw["title"].(string)
	priority, _ := raw["priority"].(string)
	state, _ := raw["state"].(string)
	issueURL, _ := raw["issueUrl"].(string)
	accountName, _ := raw["accountName"].(string)
	labels, _ := raw["labels"].(map[string]any)

	// Host from first impacted entity, fallback to title.
	host := title
	if entities, ok := raw["impactedEntities"].([]any); ok && len(entities) > 0 {
		if name, ok := entities[0].(string); ok && name != "" {
			host = name
		}
	}

	rec := snoozetypes.Record{
		Source:    "newrelic",
		Host:      host,
		Severity:  mapPriority(priority),
		Message:   title,
		Timestamp: time.Now().UTC(),
		Raw:       workflowRaw(issueURL, priority, state, accountName, labels),
	}
	if state == "CLOSED" {
		rec.State = "close"
		if rec.Severity == "critical" {
			rec.Severity = "info"
		}
	}
	return rec
}

// buildLegacyRecord maps a legacy/default condition webhook payload to a Record.
//
// Mapping:
//   - Source: "newrelic"
//   - Host: targets[0].name if present, else condition_name
//   - Severity: severity field → same priority mapping
//   - State: "close" when current_state == "closed"
//   - Message: condition_name + ": " + details (or just condition_name)
//   - Raw: incident_url, severity, current_state, account_name
func buildLegacyRecord(raw map[string]any) snoozetypes.Record {
	conditionName, _ := raw["condition_name"].(string)
	details, _ := raw["details"].(string)
	severity, _ := raw["severity"].(string)
	currentState, _ := raw["current_state"].(string)
	incidentURL, _ := raw["incident_url"].(string)
	accountName, _ := raw["account_name"].(string)

	// Host from first target name, fallback to condition_name.
	host := conditionName
	if targets, ok := raw["targets"].([]any); ok && len(targets) > 0 {
		if tmap, ok := targets[0].(map[string]any); ok {
			if name, ok := tmap["name"].(string); ok && name != "" {
				host = name
			}
		}
	}

	message := conditionName
	if details != "" {
		message = conditionName + ": " + details
	}

	rec := snoozetypes.Record{
		Source:    "newrelic",
		Host:      host,
		Severity:  mapPriority(severity),
		Message:   message,
		Timestamp: time.Now().UTC(),
		Raw:       legacyRaw(incidentURL, severity, currentState, accountName),
	}
	if currentState == "closed" {
		rec.State = "close"
		if rec.Severity == "critical" {
			rec.Severity = "info"
		}
	}
	return rec
}

// mapPriority converts a New Relic priority/severity string to a snooze
// severity keyword. Both workflow ("CRITICAL","HIGH","MEDIUM","LOW") and
// legacy ("CRITICAL","HIGH","MEDIUM","LOW") use the same set.
// Unknown values default to "critical".
func mapPriority(p string) string {
	switch p {
	case "CRITICAL":
		return "critical"
	case "HIGH":
		return "error"
	case "MEDIUM":
		return "warning"
	case "LOW":
		return "info"
	default:
		return "critical"
	}
}

// workflowRaw composes Record.Raw for a workflow payload.
func workflowRaw(issueURL, priority, state, accountName string, labels map[string]any) map[string]any {
	raw := map[string]any{}
	if issueURL != "" {
		raw["issueUrl"] = issueURL
	}
	if priority != "" {
		raw["priority"] = priority
	}
	if state != "" {
		raw["state"] = state
	}
	if accountName != "" {
		raw["accountName"] = accountName
	}
	if len(labels) > 0 {
		raw["labels"] = labels
	}
	return raw
}

// legacyRaw composes Record.Raw for a legacy/default condition payload.
func legacyRaw(incidentURL, severity, currentState, accountName string) map[string]any {
	raw := map[string]any{}
	if incidentURL != "" {
		raw["incident_url"] = incidentURL
	}
	if severity != "" {
		raw["severity"] = severity
	}
	if currentState != "" {
		raw["current_state"] = currentState
	}
	if accountName != "" {
		raw["account_name"] = accountName
	}
	return raw
}

// recordProcessor returns the host cast to the recordProcessor contract, or
// nil if the host is missing or does not satisfy it.
func (p *Plugin) recordProcessor() recordProcessor {
	if p.host == nil {
		return nil
	}
	rp, ok := any(p.host).(recordProcessor)
	if !ok {
		return nil
	}
	return rp
}

// logger returns the host logger or nil if unavailable.
func (p *Plugin) logger() interface {
	Warn(string, ...any)
} {
	if p.host == nil {
		return nil
	}
	lg := p.host.Logger()
	if lg == nil {
		return nil
	}
	return lg
}

// Compile-time proof we satisfy the contract.
var _ plugins.WebhookReceiver = (*Plugin)(nil)
