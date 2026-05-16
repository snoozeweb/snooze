// Package grafana implements the `grafana` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts the legacy
// Grafana `alert-notifier/webhook` payload (Grafana 8.4 and below — the shape
// also produced by the "Webhook" notifier in unified-alerting compatibility
// mode), maps each evalMatch to a snoozetypes.Record, and submits the records
// to the host's processing pipeline.
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid
// pulling internal/core into the plugin contract. The plugin therefore
// runtime-asserts that the Host value also satisfies a local recordProcessor
// interface — *core.Core satisfies this shape. If the assertion fails (a
// stripped-down test host), HandleWebhook logs once and degrades to a no-op,
// matching the pattern used by internal/pluginimpl/alertmanager and the
// notification plugin's bus access.
//
// # Python-fidelity drift
//
// The Python plugin emits records only when `state == "alerting"`. This Go
// port additionally handles the `ok`, `no_data`, and `paused` states so the
// pipeline can close or warn on resolved alerts:
//
//   - alerting: one record per evalMatch (port of Python parse_old).
//   - ok: one record with Severity="info" and State="close" so downstream
//     processors can close the matching open alert.
//   - no_data: one record with Severity="warning".
//   - paused: zero records — Grafana resumed/paused state is not an alert.
package grafana

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
	plugins.Register("grafana", metaYAML, factory)
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

// Plugin is the Grafana legacy webhook receiver.
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
// The full external URL is therefore /api/v1/webhook/grafana.
func (p *Plugin) WebhookPath() string { return "/grafana" }

// evalMatch is one entry in the `evalMatches` array of the Grafana legacy
// webhook payload. Tag values arrive as JSON-stringified scalars or plain
// strings depending on the data source; we keep them as `any` and emit them
// verbatim into Record.Raw.
type evalMatch struct {
	Metric string         `json:"metric"`
	Value  any            `json:"value"`
	Tags   map[string]any `json:"tags"`
}

// gfWebhook is the Grafana legacy `alert-notifier/webhook` envelope.
type gfWebhook struct {
	Title       string         `json:"title"`
	RuleID      json.Number    `json:"ruleId"`
	RuleName    string         `json:"ruleName"`
	RuleURL     string         `json:"ruleUrl"`
	State       string         `json:"state"`
	Message     string         `json:"message"`
	ImageURL    string         `json:"imageUrl"`
	OrgID       json.Number    `json:"orgId"`
	DashboardID json.Number    `json:"dashboardId"`
	PanelID     json.Number    `json:"panelId"`
	Tags        map[string]any `json:"tags"`
	EvalMatches []evalMatch    `json:"evalMatches"`
}

