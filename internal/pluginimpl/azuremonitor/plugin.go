// Package azuremonitor implements the `azuremonitor` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts the Azure
// Monitor Common Alert Schema payload, maps the alert to a snoozetypes.Record,
// and submits it to the host's processing pipeline.
//
// # Common Alert Schema
//
// Azure Monitor supports a "common alert schema" that normalises all alert
// types (metric, log, activity, service-health) into a single JSON envelope:
//
//	{
//	  "schemaId": "azureMonitorCommonAlertSchema",
//	  "data": {
//	    "essentials": { "alertId", "alertRule", "severity", "signalType",
//	                    "monitorCondition", "monitoringService",
//	                    "alertTargetIDs", "firedDateTime",
//	                    "resolvedDateTime", "description" },
//	    "alertContext": { ... signal-specific extra fields ... }
//	  }
//	}
//
// When monitorCondition == "Resolved" the plugin emits State="close" so that
// downstream processors can close the matching open alert.
//
// # Severity mapping
//
//	Sev0, Sev1 → critical
//	Sev2       → error
//	Sev3       → warning
//	Sev4       → info
//
// # Fallback for non-Common-Alert-Schema bodies
//
// If data.essentials is absent the plugin falls back to top-level title and
// description fields (best-effort) with default severity "critical".
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid
// pulling internal/core into the plugin contract. The plugin therefore
// runtime-asserts that the Host value also satisfies a local recordProcessor
// interface — *core.Core satisfies this shape. If the assertion fails (a
// stripped-down test host), HandleWebhook logs once and degrades to a no-op,
// matching the pattern used by internal/pluginimpl/grafana and
// internal/pluginimpl/alertmanager.
package azuremonitor

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	plugins.Register("azuremonitor", metaYAML, factory)
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

// Plugin is the Azure Monitor Common Alert Schema webhook receiver.
//
// Lifecycle: Register → factory → PostInit (captures the host) → HandleWebhook
// per incoming POST. There is no persistent state to load or reload.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// warnedNoProcessor tracks whether we've already logged the "host does
	// not satisfy recordProcessor" warning, so the warning fires once per
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
// The full external URL is therefore /api/v1/webhook/azuremonitor.
func (p *Plugin) WebhookPath() string { return "/azuremonitor" }

// azEssentials is the "data.essentials" block of the Azure Monitor Common
// Alert Schema.
type azEssentials struct {
	AlertID           string   `json:"alertId"`
	AlertRule         string   `json:"alertRule"`
	Severity          string   `json:"severity"`
	SignalType        string   `json:"signalType"`
	MonitorCondition  string   `json:"monitorCondition"`
	MonitoringService string   `json:"monitoringService"`
	AlertTargetIDs    []string `json:"alertTargetIDs"`
	FiredDateTime     string   `json:"firedDateTime"`
	ResolvedDateTime  string   `json:"resolvedDateTime"`
	Description       string   `json:"description"`
}

// azData is the "data" block of the Azure Monitor Common Alert Schema.
type azData struct {
	Essentials   *azEssentials  `json:"essentials"`
	AlertContext map[string]any `json:"alertContext"`
}

// azPayload is the top-level Azure Monitor Common Alert Schema envelope.
// The plugin also accepts non-schema bodies at the top level (best-effort).
type azPayload struct {
	SchemaID string  `json:"schemaId"`
	Data     *azData `json:"data"`
	// Fallback fields used when essentials is absent.
	Title       string `json:"title"`
	Description string `json:"description"`
}

// HandleWebhook decodes the Azure Monitor Common Alert Schema payload, builds a
// snoozetypes.Record, and submits it to the pipeline. The reply is a small JSON
// envelope describing how many records were accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var hook azPayload
	if err := dec.Decode(&hook); err != nil {
		http.Error(w, fmt.Sprintf("invalid Azure Monitor payload: %v", err), http.StatusBadRequest)
		return
	}

	rec := buildRecord(hook)

	proc := p.recordProcessor()
	if proc == nil && !p.warnedNoProcessor.Swap(true) {
		if lg := p.logger(); lg != nil {
			lg.Warn("azuremonitor: host does not satisfy recordProcessor; webhook is a no-op",
				"plugin", p.Name())
		}
	}

	accepted := 0
	if proc != nil {
		if _, _, err := proc.ProcessRecord(r.Context(), rec); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("azuremonitor: pipeline rejected record",
					"plugin", p.Name(),
					"host", rec.Host,
					"process", rec.Process,
					"err", err)
			}
		} else {
			accepted = 1
		}
	} else {
		// No processor — count as accepted (no-op success), matching grafana's behaviour.
		accepted = 1
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"received": 1,
		"accepted": accepted,
	})
}

