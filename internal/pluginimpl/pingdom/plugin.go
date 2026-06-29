// Package pingdom implements the `pingdom` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts Pingdom
// uptime check state-change webhooks, maps each payload to a single
// snoozetypes.Record, and submits the record to the host's processing pipeline.
//
// # Payload shape (Pingdom webhook v1)
//
//	{
//	  "check_id":          12345,
//	  "check_name":        "My homepage",
//	  "check_type":        "HTTP",
//	  "tags":              [{"name": "production"}, {"name": "web"}],
//	  "previous_state":    "UP",
//	  "current_state":     "DOWN",
//	  "importance_level":  "HIGH",
//	  "state_changed_timestamp": 1700000000,
//	  "description":       "Host is Down",
//	  "long_description":  "Real browser test failed (step 3/5)"
//	}
//
// # Mapping
//
//   - check_name                → Record.Host
//   - "pingdom" (constant)       → Record.Source
//   - check_type                 → Record.Process
//   - description                → Record.Message
//   - importance_level+state     → Record.Severity (see mapSeverity)
//   - current_state == "UP"      → Record.State = "close"
//   - tags [{"name":…}]          → Record.Tags ([]string, flattened)
//   - check_id                   → Record.Raw["check_id"]
//   - long_description           → Record.Raw["long_description"]
//   - state_changed_timestamp    → Record.Timestamp (Unix epoch → UTC)
//
// A DOWN event produces a warning/critical record; an UP event produces a
// record with State="close" and Severity="ok" that closes the prior DOWN.
// PAUSED/UNKNOWN states map to Severity="unknown" with no State.
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid pulling
// internal/core into the plugin contract. The plugin therefore runtime-asserts
// that the Host value also satisfies a local recordProcessor interface —
// *core.Core satisfies this shape. If the assertion fails (a stripped-down test
// host), HandleWebhook logs once and degrades to a no-op, matching the pattern
// used by internal/pluginimpl/newrelic.
package pingdom

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
	plugins.Register("pingdom", metaYAML, factory)
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

// Plugin is the Pingdom uptime state-change webhook receiver.
//
// Lifecycle: Register → factory → PostInit (captures the host) → HandleWebhook
// per incoming POST. There is no persistent state to load or reload.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// warnedNoProcessor tracks whether we have already logged the "host does
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
// The full external URL is therefore /api/v1/webhook/pingdom.
func (p *Plugin) WebhookPath() string { return "/pingdom" }

// HandleWebhook decodes the incoming Pingdom webhook, maps it to a single
// snoozetypes.Record, and submits the record to the pipeline. The reply is a
// small JSON envelope with {status, received, accepted}.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// UseNumber preserves numeric precision so state_changed_timestamp and
	// check_id survive the round-trip without float coercion.
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		http.Error(w, fmt.Sprintf("invalid Pingdom payload: %v", err), http.StatusBadRequest)
		return
	}

	rec := buildRecord(raw)

	proc := p.recordProcessor()
	if proc == nil {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("pingdom: host does not satisfy recordProcessor; webhook is a no-op",
					"plugin", p.Name())
			}
		}
	}

	accepted := 0
	if proc != nil {
		if _, _, err := proc.ProcessRecord(r.Context(), rec); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("pingdom: pipeline rejected record",
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

// buildRecord maps a decoded Pingdom webhook payload to a single
// snoozetypes.Record. It is a pure function: no logging, no I/O, unit-testable
// without a server context. Missing keys degrade to zero values rather than
// erroring, so a partial or future-shaped payload still yields a valid record.
func buildRecord(raw map[string]any) snoozetypes.Record {
	checkName, _ := raw["check_name"].(string)
	checkType, _ := raw["check_type"].(string)
	currentState, _ := raw["current_state"].(string)
	importanceLevel, _ := raw["importance_level"].(string)
	description, _ := raw["description"].(string)

	recRaw := map[string]any{}
	if v, ok := raw["check_id"]; ok {
		recRaw["check_id"] = v
	}
	if v, ok := raw["long_description"].(string); ok && v != "" {
		recRaw["long_description"] = v
	}

	rec := snoozetypes.Record{
		Source:    "pingdom",
		Host:      checkName,
		Process:   checkType,
		Message:   description,
		Severity:  mapSeverity(currentState, importanceLevel),
		Tags:      flattenTags(raw),
		Timestamp: eventTimestamp(raw),
		Raw:       recRaw,
	}
	if currentState == "UP" {
		rec.State = "close"
	}
	return rec
}

// mapSeverity converts a Pingdom current_state / importance_level pair to a
// snooze severity keyword:
//
//   - "UP"             → "ok"
//   - "DOWN" + "HIGH"  → "critical"
//   - "DOWN" + "LOW"   → "warning"
//   - anything else    → "unknown" (covers PAUSED/UNKNOWN/empty states)
func mapSeverity(currentState, importanceLevel string) string {
	switch currentState {
	case "UP":
		return "ok"
	case "DOWN":
		switch importanceLevel {
		case "HIGH":
			return "critical"
		case "LOW":
			return "warning"
		}
	}
	return "unknown"
}

// flattenTags extracts the "name" string from each element of the payload's
// "tags" list (Pingdom sends `[{"name":"x"}, …]`) into a []string. A missing
// or non-list "tags" key yields nil (not an empty slice); non-map elements and
// maps without a string "name" are skipped without panicking.
func flattenTags(raw map[string]any) []string {
	list, ok := raw["tags"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, elem := range list {
		m, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["name"].(string); ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}

// eventTimestamp reads state_changed_timestamp (a json.Number Unix epoch) and
// returns the corresponding UTC time. It falls back to time.Now().UTC() when
// the field is absent, unparseable, or zero.
func eventTimestamp(raw map[string]any) time.Time {
	if n, ok := raw["state_changed_timestamp"].(json.Number); ok {
		if ts, err := n.Int64(); err == nil && ts != 0 {
			return time.Unix(ts, 0).UTC()
		}
	}
	return time.Now().UTC()
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
