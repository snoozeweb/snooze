// Package graylog implements the `graylog` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts a Graylog
// stream-alert HTTP notification payload, maps it to a single
// snoozetypes.Record, and submits the record to the host's processing pipeline.
//
// # Mapping
//
//   - stream.title                          → Record.Host
//   - check_result.result_description       → Record.Message
//   - check_result.triggered_condition.id   → Record.Raw["checkId"]
//   - whole payload                         → Record.Raw["payload"]
//   - "graylog" constant                    → Record.Source
//
// Query-string overrides (Alerta-faithful) supply the remaining fields:
//
//   - ?event       (default "Alert")            → Record.Raw["event"]
//   - ?environment (default "")                 → Record.Environment
//   - ?service     (default "", comma-split)    → Record.Raw["service"] ([]string)
//   - ?severity    (default "critical")         → Record.Severity
//   - ?event_type  (default "performanceAlert") → Record.Raw["event_type"]
//
// No State is set: Graylog HTTP alerts are fire-only (there is no resolved
// event in this webhook format), so the pipeline relies on rules / snooze TTL
// for de-duplication and closure.
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid
// pulling internal/core into the plugin contract. The plugin therefore
// runtime-asserts that the Host value also satisfies a local recordProcessor
// interface — *core.Core satisfies this shape. If the assertion fails (a
// stripped-down test host), HandleWebhook logs once and degrades to a no-op,
// matching the pattern used by internal/pluginimpl/grafana.
package graylog

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	plugins.Register("graylog", metaYAML, factory)
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

// Plugin is the Graylog stream-alert webhook receiver.
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
// The full external URL is therefore /api/v1/webhook/graylog.
func (p *Plugin) WebhookPath() string { return "/graylog" }

// glStream is the `stream` object of a Graylog notification.
type glStream struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// glTriggeredCondition is the `triggered_condition` object nested under
// check_result.
type glTriggeredCondition struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// glCheckResult is the `check_result` object of a Graylog notification.
type glCheckResult struct {
	ResultDescription  string               `json:"result_description"`
	TriggeredCondition glTriggeredCondition `json:"triggered_condition"`
}

// glPayload is the Graylog stream-alert HTTP notification envelope.
type glPayload struct {
	Stream      glStream      `json:"stream"`
	CheckResult glCheckResult `json:"check_result"`
}

// HandleWebhook decodes the Graylog notification payload, maps it to a single
// snoozetypes.Record, and submits the record to the pipeline. The reply is a
// small JSON envelope describing whether the record was accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dec := json.NewDecoder(r.Body)
	var payload glPayload
	if err := dec.Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("invalid Graylog payload: %v", err), http.StatusBadRequest)
		return
	}

	if payload.Stream.Title == "" {
		http.Error(w, "invalid Graylog payload: missing stream.title", http.StatusBadRequest)
		return
	}

	rec := buildRecord(payload, r.URL.Query())

	proc := p.recordProcessor()
	if proc == nil {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("graylog: host does not satisfy recordProcessor; webhook is a no-op",
					"plugin", p.Name())
			}
		}
	}

	accepted := 0
	if proc != nil {
		if _, _, err := proc.ProcessRecord(r.Context(), rec); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("graylog: pipeline rejected record",
					"plugin", p.Name(),
					"host", rec.Host,
					"err", err)
			}
		} else {
			accepted = 1
		}
	} else {
		// No pipeline (degraded test host): count as a no-op success.
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

// buildRecord maps a decoded Graylog payload plus the request query-string into
// a single snoozetypes.Record. It is a pure function: no logging, no I/O,
// unit-testable without a server context.
func buildRecord(payload glPayload, q url.Values) snoozetypes.Record {
	raw := map[string]any{
		"event":      queryOrDefault(q, "event", "Alert"),
		"event_type": queryOrDefault(q, "event_type", "performanceAlert"),
		"checkId":    payload.CheckResult.TriggeredCondition.ID,
		"payload":    payload,
	}
	if service := queryOrDefault(q, "service", ""); service != "" {
		raw["service"] = strings.Split(service, ",")
	}

	return snoozetypes.Record{
		Source:      "graylog",
		Host:        payload.Stream.Title,
		Message:     payload.CheckResult.ResultDescription,
		Severity:    queryOrDefault(q, "severity", "critical"),
		Environment: queryOrDefault(q, "environment", ""),
		Timestamp:   time.Now().UTC(),
		Raw:         raw,
	}
}

// queryOrDefault returns q[key] when present and non-empty, otherwise fallback.
func queryOrDefault(q url.Values, key, fallback string) string {
	if v := q.Get(key); v != "" {
		return v
	}
	return fallback
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
