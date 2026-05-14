// Package prometheus implements the `prometheus` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts the
// Prometheus alert webhook payload — the same `alerts[]` envelope the Python
// `PrometheusRoute` consumes — maps each alert to a snoozetypes.Record, and
// submits the record to the host's processing pipeline.
//
// # Relationship to the `alertmanager` sibling
//
// The Python codebase ships two near-identical webhook receivers:
//
//   - `prometheus` (this plugin): the historical receiver used by
//     deployments that wire Prometheus' Alertmanager directly into Snooze
//     without further transformation. Endpoint: /api/v1/webhook/prometheus.
//     Source label: "prometheus". Default severity for firing alerts is
//     "critical" — matching the Python plugin's behaviour.
//
//   - `alertmanager` (sibling package): the explicit AlertManager v4 receiver
//     with stricter defaults (severity "info" for firing alerts, "-" host
//     fallback). Endpoint: /api/v1/webhook/alertmanager.
//
// Both consume the same JSON envelope shape — there is no separate
// "pre-alertmanager" wire format in practice — but the field mappings and
// defaults differ, so this plugin reproduces the Python `prometheus` plugin's
// `parse` semantics in full.
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid
// pulling internal/core into the plugin contract. The plugin therefore
// runtime-asserts that the Host value also satisfies a local recordProcessor
// interface — *core.Core satisfies this shape. If the assertion fails (a
// stripped-down test host), HandleWebhook logs once and degrades to a no-op,
// matching the pattern used by the alertmanager and notification plugins.
package prometheus

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// metaYAML is the raw metadata.yaml content embedded at build time.
//
//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("prometheus", metaYAML, factory)
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

// Plugin is the Prometheus webhook receiver.
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
// The full external URL is therefore /api/v1/webhook/prometheus.
func (p *Plugin) WebhookPath() string { return "/prometheus" }

// promAlert is the per-alert sub-document of the Prometheus webhook payload.
// Labels and annotations are decoded as raw JSON so we preserve values that
// the Python plugin would otherwise pass through `bson.json_util.loads`
// (e.g. a label set to a JSON-encoded list).
type promAlert struct {
	Status       string                     `json:"status"`
	Labels       map[string]json.RawMessage `json:"labels"`
	Annotations  map[string]json.RawMessage `json:"annotations"`
	StartsAt     time.Time                  `json:"startsAt"`
	EndsAt       time.Time                  `json:"endsAt"`
	GeneratorURL string                     `json:"generatorURL"`
	Fingerprint  string                     `json:"fingerprint"`
}

// promWebhook is the Prometheus webhook envelope, matching the AlertManager
// v4 shape that the Python `PrometheusRoute` ingests.
type promWebhook struct {
	Version           string                     `json:"version"`
	GroupKey          string                     `json:"groupKey"`
	Status            string                     `json:"status"`
	Receiver          string                     `json:"receiver"`
	GroupLabels       map[string]json.RawMessage `json:"groupLabels"`
	CommonLabels      map[string]json.RawMessage `json:"commonLabels"`
	CommonAnnotations map[string]json.RawMessage `json:"commonAnnotations"`
	ExternalURL       string                     `json:"externalURL"`
	Alerts            []promAlert                `json:"alerts"`
}

