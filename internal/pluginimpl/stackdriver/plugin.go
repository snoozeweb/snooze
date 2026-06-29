// Package stackdriver implements the `stackdriver` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts Google Cloud
// Monitoring (formerly Stackdriver) incident webhook notifications, maps each
// incident to a snoozetypes.Record, and submits it to the host's processing
// pipeline.
//
// # Incident payload
//
// GCP Monitoring posts a single JSON object with a top-level "incident" key:
//
//	{
//	  "incident": {
//	    "incident_id", "resource_name", "resource_id", "condition_name",
//	    "policy_name", "state", "severity", "summary", "url",
//	    "started_at", "ended_at", "environment", "origin",
//	    "documentation": { "content": "..." }
//	  }
//	}
//
// # State → (severity, State) mapping
//
//	open          → (severity field or "critical", "")
//	acknowledged  → (severity preserved,            "ack"   via receiverutil.MapLegacyState)
//	closed        → ("ok",                          "close" via receiverutil.MapLegacyState)
//	anything else → ("indeterminate",               "")
//
// The acknowledged/closed branches delegate to receiverutil.MapLegacyState so
// the canonical Alerta-style state convention lives in one place; the
// open/default branches are inline because they also carry severity side
// effects.
//
// # documentation override
//
// If incident.documentation.content is present and parses as a JSON object, the
// recognised fields (severity, summary, environment, origin) overlay the
// incident before the record is built — a flexible escape hatch for operators.
// Invalid content is logged at WARN and ignored (no 400, no panic).
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid pulling
// internal/core into the plugin contract. The plugin therefore runtime-asserts
// that the Host value also satisfies a local recordProcessor interface —
// *core.Core satisfies this shape. If the assertion fails (a stripped-down test
// host), HandleWebhook logs once and degrades to a no-op, matching the pattern
// used by internal/pluginimpl/azuremonitor and internal/pluginimpl/grafana.
package stackdriver

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/snoozeweb/snooze/internal/pluginimpl/receiverutil"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// metaYAML is the raw metadata.yaml content embedded at build time.
//
//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("stackdriver", metaYAML, factory)
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

// Plugin is the Google Cloud Monitoring (Stackdriver) incident webhook receiver.
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
// The full external URL is therefore /api/v1/webhook/stackdriver.
func (p *Plugin) WebhookPath() string { return "/stackdriver" }

// sdDocumentation is the optional incident.documentation block.
type sdDocumentation struct {
	Content string `json:"content"`
}

// sdIncident is the GCP Monitoring incident object.
type sdIncident struct {
	IncidentID    string           `json:"incident_id"`
	ResourceName  string           `json:"resource_name"`
	ResourceID    string           `json:"resource_id"`
	ConditionName string           `json:"condition_name"`
	PolicyName    string           `json:"policy_name"`
	State         string           `json:"state"`
	Severity      string           `json:"severity"`
	Summary       string           `json:"summary"`
	URL           string           `json:"url"`
	StartedAt     int64            `json:"started_at"`
	EndedAt       *int64           `json:"ended_at"`
	Environment   string           `json:"environment"`
	Origin        string           `json:"origin"`
	Documentation *sdDocumentation `json:"documentation"`
}

// sdPayload is the top-level GCP Monitoring webhook envelope.
type sdPayload struct {
	Incident sdIncident `json:"incident"`
}

// HandleWebhook decodes the GCP Monitoring incident payload, builds a
// snoozetypes.Record, and submits it to the pipeline. The reply is a small JSON
// envelope describing how many records were accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dec := json.NewDecoder(r.Body)
	var hook sdPayload
	if err := dec.Decode(&hook); err != nil {
		http.Error(w, fmt.Sprintf("invalid Stackdriver payload: %v", err), http.StatusBadRequest)
		return
	}

	rec := p.buildRecord(hook)

	proc := p.recordProcessor()
	if proc == nil && !p.warnedNoProcessor.Swap(true) {
		if lg := p.logger(); lg != nil {
			lg.Warn("stackdriver: host does not satisfy recordProcessor; webhook is a no-op",
				"plugin", p.Name())
		}
	}

	accepted := 0
	if proc != nil {
		if _, _, err := proc.ProcessRecord(r.Context(), rec); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("stackdriver: pipeline rejected record",
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

// buildRecord maps a GCP Monitoring incident to a snoozetypes.Record. It first
// applies any documentation override, then maps state/severity and assembles
// the record fields.
func (p *Plugin) buildRecord(hook sdPayload) snoozetypes.Record {
	inc := hook.Incident
	p.applyDocumentation(&inc)

	severity, state := mapStateAndSeverity(inc.State, inc.Severity)

	var tags []string
	if inc.PolicyName != "" {
		tags = []string{inc.PolicyName}
	}

	raw := map[string]any{}
	if inc.IncidentID != "" {
		raw["incident_id"] = inc.IncidentID
	}
	if inc.ResourceID != "" {
		raw["resource_id"] = inc.ResourceID
	}
	if inc.URL != "" {
		raw["url"] = inc.URL
	}
	raw["started_at"] = inc.StartedAt
	if inc.PolicyName != "" {
		raw["policy_name"] = inc.PolicyName
	}
	// Only populate ended_at when present — GCP sends null for open incidents,
	// so guard against a nil-pointer dereference.
	if inc.EndedAt != nil {
		raw["ended_at"] = *inc.EndedAt
	}

	return snoozetypes.Record{
		Source:    "stackdriver",
		Host:      inc.ResourceName,
		Process:   inc.ConditionName,
		Severity:  severity,
		State:     state,
		Message:   inc.Summary,
		Tags:      tags,
		Timestamp: time.Now().UTC(),
		Raw:       raw,
	}
}

// mapStateAndSeverity maps the GCP incident state (and the optional severity
// field) to a Snooze (severity, State) pair. The acknowledged/closed branches
// delegate to receiverutil.MapLegacyState so the canonical Alerta-style state
// convention lives in one place; open/default are inline because they carry
// severity side effects too.
func mapStateAndSeverity(state, severity string) (recSeverity, recState string) {
	switch state {
	case "open":
		// Default to "critical" when the (optional) severity field is absent.
		if severity != "" {
			return severity, ""
		}
		return "critical", ""
	case "acknowledged":
		// Severity preserved; State="ack" via the shared helper.
		return severity, receiverutil.MapLegacyState(state)
	case "closed":
		// Severity downgraded to "ok"; State="close" via the shared helper.
		return "ok", receiverutil.MapLegacyState(state)
	default:
		return "indeterminate", ""
	}
}

// applyDocumentation parses incident.documentation.content as a JSON object and
// overlays the recognised fields (severity, summary, environment, origin) onto
// the incident before the record is built. Invalid content is logged at WARN
// and ignored — it never yields a 400 or a panic.
func (p *Plugin) applyDocumentation(inc *sdIncident) {
	if inc.Documentation == nil || inc.Documentation.Content == "" {
		return
	}

	var overlay map[string]any
	if err := json.Unmarshal([]byte(inc.Documentation.Content), &overlay); err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("stackdriver: documentation.content is not valid JSON; ignoring override",
				"plugin", p.Name(),
				"incident_id", inc.IncidentID,
				"err", err)
		}
		return
	}

	if v, ok := overlay["severity"].(string); ok && v != "" {
		inc.Severity = v
	}
	if v, ok := overlay["summary"].(string); ok && v != "" {
		inc.Summary = v
	}
	if v, ok := overlay["environment"].(string); ok && v != "" {
		inc.Environment = v
	}
	if v, ok := overlay["origin"].(string); ok && v != "" {
		inc.Origin = v
	}
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
