// Package alertmanager implements the `alertmanager` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts the
// Prometheus AlertManager v4 webhook payload, maps each alert to a
// snoozetypes.Record, and submits the record to the host's processing
// pipeline.
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid
// pulling internal/core into the plugin contract. The plugin therefore
// runtime-asserts that the Host value also satisfies a local recordProcessor
// interface — *core.Core satisfies this shape. If the assertion fails (a
// stripped-down test host), HandleWebhook logs once and degrades to a no-op,
// matching the pattern used by the notification plugin for bus access.
package alertmanager

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	plugins.Register("alertmanager", metaYAML, factory)
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

// Plugin is the AlertManager webhook receiver.
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
// The full external URL is therefore /api/v1/webhook/alertmanager.
func (p *Plugin) WebhookPath() string { return "/alertmanager" }

// am4Alert is the per-alert sub-document of the AlertManager v4 webhook
// payload.
type am4Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// am4Webhook is the AlertManager v4 webhook envelope.
type am4Webhook struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []am4Alert        `json:"alerts"`
}

// HandleWebhook decodes the AlertManager v4 payload, maps each alert to a
// snoozetypes.Record, and submits each record to the pipeline. The reply is
// a small JSON envelope describing how many alerts were accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var hook am4Webhook
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&hook); err != nil {
		http.Error(w, fmt.Sprintf("invalid AlertManager payload: %v", err), http.StatusBadRequest)
		return
	}

	proc := p.recordProcessor()
	if proc == nil {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("alertmanager: host does not satisfy recordProcessor; webhook is a no-op",
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
					lg.Warn("alertmanager: pipeline rejected record",
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

// buildRecord maps one AlertManager v4 alert to a snoozetypes.Record.
//
// Mapping (mirrors the Python plugin where it differs):
//
//   - Host: labels.instance (port stripped) falling back to labels.host,
//     labels.exported_instance, or "-".
//   - Source: "alertmanager".
//   - Severity: labels.severity (then commonLabels.severity); "info" default
//     for firing alerts, "ok" for resolved alerts.
//   - Message: annotations.summary then annotations.description then
//     labels.alertname.
//   - Timestamp: startsAt; falls back to time.Now() when absent.
//   - Tags: labels["tags"] split on whitespace/commas when present, else nil.
//   - State: "close" for resolved alerts, empty otherwise.
//   - Raw: the merged label+annotation map (preserves data the rule plugin
//     may reference).
//   - Process: labels.alertname (falling back to labels.service /
//     labels.job).
func buildRecord(hook am4Webhook, a am4Alert) snoozetypes.Record {
	labels := mergeStrings(hook.CommonLabels, hook.GroupLabels, a.Labels)
	annotations := mergeStrings(hook.CommonAnnotations, a.Annotations)

	resolved := strings.EqualFold(a.Status, "resolved")

	rec := snoozetypes.Record{
		Source:    "alertmanager",
		Host:      pickHost(labels),
		Severity:  pickSeverity(labels, resolved),
		Message:   pickMessage(annotations, labels),
		Process:   pickProcess(labels),
		Timestamp: a.StartsAt,
		Tags:      pickTags(labels),
	}

	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}

	if resolved {
		rec.State = "close"
	}

	raw := make(map[string]any, len(labels)+len(annotations)+1)
	if len(labels) > 0 {
		raw["labels"] = stringMapToAny(labels)
	}
	if len(annotations) > 0 {
		raw["annotations"] = stringMapToAny(annotations)
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
	if a.Status != "" {
		raw["status"] = a.Status
	}
	if len(raw) > 0 {
		rec.Raw = raw
	}

	return rec
}

// pickHost extracts the host identifier from the merged label map and strips
// a trailing ":port" if present. Returns "-" when no candidate label exists,
// matching the Python plugin.
func pickHost(labels map[string]string) string {
	for _, k := range []string{"instance", "host", "exported_instance"} {
		if v, ok := labels[k]; ok && v != "" {
			return stripPort(v)
		}
	}
	return "-"
}

// stripPort removes a trailing :NNN segment, preserving the host for typical
// Prometheus targets ("node-1.local:9100" -> "node-1.local"). It does not
// attempt full URL parsing — the instance label is host:port by convention.
func stripPort(s string) string {
	if i := strings.LastIndex(s, ":"); i >= 0 {
		// Only strip when everything after the colon is digits.
		port := s[i+1:]
		if port != "" && allDigits(port) {
			return s[:i]
		}
	}
	return s
}

// allDigits reports whether s is non-empty and made of ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// pickSeverity resolves the severity label, defaulting to "ok" for resolved
// alerts and "info" for firing alerts.
func pickSeverity(labels map[string]string, resolved bool) string {
	if resolved {
		return "ok"
	}
	if v, ok := labels["severity"]; ok && v != "" {
		return v
	}
	return "info"
}

// pickMessage chooses the most descriptive annotation/label for the human
// message column.
func pickMessage(annotations, labels map[string]string) string {
	for _, k := range []string{"summary", "description", "message"} {
		if v, ok := annotations[k]; ok && v != "" {
			return v
		}
	}
	if v, ok := labels["alertname"]; ok && v != "" {
		return v
	}
	return ""
}

// pickProcess resolves the "process" column from the label map.
func pickProcess(labels map[string]string) string {
	for _, k := range []string{"alertname", "service", "job"} {
		if v, ok := labels[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// pickTags pulls a "tags" label and splits it on commas/whitespace. Returns
// nil when no tags label is present, mirroring the spec.
func pickTags(labels map[string]string) []string {
	v, ok := labels["tags"]
	if !ok || v == "" {
		return nil
	}
	// Accept commas or whitespace as separators.
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// mergeStrings combines several string maps with later sources overriding
// earlier ones, returning a fresh map.
func mergeStrings(maps ...map[string]string) map[string]string {
	size := 0
	for _, m := range maps {
		size += len(m)
	}
	out := make(map[string]string, size)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// stringMapToAny converts a string map to a generic map ready for JSON
// embedding into Record.Raw.
func stringMapToAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
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
