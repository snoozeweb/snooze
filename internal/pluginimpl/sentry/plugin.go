// Package sentry implements the `sentry` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts both Sentry
// payload shapes:
//
//   - Legacy "webhook" plugin shape: a flat JSON object with top-level fields
//     id, project, project_name, culprit, message, url, level, server_name, and
//     an event sub-object containing event_id, tags, environment, and release.
//
//   - Modern "Integration" issue/event alert shape: an object with a top-level
//     "data" key wrapping either data.event or data.issue, plus an optional
//     action field ("triggered", "created", or "resolved").
//
// Shape detection is done by the presence of a top-level "data" key in the raw
// JSON: if present → modern; otherwise → legacy.
//
// # Field mapping
//
//   - Source:   "sentry"
//   - Severity: mapped from the Sentry level field
//     (fatal/error → critical, warning → warning, info/debug → info; unknown → critical)
//   - Host:     server_name or tags["server_name"] (legacy); fallback to project / project_name
//   - Process:  project slug or project_name
//   - Message:  message (legacy) or culprit or issue.title
//   - State:    "close" when action == "resolved"
//   - Raw:      url/permalink, project, event_id, environment, release, level
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
package sentry

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// sentrySignatureHeader is the request header carrying Sentry's HMAC-SHA256
// signature of the raw request body.
const sentrySignatureHeader = "sentry-hook-signature"

// metaYAML is the raw metadata.yaml content embedded at build time.
//
//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("sentry", metaYAML, factory)
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

// Plugin is the Sentry webhook receiver.
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
// The full external URL is therefore /api/v1/webhook/sentry.
func (p *Plugin) WebhookPath() string { return "/sentry" }

// ---------------------------------------------------------------------------
// Payload types — legacy shape
// ---------------------------------------------------------------------------

// sentryEvent represents the nested event sub-object in the legacy payload.
type sentryEvent struct {
	EventID     string `json:"event_id"`
	Tags        any    `json:"tags"` // [[k, v], ...] or map[string]any
	Environment string `json:"environment"`
	Release     string `json:"release"`
}

// sentryLegacyPayload represents the Sentry legacy "webhook" plugin shape.
// Fields are present at the top level alongside an `event` sub-object.
type sentryLegacyPayload struct {
	ID          string      `json:"id"`
	Project     string      `json:"project"`
	ProjectName string      `json:"project_name"`
	Culprit     string      `json:"culprit"`
	Message     string      `json:"message"`
	URL         string      `json:"url"`
	Level       string      `json:"level"`
	ServerName  string      `json:"server_name"`
	Event       sentryEvent `json:"event"`
}

// ---------------------------------------------------------------------------
// Payload types — modern shape
// ---------------------------------------------------------------------------