// buildRecord maps the Azure Monitor Common Alert Schema payload to a
// snoozetypes.Record. When data.essentials is absent it falls back to
// top-level title/description fields (best-effort) with default severity
// "critical".
func buildRecord(hook azPayload) snoozetypes.Record {
	if hook.Data == nil || hook.Data.Essentials == nil {
		return buildFallbackRecord(hook)
	}
	return buildEssentialsRecord(hook)
}

// buildEssentialsRecord maps a fully-formed Common Alert Schema body.
func buildEssentialsRecord(hook azPayload) snoozetypes.Record {
	ess := hook.Data.Essentials

	severity := mapSeverity(ess.Severity, ess.MonitorCondition)
	state := ""
	if ess.MonitorCondition == "Resolved" {
		state = "close"
		if severity == "critical" || severity == "error" || severity == "warning" {
			// Resolved alerts are informational by convention.
			severity = "info"
		}
	}

	host := hostFromTargetIDs(ess.AlertTargetIDs)
	if host == "" {
		host = ess.AlertRule
	}

	process := ess.MonitoringService
	if ess.SignalType != "" {
		if process != "" {
			process = process + "/" + ess.SignalType
		} else {
			process = ess.SignalType
		}
	}

	message := ess.Description
	if message == "" {
		message = ess.AlertRule
	}

	raw := buildRaw(ess, hook.Data.AlertContext)

	return snoozetypes.Record{
		Source:    "azuremonitor",
		Host:      host,
		Process:   process,
		Severity:  severity,
		Message:   message,
		Timestamp: time.Now().UTC(),
		Raw:       raw,
		State:     state,
	}
}

// buildFallbackRecord constructs a best-effort record from a non-Common-Alert-
// Schema body using any top-level title/description fields.
func buildFallbackRecord(hook azPayload) snoozetypes.Record {
	message := hook.Description
	if message == "" {
		message = hook.Title
	}

	raw := map[string]any{}
	if hook.SchemaID != "" {
		raw["schemaId"] = hook.SchemaID
	}
	if hook.Title != "" {
		raw["title"] = hook.Title
	}
	if hook.Description != "" {
		raw["description"] = hook.Description
	}

	return snoozetypes.Record{
		Source:    "azuremonitor",
		Severity:  "critical",
		Message:   message,
		Timestamp: time.Now().UTC(),
		Raw:       raw,
	}
}

// mapSeverity converts Azure Monitor Sev0..Sev4 to Snooze severity keywords.
// The monitorCondition is used to pick a sensible default when the severity
// field is empty.
func mapSeverity(azSev, monitorCondition string) string {
	switch strings.ToLower(azSev) {
	case "sev0", "sev1":
		return "critical"
	case "sev2":
		return "error"
	case "sev3":
		return "warning"
	case "sev4":
		return "info"
	default:
		if monitorCondition == "Resolved" {
			return "info"
		}
		return "critical"
	}
}

// hostFromTargetIDs extracts a human-friendly host name from the first element
// of alertTargetIDs by taking the last "/"-separated segment of the Azure
// resource ID (e.g. "/subscriptions/.../virtualMachines/my-vm" → "my-vm").
func hostFromTargetIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	id := ids[0]
	if idx := strings.LastIndex(id, "/"); idx >= 0 && idx < len(id)-1 {
		return id[idx+1:]
	}
	return id
}

// buildRaw composes Record.Raw from the essentials block and, compactly, the
// alertContext so that rules can match on them.
func buildRaw(ess *azEssentials, alertContext map[string]any) map[string]any {
	raw := map[string]any{}
	if ess.AlertID != "" {
		raw["alertId"] = ess.AlertID
	}
	if ess.AlertRule != "" {
		raw["alertRule"] = ess.AlertRule
	}
	if ess.Severity != "" {
		raw["severity"] = ess.Severity
	}
	if ess.SignalType != "" {
		raw["signalType"] = ess.SignalType
	}
	if ess.MonitorCondition != "" {
		raw["monitorCondition"] = ess.MonitorCondition
	}
	if ess.MonitoringService != "" {
		raw["monitoringService"] = ess.MonitoringService
	}
	if len(ess.AlertTargetIDs) > 0 {
		raw["alertTargetIDs"] = ess.AlertTargetIDs
	}
	if ess.FiredDateTime != "" {
		raw["firedDateTime"] = ess.FiredDateTime
	}
	if ess.ResolvedDateTime != "" {
		raw["resolvedDateTime"] = ess.ResolvedDateTime
	}
	if len(alertContext) > 0 {
		raw["alertContext"] = alertContext
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