// HandleWebhook decodes the Prometheus payload, maps each alert to a
// snoozetypes.Record, and submits each record to the pipeline. The reply is
// a small JSON envelope describing how many alerts were accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var hook promWebhook
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&hook); err != nil {
		http.Error(w, fmt.Sprintf("invalid Prometheus payload: %v", err), http.StatusBadRequest)
		return
	}

	proc := p.recordProcessor()
	if proc == nil {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("prometheus: host does not satisfy recordProcessor; webhook is a no-op",
					"plugin", p.Name())
			}
		}
	}

	ctx := r.Context()
	accepted := 0
	for i := range hook.Alerts {
		rec := buildRecord(hook, hook.Alerts[i])
		if proc != nil {
			if _, _, err := proc.ProcessRecord(ctx, rec); err != nil {
				if lg := p.logger(); lg != nil {
					lg.Warn("prometheus: pipeline rejected record",
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
		"received": len(hook.Alerts),
		"accepted": accepted,
	})
}

// buildRecord maps one Prometheus alert to a snoozetypes.Record.
//
// Mapping mirrors the Python `PrometheusRoute.parse` method:
//
//   - Source: "prometheus" (hard-coded).
//   - Severity: "critical" for firing alerts (overridable via labels.severity),
//     "ok" for resolved alerts, "unknown" otherwise. Matches Python defaults.
//   - Host: labels.host then labels.instance then labels.exported_instance
//     (no value-fallback string — empty stays empty).
//   - Process: labels.process then labels.service.
//   - Message: annotations.message then .summary then .description then
//     .externalURL.
//   - Timestamp: startsAt; falls back to time.Now().UTC() when absent.
//   - State: "close" for resolved alerts, empty otherwise.
//   - Raw: the full decoded alert content (labels + annotations + status +
//     generatorURL + envelope externalURL), preserving the Python plugin's
//     deepcopy(content) behaviour.
func buildRecord(hook promWebhook, a promAlert) snoozetypes.Record {
	labels := mergeRaw(hook.CommonLabels, hook.GroupLabels, a.Labels)
	annotations := mergeRaw(hook.CommonAnnotations, a.Annotations)

	status := a.Status
	if status == "" {
		// Python defaults missing status to "firing".
		status = "firing"
	}
	resolved := status == "resolved"

	rec := snoozetypes.Record{
		Source:    "prometheus",
		Host:      pickHost(labels),
		Severity:  pickSeverity(labels, status),
		Message:   pickMessage(annotations),
		Process:   pickProcess(labels),
		Timestamp: a.StartsAt,
	}

	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}

	if resolved {
		rec.State = "close"
	}

	raw := make(map[string]any, len(labels)+len(annotations)+3)
	if len(labels) > 0 {
		raw["labels"] = rawMapToAny(labels)
	}
	if len(annotations) > 0 {
		raw["annotations"] = rawMapToAny(annotations)
	}
	if a.GeneratorURL != "" {
		raw["generatorURL"] = a.GeneratorURL
	}
	if hook.ExternalURL != "" {
		raw["externalURL"] = hook.ExternalURL
	}
	if a.Fingerprint != "" {
		raw["fingerprint"] = a.Fingerprint
	}
	if status != "" {
		raw["status"] = status
	}
	if len(raw) > 0 {
		rec.Raw = raw
	}

	return rec
}

// pickHost mirrors Python's `labels.pop('host', labels.pop('instance',
// labels.pop('exported_instance', '')))`. We do not strip a trailing port:
// the Python `prometheus` plugin keeps the instance label verbatim. Returns
// "" when no candidate label is set.
func pickHost(labels map[string]json.RawMessage) string {
	for _, k := range []string{"host", "instance", "exported_instance"} {
		if v, ok := stringOf(labels, k); ok && v != "" {
			return v
		}
	}
	return ""
}

// pickSeverity matches Python:
//
//	if firing:   severity = labels.pop('severity', 'critical')
//	if resolved: severity = 'ok'
//	otherwise:   severity = 'unknown'
func pickSeverity(labels map[string]json.RawMessage, status string) string {
	switch status {
	case "firing":
		if v, ok := stringOf(labels, "severity"); ok && v != "" {
			return v
		}
		return "critical"
	case "resolved":
		return "ok"
	default:
		return "unknown"
	}
}

// pickMessage walks the annotation candidates in the Python order:
// message → summary → description → externalURL.
func pickMessage(annotations map[string]json.RawMessage) string {
	for _, k := range []string{"message", "summary", "description", "externalURL"} {
		if v, ok := stringOf(annotations, k); ok && v != "" {
			return v
		}
	}
	return ""
}

// pickProcess mirrors Python's `labels.pop('process', labels.pop('service'))`.
// Empty when neither label is present.
func pickProcess(labels map[string]json.RawMessage) string {
	for _, k := range []string{"process", "service"} {
		if v, ok := stringOf(labels, k); ok && v != "" {
			return v
		}
	}
	return ""
}

// stringOf returns the string value of a label/annotation, dropping the
// surrounding JSON quotes when present. Non-string JSON values are returned
// as their raw JSON form so the caller still sees something useful. The
// boolean reports whether the key existed in the map.
func stringOf(m map[string]json.RawMessage, k string) (string, bool) {
	raw, ok := m[k]
	if !ok {
		return "", false
	}
	// Try to unmarshal as a JSON string first (the common case).
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	// Fall back to the raw bytes so an integer/object label is preserved.
	return string(raw), true
}

// mergeRaw combines several raw-label maps with later sources overriding
// earlier ones, returning a fresh map.
func mergeRaw(maps ...map[string]json.RawMessage) map[string]json.RawMessage {
	size := 0
	for _, m := range maps {
		size += len(m)
	}
	out := make(map[string]json.RawMessage, size)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// rawMapToAny converts a raw-label map to a generic map ready for JSON
// embedding into Record.Raw. Values are decoded into Go-native types so the
// downstream consumer sees strings/numbers/objects, not opaque json.RawMessage.
func rawMapToAny(m map[string]json.RawMessage) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		var decoded any
		if err := json.Unmarshal(v, &decoded); err != nil {
			// Should not happen for valid JSON we just decoded, but keep
			// the raw bytes as a safety net so we never panic.
			out[k] = string(v)
			continue
		}
		out[k] = decoded
	}
	return out
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
