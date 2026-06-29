// Package forward implements the "forward" alert-federation plugin: a
// DataModel+Processor pair (the same shape as aggregaterule) that mirrors
// accepted alerts to one or more downstream Snooze/HTTP peers.
//
// Each destination is a CRUD-able, condition-scoped row in the `forward`
// collection (endpoint + auth + event-class flags). After the pipeline accepts
// a record, Process fire-and-forget POSTs the post-pipeline record JSON to each
// matching enabled destination with an `X-Snooze-Loop` header carrying the
// chain of server ids the alert has already traversed (this server's
// `syncer.hostname` appended). A peer that finds its own id already in the
// chain accepts the alert but does not re-relay, breaking loops in cyclic
// (hub-and-spoke / active-active) topologies.
//
// Relay is best-effort: a slow or failing peer never blocks or errors local
// ingestion — Process always returns ActionContinue with a nil error, and the
// HTTP round-trip runs on a detached, bounded-timeout goroutine.
package forward

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/pluginimpl/webhook"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

// collectionName is the storage collection for forward-destination definitions.
const collectionName = "forward"

// relayTimeout caps a single relay POST. The pipeline goroutine returns
// immediately; the detached relay goroutine enforces this deadline so a hung
// peer cannot leak goroutines indefinitely. Mirrors notification's
// notifierSendTimeout intent.
const relayTimeout = 30 * time.Second

func init() {
	plugins.Register("forward", metaYAML, factory)
}

var (
	_ plugins.DataModel = (*Plugin)(nil)
	_ plugins.Processor = (*Plugin)(nil)
)

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, Now: time.Now}, nil
}

// Plugin is the forward federation Processor.
type Plugin struct {
	meta plugins.Metadata

	// Now returns the moment a relay is timestamped. Defaults to time.Now;
	// tests inject a deterministic clock (satisfies the injected-clock rule).
	Now func() time.Time

	mu    sync.RWMutex
	dests map[string][]destination // tenantID → compiled destinations
	host  plugins.Host
}

// destination is the in-memory, pre-compiled form of one forward-collection row.
type destination struct {
	uid          string
	name         string
	enabled      bool
	endpoint     string
	cond         *condition.Compiled
	eventClasses []string
	auth         webhook.Auth
	tlsInsecure  bool
	timeout      time.Duration
}

// Name returns the registered plugin identifier.
func (p *Plugin) Name() string { return "forward" }

// Metadata returns the static descriptor parsed from metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// Schema returns the JSON Schema for a forward-destination document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":          map[string]any{"type": "string"},
			"enabled":       map[string]any{"type": "boolean"},
			"endpoint":      map[string]any{"type": "string"},
			"condition":     map[string]any{},
			"event_classes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"auth": map[string]any{"type": "object", "properties": map[string]any{
				"type":     map[string]any{"type": "string", "enum": []any{"", "bearer", "basic", "apikey"}},
				"token":    map[string]any{"type": "string"},
				"username": map[string]any{"type": "string"},
				"password": map[string]any{"type": "string"},
				"api_key":  map[string]any{"type": "string"},
				"header":   map[string]any{"type": "string"},
			}},
			"tls_insecure": map[string]any{"type": "boolean"},
			"timeout":      map[string]any{"type": "integer"},
		},
		"additionalProperties": true,
	}
}

// validEventClasses are the event_classes values accepted today. Only "alerts"
// (and the "*" wildcard) is meaningful: Snooze has no action/delete federation
// surface yet. The field is kept for forward-compat but unknown values are
// rejected at write time.
var validEventClasses = map[string]bool{"*": true, "alerts": true}

// Validate enforces structural rules. Partial PATCH bodies are tolerated: a
// field is only checked when present.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if raw, ok := obj["endpoint"]; ok {
		if s, _ := raw.(string); strings.TrimSpace(s) == "" {
			return errors.New("forward: endpoint must not be empty")
		}
	}
	if raw, ok := obj["event_classes"]; ok {
		for _, ec := range toStringSlice(raw) {
			if !validEventClasses[ec] {
				return fmt.Errorf("forward: unknown event_class %q (allowed: \"*\", \"alerts\")", ec)
			}
		}
	}
	return nil
}

// PostInit captures the Host and primes the destination cache.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.mu.Lock()
	p.host = host
	if p.Now == nil {
		p.Now = time.Now
	}
	p.mu.Unlock()
	return p.Reload(ctx)
}

