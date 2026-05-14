// Package influxdb2 implements the `influxdb2` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts the
// InfluxDB 2.x HTTP notification endpoint payload, maps the alert to a
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
// matching the pattern used by the alertmanager plugin.
package influxdb2

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
	plugins.Register("influxdb2", metaYAML, factory)
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

// Plugin is the InfluxDB 2.x webhook receiver.
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
// The full external URL is therefore /api/v1/webhook/influxdb2.
func (p *Plugin) WebhookPath() string { return "/influxdb2" }

// HandleWebhook decodes the InfluxDB 2.x notification payload, maps it to a
// snoozetypes.Record, and submits the record to the pipeline. The reply is a
// small JSON envelope describing whether the alert was accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var media map[string]any
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&media); err != nil {
		http.Error(w, fmt.Sprintf("invalid InfluxDB 2 payload: %v", err), http.StatusBadRequest)
		return
	}
	if media == nil {
		http.Error(w, "invalid InfluxDB 2 payload: empty body", http.StatusBadRequest)
		return
	}

	rec := buildRecord(media)

	proc := p.recordProcessor()
	accepted := 0
	if proc == nil {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("influxdb2: host does not satisfy recordProcessor; webhook is a no-op",
					"plugin", p.Name())
			}
		}
	} else {
		if _, _, err := proc.ProcessRecord(r.Context(), rec); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("influxdb2: pipeline rejected record",
					"plugin", p.Name(),
					"process", rec.Process,
					"err", err)
			}
		} else {
			accepted = 1
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"received": 1,
		"accepted": accepted,
	})
}

// buildRecord maps an InfluxDB 2.x notification payload to a Record.
//
// Mapping (mirrors src/snooze/plugins/core/influxdb2/falcon/route.py):
//
//   - Source: "influxdb2".
//   - Severity: media.severity wins; otherwise _level mapped — crit→critical,
//     warn→warning, info→info, ok/normal→ok, unknown/other passes through.
//   - Message: _message.
//   - Process: media.process if present, else _source_measurement.
//   - Timestamp: _status_timestamp interpreted as epoch seconds (zero or
//     missing falls back to time.Now().UTC()).
//   - State: "close" when the resolved level (after mapping) is "ok" — this
//     matches the AlertManager plugin's resolved-alert convention. The Python
//     plugin emits the severity as-is and lets downstream rules close the
//     alert; this Go port sets state directly so the pipeline picks up the
//     resolution without an extra rule. (See Python-fidelity note in the
//     package-level doc.)
//   - Raw: the original deep-copied payload (matches Python `alert['raw']`).
func buildRecord(media map[string]any) snoozetypes.Record {
	rawCopy := deepCopyMap(media)

	level := stringField(media, "_level")
	mapped := mapLevel(level)

	severity := stringField(media, "severity")
	if severity == "" {
		severity = mapped
	}

	process := stringField(media, "process")
	if process == "" {
		process = stringField(media, "_source_measurement")
	}

	rec := snoozetypes.Record{
		Source:   "influxdb2",
		Severity: severity,
		Message:  stringField(media, "_message"),
		Process:  process,
		Raw:      rawCopy,
	}

	if mapped == "ok" {
		rec.State = "close"
	}

	if ts := epochField(media, "_status_timestamp"); !ts.IsZero() {
		rec.Timestamp = ts
	} else {
		rec.Timestamp = time.Now().UTC()
	}

	return rec
}

// mapLevel translates InfluxDB 2's `_level` enum into the Snooze severity
// vocabulary. Unknown / missing levels pass through unchanged so the
// pipeline can still surface them.
//
//   - "crit"   → "critical"
//   - "warn"   → "warning"
//   - "info"   → "info"
//   - "ok"     → "ok"
//   - "normal" → "ok"   (Python alias)
//   - other    → the raw value (or "" when missing)
func mapLevel(level string) string {
	switch level {
	case "crit":
		return "critical"
	case "warn":
		return "warning"
	case "normal":
		return "ok"
	default:
		return level
	}
}

// stringField fetches a string-typed field; returns "" if missing or not a
// string.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// epochField interprets a numeric field as epoch seconds. Returns the zero
// time when the field is missing, non-numeric, or zero.
func epochField(m map[string]any, key string) time.Time {
	v, ok := m[key]
	if !ok {
		return time.Time{}
	}
	var sec int64
	switch n := v.(type) {
	case float64:
		sec = int64(n)
	case float32:
		sec = int64(n)
	case int:
		sec = int64(n)
	case int32:
		sec = int64(n)
	case int64:
		sec = n
	case json.Number:
		if parsed, err := n.Int64(); err == nil {
			sec = parsed
		} else {
			return time.Time{}
		}
	default:
		return time.Time{}
	}
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// deepCopyMap clones a JSON-decoded map[string]any so the Record.Raw payload
// is independent of the request decoder's buffer. It walks nested maps and
// slices to mirror Python's `copy.deepcopy`.
func deepCopyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCopyAny(v)
	}
	return out
}

func deepCopyAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopyMap(t)
	case []any:
		cp := make([]any, len(t))
		for i := range t {
			cp[i] = deepCopyAny(t[i])
		}
		return cp
	default:
		return v
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
