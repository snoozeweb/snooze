// Package kapacitor implements the `kapacitor` WebhookReceiver plugin.
//
// Kapacitor's `http()` alert handler POSTs a JSON envelope containing an
// `id`, `message`, `level`, `time`, and an InfluxDB-style `data.series[]`
// payload. Each series carries `tags`, `columns`, and tabular `values`.
// This plugin maps every series entry to one snoozetypes.Record and submits
// each record to the host's processing pipeline.
//
// # Series fan-out
//
// The Python implementation in src/snooze/plugins/core/kapacitor produces
// one alert per `data.series` entry: a Kapacitor alert that emits N series
// (e.g. several hosts in one query) generates N records. The Go port keeps
// the same semantics. When `data.series` is missing or empty the handler
// returns a 200 envelope with received=0 — Kapacitor only sends data for
// state changes, but it occasionally emits keepalive payloads that would
// otherwise look like errors.
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid
// pulling internal/core into the plugin contract. As in the alertmanager
// plugin, this package runtime-asserts that the Host value also satisfies
// a local recordProcessor interface — *core.Core satisfies that shape. If
// the assertion fails (stripped-down test host), HandleWebhook logs once
// and degrades to a no-op, mirroring the notification plugin's bus-access
// pattern.
package kapacitor

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
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
	plugins.Register("kapacitor", metaYAML, factory)
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

// Plugin is the Kapacitor webhook receiver.
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
// The full external URL is therefore /api/v1/webhook/kapacitor.
func (p *Plugin) WebhookPath() string { return "/kapacitor" }

// kapSeries is one InfluxDB-style sub-series of a Kapacitor alert payload.
type kapSeries struct {
	Name    string            `json:"name"`
	Tags    map[string]string `json:"tags"`
	Columns []string          `json:"columns"`
	Values  [][]any           `json:"values"`
}

// kapData wraps the series array.
type kapData struct {
	Series []kapSeries `json:"series"`
}

// kapPayload is the Kapacitor http() handler envelope.
type kapPayload struct {
	ID       string            `json:"id"`
	Message  string            `json:"message"`
	Details  string            `json:"details"`
	Time     time.Time         `json:"time"`
	Duration int64             `json:"duration"`
	Level    string            `json:"level"`
	Data     kapData           `json:"data"`
	Tags     map[string]string `json:"tags"`
}

// HandleWebhook decodes the Kapacitor payload, fans it out across the
// `data.series` entries, maps each to a snoozetypes.Record, and submits
// each record to the pipeline. The reply is a small JSON envelope describing
// how many records were accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var hook kapPayload
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&hook); err != nil {
		http.Error(w, fmt.Sprintf("invalid Kapacitor payload: %v", err), http.StatusBadRequest)
		return
	}

	proc := p.recordProcessor()
	if proc == nil {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("kapacitor: host does not satisfy recordProcessor; webhook is a no-op",
					"plugin", p.Name())
			}
		}
	}

	records := buildRecords(hook)

	ctx := r.Context()
	accepted := 0
	for _, rec := range records {
		if proc != nil {
			if _, _, err := proc.ProcessRecord(ctx, rec); err != nil {
				if lg := p.logger(); lg != nil {
					lg.Warn("kapacitor: pipeline rejected record",
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

// buildRecords fans the payload out across data.series. When the series
// array is empty the payload is still acknowledged but produces zero records
// — matching the Python plugin which iterates an empty list silently.
func buildRecords(hook kapPayload) []snoozetypes.Record {
	if len(hook.Data.Series) == 0 {
		return nil
	}
	out := make([]snoozetypes.Record, 0, len(hook.Data.Series))
	for _, s := range hook.Data.Series {
		out = append(out, buildRecord(hook, s))
	}
	return out
}

// buildRecord maps one (envelope, series) pair to a snoozetypes.Record.
//
// Mapping (mirrors the Python plugin where the contract overlaps):
//
//   - Host: series.tags["host"] then series.tags["instance"]; "" when absent.
//   - Source: "kapacitor".
//   - Severity: series.tags["severity"] (verbatim, matches Python's pop),
//     falling back to normalised level (CRITICAL→critical, WARNING→warning,
//     INFO→info, OK→info), defaulting to "critical" when level is missing.
//   - Message: envelope.message.
//   - Process: series.tags["process"] then envelope.id.
//   - Timestamp: envelope.time; falls back to time.Now() when zero.
//   - Tags: sorted series.tags keys (other than the popped host/process/
//     severity), prefixed with the keys from envelope.tags. Matches the
//     spec: "Tags = data.series[].tags keys".
//   - State: "close" when level is "OK" (the alert resolved).
//   - Raw: full envelope, JSON-round-tripped to map[string]any so downstream
//     plugins (rule, aggregaterule) can index into it.
func buildRecord(hook kapPayload, s kapSeries) snoozetypes.Record {
	level := strings.ToUpper(strings.TrimSpace(hook.Level))

	// Pull and pop the special tag keys; everything else becomes a Tag entry.
	tags := make(map[string]string, len(s.Tags))
	for k, v := range s.Tags {
		tags[k] = v
	}
	host := popTag(tags, "host")
	if host == "" {
		host = popTag(tags, "instance")
	}
	process := popTag(tags, "process")
	if process == "" {
		process = hook.ID
	}
	severityRaw := popTag(tags, "severity")

	rec := snoozetypes.Record{
		Source:    "kapacitor",
		Host:      host,
		Severity:  resolveSeverity(severityRaw, level),
		Message:   hook.Message,
		Process:   process,
		Timestamp: hook.Time,
		Tags:      collectTagKeys(hook.Tags, tags),
	}

	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}
	if level == "OK" {
		rec.State = "close"
	}

	rec.Raw = rawFromPayload(hook)

	return rec
}

// popTag removes the named key from m and returns its value, or "" when
// absent.
func popTag(m map[string]string, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	delete(m, key)
	return v
}

// resolveSeverity picks the record severity following Python's precedence:
// a per-series `severity` tag wins (used verbatim), otherwise the alert
// level is normalised. Returns "critical" when nothing is available, the
// same default the Python plugin uses.
func resolveSeverity(seriesSeverity, level string) string {
	if seriesSeverity != "" {
		return seriesSeverity
	}
	switch level {
	case "CRITICAL":
		return "critical"
	case "WARNING":
		return "warning"
	case "INFO":
		return "info"
	case "OK":
		return "info"
	case "":
		return "critical"
	default:
		// Unknown levels fall through as lower-cased so operators see the
		// raw value instead of losing it.
		return strings.ToLower(level)
	}
}

// collectTagKeys returns the union of envelope and series tag keys (with the
// special-cased ones already popped from `series`), deduplicated and
// sorted for deterministic output.
func collectTagKeys(envelopeTags, seriesTags map[string]string) []string {
	if len(envelopeTags) == 0 && len(seriesTags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(envelopeTags)+len(seriesTags))
	for k := range envelopeTags {
		seen[k] = struct{}{}
	}
	for k := range seriesTags {
		seen[k] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// rawFromPayload turns the envelope into a map[string]any for Record.Raw.
// The round-trip preserves nested structures (data.series with mixed-type
// values) without forcing this package to learn each downstream consumer's
// schema, matching the Python plugin's `alert['raw'] = media` line.
func rawFromPayload(hook kapPayload) map[string]any {
	raw, err := json.Marshal(hook)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
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