// Reload refreshes the in-memory destination snapshot for the tenant in ctx
// from the forward collection. A context with no tenant is silently skipped.
func (p *Plugin) Reload(ctx context.Context) error {
	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	if host == nil || host.DB() == nil {
		return nil
	}
	tenantID, ok := auth.TenantFrom(ctx)
	if !ok || tenantID == "" {
		return nil
	}
	docs, _, err := host.DB().Search(ctx, collectionName, condition.Cond{}, db.Page{})
	if err != nil {
		return fmt.Errorf("forward: reload: %w", err)
	}
	dests := make([]destination, 0, len(docs))
	for _, d := range docs {
		dest, cerr := compileDestination(d)
		if cerr != nil {
			if lg := host.Logger(); lg != nil {
				lg.Warn("forward: skipping invalid destination",
					"uid", d["uid"], "name", d["name"], "err", cerr)
			}
			continue
		}
		dests = append(dests, dest)
	}
	p.mu.Lock()
	if p.dests == nil {
		p.dests = make(map[string][]destination)
	}
	p.dests[tenantID] = dests
	p.mu.Unlock()
	return nil
}

// Process relays the accepted record to each matching enabled destination.
// Loop prevention: if this server's id is already in the inbound X-Snooze-Loop
// chain, the alert is left to persist locally but is NOT relayed. The verdict
// is always ActionContinue — federation is a side-effect, never a pipeline gate.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	chain := auth.LoopChainFrom(ctx)

	p.mu.RLock()
	host := p.host
	tenantID, _ := auth.TenantFrom(ctx)
	dests := p.dests[tenantID]
	p.mu.RUnlock()

	self := ""
	if host != nil && host.Config() != nil {
		self = host.Config().Syncer.Hostname
	}

	// This server already saw the alert (cyclic topology) → accept locally but
	// do not re-relay.
	if self != "" && slices.Contains(chain, self) {
		return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
	}

	if len(dests) > 0 {
		recMap := recordToMap(rec)
		// Body is the post-pipeline record JSON the peer ingests (already
		// normalized), not the raw inbound body. Encode once and share.
		body, err := json.Marshal(rec)
		if err != nil {
			if host != nil && host.Logger() != nil {
				host.Logger().Warn("forward: marshal record for relay", "err", err)
			}
			body = nil
		}
		outChain := slices.Clone(chain)
		if self != "" {
			outChain = append(outChain, self)
		}
		loopHeader := strings.Join(outChain, ",")
		relayedAt := p.now().UTC().Format(time.RFC3339)

		for _, d := range dests {
			if !d.enabled {
				continue
			}
			if !coversAlerts(d.eventClasses) {
				continue
			}
			if d.cond != nil && !d.cond.Match(recMap) {
				continue
			}
			// Skip a destination already present in the inbound chain
			// (Alerta's per-remote is_in_xloop).
			if d.name != "" && slices.Contains(chain, d.name) {
				continue
			}
			if body == nil {
				continue
			}
			p.relay(d, body, loopHeader, relayedAt)
		}
	}

	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

