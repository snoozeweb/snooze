// Package datadog implements the `datadog` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts Datadog
// monitor-alert webhook payloads, maps them to snoozetypes.Record values, and
// submits each record to the host's processing pipeline.
//
// # Recommended Datadog webhook payload template
//
// In the Datadog Webhooks integration, configure the webhook URL to
// https://<snooze-host>/api/v1/webhook/datadog and use the following JSON
// template in the "Payload" field:
//
//	{
//	  "alert_id":         "$ALERT_ID",
//	  "title":            "$EVENT_TITLE",
//	  "body":             "$EVENT_MSG",
//	  "event_type":       "$EVENT_TYPE",
//	  "alert_type":       "$ALERT_TYPE",
//	  "alert_transition": "$ALERT_TRANSITION",
//	  "date":             $DATE,
//	  "org_id":           "$ORG_ID",
//	  "host":             "$HOSTNAME",
//	  "tags":             "$TAGS",
//	  "priority":         "$PRIORITY",
//	  "aggreg_key":       "$AGGREG_KEY",
//	  "link":             "$LINK"
//	}
//
// Trigger the webhook in a monitor notification with @webhook-<name>.
//
// # Severity mapping
//
// Datadog alert_type → snooze severity:
//
//	error   → critical
//	warning → warning
//	success → info  (also sets State="close")
//	info    → info
//	other   → critical (safe default for unknown firing alerts)
//
// # Close / resolve detection
//
// A record with State="close" is emitted when alert_type is "success" OR
// alert_transition is "Recovered" OR event_type contains "recovered" or
// "resolved". This allows downstream processors to close the matching
// open alert.
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
package datadog

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
	plugins.Register("datadog", metaYAML, factory)
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

// Plugin is the Datadog monitor-alert webhook receiver.
//
// Lifecycle: Register → factory → PostInit (captures the host) → HandleWebhook
// per incoming POST. There is no persistent state to load or reload.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// warnedNoProcessor tracks whether we've already logged the "host does
	// not satisfy recordProcessor" warning, so it fires once per process
	// even when many webhook calls flow through.
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
// The full external URL is therefore /api/v1/webhook/datadog.
func (p *Plugin) WebhookPath() string { return "/datadog" }

// ddPayload is the recommended Datadog monitor webhook JSON body. Keys follow
// the Datadog template variable names documented at
// https://docs.datadoghq.com/integrations/webhooks/#variables.
//
// Numeric date is kept as json.Number so large epoch-millisecond values are
// not rounded by float64 conversion.
type ddPayload struct {
	AlertID         string      `json:"alert_id"`
	Title           string      `json:"title"`
	Body            string      `json:"body"`
	EventType       string      `json:"event_type"`
	AlertType       string      `json:"alert_type"`
	AlertTransition string      `json:"alert_transition"`
	Date            json.Number `json:"date"`
	OrgID           string      `json:"org_id"`
	Host            string      `json:"host"`
	Tags            string      `json:"tags"` // comma-separated string from $TAGS
	Priority        string      `json:"priority"`
	AggregKey       string      `json:"aggreg_key"`
	Link            string      `json:"link"`
}

// HandleWebhook decodes the Datadog webhook payload, maps it to a
// snoozetypes.Record, and submits it to the pipeline. The reply is a small
// JSON envelope describing how many records were accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var hook ddPayload
	if err := dec.Decode(&hook); err != nil {
		http.Error(w, fmt.Sprintf("invalid Datadog payload: %v", err), http.StatusBadRequest)
		return
	}

	records := buildRecords(hook)

	proc := p.recordProcessor()
	if proc == nil && len(records) > 0 {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("datadog: host does not satisfy recordProcessor; webhook is a no-op",
					"plugin", p.Name())
			}
		}
	}

	ctx := r.Context()
	accepted := 0
	for _, rec := range records {
		if proc != nil {
			if _, _, err := proc.ProcessRecord(ctx, rec); err != nil {
				if lg := p.logger(); lg != nil {
					lg.Warn("datadog: pipeline rejected record",
						"plugin", p.Name(),
						"host", rec.Host,
						"process", rec.Process,
						"err", err)
				}
				continue
			}
		}
		accepted++
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"received": len(records),
		"accepted": accepted,
	})
}