// HandleWebhook decodes the Grafana legacy webhook payload, maps each
// evalMatch (or the envelope itself for resolved/no-data states) to a
// snoozetypes.Record, and submits each record to the pipeline. The reply is
// a small JSON envelope describing how many records were accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var hook gfWebhook
	if err := dec.Decode(&hook); err != nil {
		http.Error(w, fmt.Sprintf("invalid Grafana payload: %v", err), http.StatusBadRequest)
		return
	}

	records := buildRecords(hook)

	proc := p.recordProcessor()
	if proc == nil && len(records) > 0 {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("grafana: host does not satisfy recordProcessor; webhook is a no-op",
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
					lg.Warn("grafana: pipeline rejected record",
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

// buildRecords fans the webhook payload out into the list of records to
// submit. See the package doc for the per-state semantics.
func buildRecords(hook gfWebhook) []snoozetypes.Record {
	switch hook.State {
	case "alerting":
		if len(hook.EvalMatches) == 0 {
			// Defensive: Grafana usually sends at least one match for an
			// alerting transition, but a missing/empty array should still
			// produce a record (mirrors the Python fallback chain).
			return []snoozetypes.Record{buildEnvelopeRecord(hook, "critical", "")}
		}
		out := make([]snoozetypes.Record, 0, len(hook.EvalMatches))
		for i := range hook.EvalMatches {
			out = append(out, buildMatchRecord(hook, hook.EvalMatches[i]))
		}
		return out
	case "ok":
		return []snoozetypes.Record{buildEnvelopeRecord(hook, "info", "close")}
	case "no_data":
		return []snoozetypes.Record{buildEnvelopeRecord(hook, "warning", "")}
	default:
		// "paused", unknown — no records.
		return nil
	}
}

// buildMatchRecord ports the Python `parse_old` mapping for one evalMatch.
//
// Mapping (mirrors src/snooze/plugins/core/grafana/falcon/route.py parse_old):
//
//   - Host: tags.host (falls back to ruleName).
//   - Source: "grafana".
//   - Severity: tags.severity (default "critical").
//   - Message: webhook.message then webhook.title then webhook.ruleName.
//   - Process: tags.process (falls back to match.metric).
//   - Timestamp: time.Now() — the legacy webhook carries no per-match time.
//   - Raw: the original evalMatch plus the envelope's rule/panel identifiers.
func buildMatchRecord(hook gfWebhook, m evalMatch) snoozetypes.Record {
	tags := m.Tags

	host, _ := stringFromMap(tags, "host")
	if host == "" {
		host = hook.RuleName
	}

	process, _ := stringFromMap(tags, "process")
	if process == "" {
		process = m.Metric
	}

	severity, _ := stringFromMap(tags, "severity")
	if severity == "" {
		severity = "critical"
	}

	rec := snoozetypes.Record{
		Source:    "grafana",
		Host:      host,
		Process:   process,
		Severity:  severity,
		Message:   pickMessage(hook),
		Timestamp: time.Now().UTC(),
		Raw:       matchRaw(hook, m),
	}
	return rec
}

// buildEnvelopeRecord constructs a single record from the webhook envelope
// alone, used for resolved (state=ok) and no-data states. The envelope's
// tags map (if present) supplies the host/process/severity overrides.
func buildEnvelopeRecord(hook gfWebhook, defaultSeverity, state string) snoozetypes.Record {
	tags := hook.Tags

	host, _ := stringFromMap(tags, "host")
	if host == "" {
		host = hook.RuleName
	}

	process, _ := stringFromMap(tags, "process")
	if process == "" {
		process = hook.RuleName
	}

	severity, _ := stringFromMap(tags, "severity")
	if severity == "" {
		severity = defaultSeverity
	}

	rec := snoozetypes.Record{
		Source:    "grafana",
		Host:      host,
		Process:   process,
		Severity:  severity,
		Message:   pickMessage(hook),
		Timestamp: time.Now().UTC(),
		Raw:       envelopeRaw(hook),
		State:     state,
	}
	return rec
}

// pickMessage chooses the most descriptive envelope field for the human
// message column, mirroring the Python fallback chain.
func pickMessage(hook gfWebhook) string {
	if hook.Message != "" {
		return hook.Message
	}
	if hook.Title != "" {
		return hook.Title
	}
	return hook.RuleName
}

// stringFromMap returns the string form of m[k] when it is a string (or a
// json.Number) and reports whether the key was present.
func stringFromMap(m map[string]any, k string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[k]
	if !ok {
		return "", false
	}
	switch s := v.(type) {
	case string:
		return s, true
	case json.Number:
		return s.String(), true
	case fmt.Stringer:
		return s.String(), true
	}
	return "", false
}

// matchRaw composes the Record.Raw map for an alerting evalMatch. The shape
// is intentionally compatible with downstream rule/aggregaterule plugins
// that may reference the raw payload.
func matchRaw(hook gfWebhook, m evalMatch) map[string]any {
	raw := map[string]any{}
	if m.Metric != "" {
		raw["metric"] = m.Metric
	}
	if m.Value != nil {
		raw["value"] = m.Value
	}
	if len(m.Tags) > 0 {
		raw["tags"] = m.Tags
	}
	if hook.RuleID != "" {
		raw["ruleId"] = hook.RuleID.String()
	}
	if hook.RuleName != "" {
		raw["ruleName"] = hook.RuleName
	}
	if hook.RuleURL != "" {
		raw["ruleUrl"] = hook.RuleURL
	}
	if hook.PanelID != "" {
		raw["panelId"] = hook.PanelID.String()
	}
	if hook.DashboardID != "" {
		raw["dashboardId"] = hook.DashboardID.String()
	}
	if hook.OrgID != "" {
		raw["orgId"] = hook.OrgID.String()
	}
	if hook.ImageURL != "" {
		raw["imageUrl"] = hook.ImageURL
	}
	if hook.State != "" {
		raw["state"] = hook.State
	}
	return raw
}

// envelopeRaw composes Record.Raw for envelope-only states (ok/no_data),
// where there is no per-match data to embed.
func envelopeRaw(hook gfWebhook) map[string]any {
	raw := map[string]any{}
	if hook.RuleID != "" {
		raw["ruleId"] = hook.RuleID.String()
	}
	if hook.RuleName != "" {
		raw["ruleName"] = hook.RuleName
	}
	if hook.RuleURL != "" {
		raw["ruleUrl"] = hook.RuleURL
	}
	if hook.State != "" {
		raw["state"] = hook.State
	}
	if len(hook.Tags) > 0 {
		raw["tags"] = hook.Tags
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