// now returns the plugin's clock (injected in tests, time.Now in production).
// Used to stamp the relay's X-Snooze-Relayed-At header so the relay path never
// reaches time.Now() directly (the injected-clock rule).
func (p *Plugin) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// relay fires a fire-and-forget POST to one destination on a detached goroutine
// bounded by relayTimeout. Peer failures are logged at WARN (nil-guarded) and
// never propagate to the pipeline.
func (p *Plugin) relay(d destination, body []byte, loopHeader, relayedAt string) {
	host := p.host
	go func() { //nolint:gosec // detached by design: the request ctx is cancelled when Process returns.
		// The context deadline uses the real monotonic clock (a frozen test
		// clock must not pre-cancel a live relay); relayedAt carries the
		// injected-clock timestamp on the wire instead.
		ctx, cancel := context.WithTimeout(context.Background(), relayTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
		if err != nil {
			warn(host, "forward: build relay request", d, err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Snooze-Loop", loopHeader)
		if relayedAt != "" {
			req.Header.Set("X-Snooze-Relayed-At", relayedAt)
		}
		if err := webhook.ApplyAuth(req, d.auth); err != nil {
			warn(host, "forward: apply destination auth", d, err)
			return
		}

		client := webhook.NewClient(webhook.Config{
			URL:         d.endpoint,
			TLSInsecure: d.tlsInsecure,
			Timeout:     d.timeout,
		})
		resp, err := client.Do(req)
		if err != nil {
			warn(host, "forward: relay POST failed", d, err)
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			warn(host, "forward: peer rejected relay", d, fmt.Errorf("HTTP %d", resp.StatusCode))
		}
	}()
}

// warn logs a relay failure if a logger is available. Federation failures are
// best-effort: they must never surface to the pipeline.
func warn(host plugins.Host, msg string, d destination, err error) {
	if host == nil || host.Logger() == nil {
		return
	}
	host.Logger().Warn(msg, "destination", d.name, "endpoint", d.endpoint, "err", err)
}

// coversAlerts reports whether the destination's event_classes admits the
// "alerts" event class. An empty list defaults to covering alerts (the only
// meaningful class today).
func coversAlerts(classes []string) bool {
	if len(classes) == 0 {
		return true
	}
	for _, c := range classes {
		if c == "*" || c == "alerts" {
			return true
		}
	}
	return false
}

// --- compilation ---

func compileDestination(d db.Document) (destination, error) {
	name, _ := d["name"].(string)
	if name == "" {
		return destination{}, errors.New("destination has no name")
	}
	endpoint, _ := d["endpoint"].(string)
	if strings.TrimSpace(endpoint) == "" {
		return destination{}, errors.New("destination has no endpoint")
	}
	enabled := true
	if v, ok := d["enabled"].(bool); ok {
		enabled = v
	}
	c, err := condFromDoc(d["condition"])
	if err != nil {
		return destination{}, fmt.Errorf("condition: %w", err)
	}
	cp, err := condition.Compile(c)
	if err != nil {
		return destination{}, fmt.Errorf("condition: %w", err)
	}
	dst := destination{
		uid:          stringField(d, "uid"),
		name:         name,
		enabled:      enabled,
		endpoint:     endpoint,
		cond:         cp,
		eventClasses: toStringSlice(d["event_classes"]),
		auth:         authFromDoc(d["auth"]),
		tlsInsecure:  boolField(d, "tls_insecure"),
		timeout:      durationField(d["timeout"]),
	}
	return dst, nil
}

// authFromDoc decodes the destination auth sub-document into a webhook.Auth so
// the relay reuses webhook's single auth code path.
func authFromDoc(v any) webhook.Auth {
	m, ok := v.(map[string]any)
	if !ok {
		return webhook.Auth{}
	}
	return webhook.Auth{
		Type:     strings.ToLower(stringField(m, "type")),
		Token:    stringField(m, "token"),
		Username: stringField(m, "username"),
		Password: stringField(m, "password"),
		APIKey:   stringField(m, "api_key"),
		Header:   stringField(m, "header"),
	}
}

// condFromDoc accepts the dual-format condition representation: legacy
// nested-list form or the structured object form.
func condFromDoc(v any) (condition.Cond, error) {
	switch x := v.(type) {
	case nil:
		return condition.Cond{}, nil
	case []any:
		if len(x) == 0 {
			return condition.Cond{}, nil
		}
		return condition.FromList(x)
	case map[string]any:
		var out condition.Cond
		b, err := json.Marshal(x)
		if err != nil {
			return condition.Cond{}, err
		}
		if err := out.UnmarshalJSON(b); err != nil {
			return condition.Cond{}, err
		}
		return out, nil
	default:
		return condition.Cond{}, fmt.Errorf("unsupported condition shape %T", v)
	}
}

// --- value helpers ---

func stringField(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func boolField(m map[string]any, k string) bool {
	v, _ := m[k].(bool)
	return v
}

// durationField interprets a stored timeout as seconds (the JSON/DB number
// form) or as a Go duration string. Zero/absent → 0 (the relay falls back to
// webhook's default timeout).
func durationField(v any) time.Duration {
	switch x := v.(type) {
	case int:
		return time.Duration(x) * time.Second
	case int64:
		return time.Duration(x) * time.Second
	case float64:
		return time.Duration(x) * time.Second
	case string:
		if d, err := time.ParseDuration(x); err == nil {
			return d
		}
	}
	return 0
}

func toStringSlice(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		return slices.Clone(x)
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// recordToMap projects a typed Record into the map shape the condition
// evaluator consumes.
func recordToMap(rec snoozetypes.Record) map[string]any {
	m := map[string]any{}
	if rec.UID != "" {
		m["uid"] = rec.UID
	}
	if rec.Host != "" {
		m["host"] = rec.Host
	}
	if rec.Source != "" {
		m["source"] = rec.Source
	}
	if rec.Process != "" {
		m["process"] = rec.Process
	}
	if rec.Severity != "" {
		m["severity"] = rec.Severity
	}
	if rec.Message != "" {
		m["message"] = rec.Message
	}
	if !rec.Timestamp.IsZero() {
		m["timestamp"] = rec.Timestamp
	}
	if rec.DateEpoch != 0 {
		m["date_epoch"] = rec.DateEpoch
	}
	if rec.TTL != 0 {
		m["ttl"] = rec.TTL
	}
	if rec.Environment != "" {
		m["environment"] = rec.Environment
	}
	if rec.Hash != "" {
		m["hash"] = rec.Hash
	}
	if len(rec.Tags) > 0 {
		m["tags"] = rec.Tags
	}
	if len(rec.Raw) > 0 {
		m["raw"] = rec.Raw
	}
	if rec.State != "" {
		m["state"] = rec.State
	}
	if len(rec.Plugins) > 0 {
		m["plugins"] = rec.Plugins
	}
	for k, v := range rec.Extra {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return m
}