// sentryIssue is the nested issue object in modern Integration payloads.
type sentryIssue struct {
	Title   string `json:"title"`
	Culprit string `json:"culprit"`
	Level   string `json:"level"`
	Link    string `json:"permalink"`
	Project struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"project"`
}

// sentryModernEvent is the nested event object in modern Integration payloads.
type sentryModernEvent struct {
	EventID     string `json:"event_id"`
	Level       string `json:"level"`
	Message     string `json:"message"`
	Title       string `json:"title"`
	Culprit     string `json:"culprit"`
	Environment string `json:"environment"`
	Release     string `json:"release"`
	ServerName  string `json:"server_name"`
	Tags        any    `json:"tags"` // [[k, v], ...] or map[string]any
	Project     string `json:"project"`
}

// sentryModernData holds the data field of a modern Integration payload.
type sentryModernData struct {
	Event         *sentryModernEvent `json:"event"`
	Issue         *sentryIssue       `json:"issue"`
	TriggeredRule string             `json:"triggered_rule"`
}

// sentryModernPayload represents the Sentry modern issue/event alert shape.
// Detected by the presence of a top-level "data" key.
type sentryModernPayload struct {
	Action string           `json:"action"`
	Data   sentryModernData `json:"data"`
}

// ---------------------------------------------------------------------------
// HandleWebhook
// ---------------------------------------------------------------------------

// HandleWebhook decodes the Sentry webhook payload (both legacy and modern
// shapes), maps it to a snoozetypes.Record, and submits the record to the
// pipeline. The reply is a small JSON envelope describing how many records
// were received and accepted.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the RAW request body first. The HMAC must be computed over the exact
	// bytes Sentry signed, so we cannot let the JSON decoder consume the reader
	// before verification; we buffer the bytes and decode from the buffer.
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid Sentry payload: %v", err), http.StatusBadRequest)
		return
	}

	// Opt-in HMAC verification. When Ingest.SentrySecret is empty (the default)
	// this is a no-op and behavior is identical to today (no header required).
	if !p.verifySignature(r, raw) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	// Decode the full body into a raw map so we can detect the payload shape
	// without re-reading a consumed io.Reader, then re-unmarshal into the
	// appropriate typed struct.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var rawMap map[string]json.RawMessage
	if err := dec.Decode(&rawMap); err != nil {
		http.Error(w, fmt.Sprintf("invalid Sentry payload: %v", err), http.StatusBadRequest)
		return
	}

	var rec snoozetypes.Record

	if _, hasData := rawMap["data"]; hasData {
		// Modern Integration payload.
		// Re-encode to JSON and decode into the typed struct.
		fullJSON, err := json.Marshal(rawMap)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid Sentry payload: %v", err), http.StatusBadRequest)
			return
		}
		var modern sentryModernPayload
		if err := json.Unmarshal(fullJSON, &modern); err != nil {
			http.Error(w, fmt.Sprintf("invalid Sentry payload: %v", err), http.StatusBadRequest)
			return
		}
		rec = buildModernRecord(modern)
	} else {
		// Legacy webhook payload.
		fullJSON, err := json.Marshal(rawMap)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid Sentry payload: %v", err), http.StatusBadRequest)
			return
		}
		var legacy sentryLegacyPayload
		if err := json.Unmarshal(fullJSON, &legacy); err != nil {
			http.Error(w, fmt.Sprintf("invalid Sentry payload: %v", err), http.StatusBadRequest)
			return
		}
		rec = buildLegacyRecord(legacy)
	}

	records := []snoozetypes.Record{rec}

	proc := p.recordProcessor()
	if proc == nil && len(records) > 0 {
		if !p.warnedNoProcessor.Swap(true) {
			if lg := p.logger(); lg != nil {
				lg.Warn("sentry: host does not satisfy recordProcessor; webhook is a no-op",
					"plugin", p.Name())
			}
		}
	}

	ctx := r.Context()
	accepted := 0
	for _, r := range records {
		if proc != nil {
			if _, _, err := proc.ProcessRecord(ctx, r); err != nil {
				if lg := p.logger(); lg != nil {
					lg.Warn("sentry: pipeline rejected record",
						"plugin", p.Name(),
						"host", r.Host,
						"process", r.Process,
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

// ---------------------------------------------------------------------------
// Record builders
// ---------------------------------------------------------------------------

// buildLegacyRecord maps a sentryLegacyPayload to a snoozetypes.Record.
func buildLegacyRecord(hook sentryLegacyPayload) snoozetypes.Record {
	// Host: server_name → event.tags["server_name"] → project / project_name.
	host := hook.ServerName
	if host == "" {
		host = tagsServerName(hook.Event.Tags)
	}
	if host == "" {
		host = hook.Project
	}
	if host == "" {
		host = hook.ProjectName
	}

	// Process: project slug → project_name.
	process := hook.Project
	if process == "" {
		process = hook.ProjectName
	}

	// Message: message → culprit.
	message := hook.Message
	if message == "" {
		message = hook.Culprit
	}

	severity := mapSeverity(hook.Level)

	raw := map[string]any{}
	if hook.URL != "" {
		raw["url"] = hook.URL
	}
	if hook.Project != "" {
		raw["project"] = hook.Project
	}
	if hook.Event.EventID != "" {
		raw["event_id"] = hook.Event.EventID
	}
	if hook.Event.Environment != "" {
		raw["environment"] = hook.Event.Environment
	}
	if hook.Event.Release != "" {
		raw["release"] = hook.Event.Release
	}
	if hook.Level != "" {
		raw["level"] = hook.Level
	}

	return snoozetypes.Record{
		Source:    "sentry",
		Host:      host,
		Process:   process,
		Severity:  severity,
		Message:   message,
		Timestamp: time.Now().UTC(),
		Raw:       raw,
	}
}

// buildModernRecord maps a sentryModernPayload to a snoozetypes.Record.
func buildModernRecord(p sentryModernPayload) snoozetypes.Record {
	state := ""
	if p.Action == "resolved" {
		state = "close"
	}

	var host, process, message, level, url, eventID, environment, release string

	if p.Data.Issue != nil {
		issue := p.Data.Issue
		level = issue.Level
		message = issue.Title
		if message == "" {
			message = issue.Culprit
		}
		url = issue.Link
		process = issue.Project.Slug
		if process == "" {
			process = issue.Project.Name
		}
		// For modern issue payloads there is usually no server_name; use project slug.
		host = issue.Project.Slug
		if host == "" {
			host = issue.Project.Name
		}
	}

	if p.Data.Event != nil {
		ev := p.Data.Event
		if level == "" {
			level = ev.Level
		}
		if message == "" {
			message = ev.Message
			if message == "" {
				message = ev.Title
			}
			if message == "" {
				message = ev.Culprit
			}
		}
		// Prefer server_name from the event when available; override the issue's
		// project-slug-based host only when a real server_name is present.
		if ev.ServerName != "" {
			host = ev.ServerName
		} else if sn := tagsServerName(ev.Tags); sn != "" {
			host = sn
		}
		if process == "" {
			process = ev.Project
		}
		if eventID == "" {
			eventID = ev.EventID
		}
		if environment == "" {
			environment = ev.Environment
		}
		if release == "" {
			release = ev.Release
		}
	}

	// Resolve events default to info when no level is available.
	if level == "" && state == "close" {
		level = "info"
	}
	severity := mapSeverity(level)

	raw := map[string]any{}
	if url != "" {
		raw["url"] = url
	}
	if process != "" {
		raw["project"] = process
	}
	if eventID != "" {
		raw["event_id"] = eventID
	}
	if environment != "" {
		raw["environment"] = environment
	}
	if release != "" {
		raw["release"] = release
	}
	if level != "" {
		raw["level"] = level
	}

	return snoozetypes.Record{
		Source:    "sentry",
		Host:      host,
		Process:   process,
		Severity:  severity,
		Message:   message,
		Timestamp: time.Now().UTC(),
		Raw:       raw,
		State:     state,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mapSeverity converts a Sentry level string to a snooze severity keyword.
// fatal/error → critical, warning → warning, info/debug → info, unknown → critical.
func mapSeverity(level string) string {
	switch level {
	case "fatal", "error":
		return "critical"
	case "warning":
		return "warning"
	case "info", "debug":
		return "info"
	default:
		return "critical"
	}
}

// tagsServerName extracts the server_name value from a Sentry tags field.
// Sentry tags can be either [[k, v], ...] arrays or map[string]any; we handle both.
func tagsServerName(tags any) string {
	if tags == nil {
		return ""
	}
	// Array of [k, v] pairs.
	if pairs, ok := tags.([]any); ok {
		for _, item := range pairs {
			if pair, ok := item.([]any); ok && len(pair) == 2 {
				if k, ok := pair[0].(string); ok && k == "server_name" {
					if v, ok := pair[1].(string); ok {
						return v
					}
				}
			}
		}
	}
	// Object / map.
	if m, ok := tags.(map[string]any); ok {
		if v, ok := m["server_name"].(string); ok {
			return v
		}
	}
	return ""
}

// verifySignature implements opt-in inbound HMAC verification.
//
// When Ingest.SentrySecret is empty (the default) it returns true unchanged —
// behavior is identical to today and no header is required. When a secret is
// configured it computes HMAC-SHA256 over the RAW request body keyed by the
// secret, hex-encodes it, and constant-time-compares it against the
// sentry-hook-signature header. A missing or mismatching header → false.
func (p *Plugin) verifySignature(r *http.Request, body []byte) bool {
	secret := ""
	if p.host != nil {
		if cfg := p.host.Config(); cfg != nil {
			secret = cfg.Ingest.SentrySecret
		}
	}
	if secret == "" {
		return true // verification disabled (default)
	}

	got := r.Header.Get(sentrySignatureHeader)
	if got == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	// Constant-time compare over the hex-encoded forms.
	return hmac.Equal([]byte(got), []byte(want))
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