// buildRecords maps a ddPayload to the list of snoozetypes.Record values to
// submit. Datadog sends one event per webhook call, so this always returns
// zero or one records.
func buildRecords(hook ddPayload) []snoozetypes.Record {
	severity := mapSeverity(hook.AlertType)
	state := mapState(hook.AlertType, hook.AlertTransition, hook.EventType)

	host := hook.Host
	if host == "" {
		host = hook.AlertID
	}

	process := processFromTags(hook.Tags)

	var tags []string
	if hook.Tags != "" {
		raw := strings.Split(hook.Tags, ",")
		tags = make([]string, 0, len(raw))
		for _, t := range raw {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	// Message: prefer title, fall back to body.
	message := hook.Title
	if message == "" {
		message = hook.Body
	}

	raw := map[string]any{}
	if hook.AlertID != "" {
		raw["alert_id"] = hook.AlertID
	}
	if hook.AggregKey != "" {
		raw["aggreg_key"] = hook.AggregKey
	}
	if hook.Link != "" {
		raw["link"] = hook.Link
	}
	if hook.EventType != "" {
		raw["event_type"] = hook.EventType
	}
	if hook.AlertType != "" {
		raw["alert_type"] = hook.AlertType
	}
	if hook.Priority != "" {
		raw["priority"] = hook.Priority
	}
	if hook.OrgID != "" {
		raw["org_id"] = hook.OrgID
	}
	// Always include the raw tags string so downstream rules can match on it.
	raw["tags"] = hook.Tags

	rec := snoozetypes.Record{
		Source:    "datadog",
		Host:      host,
		Process:   process,
		Severity:  severity,
		Message:   message,
		Timestamp: time.Now().UTC(),
		Tags:      tags,
		Raw:       raw,
		State:     state,
	}
	return []snoozetypes.Record{rec}
}

// mapSeverity converts a Datadog alert_type to a snooze severity keyword.
//
//	error   → critical
//	warning → warning
//	success → info   (recovery; caller also sets State="close")
//	info    → info
//	other   → critical (safe default for unknown/firing alerts)
func mapSeverity(alertType string) string {
	switch strings.ToLower(alertType) {
	case "error":
		return "critical"
	case "warning":
		return "warning"
	case "success":
		return "info"
	case "info":
		return "info"
	default:
		return "critical"
	}
}

// mapState returns "close" when the event indicates a recovery, empty string
// otherwise.
//
// Recovery is detected when:
//   - alert_type is "success", OR
//   - alert_transition contains "Recovered" (case-sensitive Datadog value), OR
//   - event_type is "recovered" or "resolved" (common Datadog event types).
func mapState(alertType, alertTransition, eventType string) string {
	if strings.ToLower(alertType) == "success" {
		return "close"
	}
	if strings.EqualFold(alertTransition, "recovered") {
		return "close"
	}
	et := strings.ToLower(eventType)
	if et == "recovered" || et == "resolved" {
		return "close"
	}
	return ""
}

// processFromTags extracts the value of the first "service:" or "process:"
// tag in the comma-separated tags string.  Returns "" if neither is found.
func processFromTags(tagsStr string) string {
	if tagsStr == "" {
		return ""
	}
	for _, tag := range strings.Split(tagsStr, ",") {
		tag = strings.TrimSpace(tag)
		if v, ok := strings.CutPrefix(tag, "service:"); ok {
			return v
		}
		if v, ok := strings.CutPrefix(tag, "process:"); ok {
			return v
		}
	}
	return ""
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

// Compile-time proof we satisfy the WebhookReceiver contract.
var _ plugins.WebhookReceiver = (*Plugin)(nil)
