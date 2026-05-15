// Package notification implements the `notification` core Processor plugin.
//
// A notification entry pairs a Condition with a set of named action targets.
// When a record matches the entry's condition (and falls inside the entry's
// time-constraint window), the plugin publishes one message per action to the
// process-wide message bus. Concrete delivery is performed asynchronously by
// the matching Notifier plugins (mail/webhook/script/patlite, Phase 5).
//
// # Bus-access choice
//
// internal/plugins.Host exposes only a narrow Bus surface (`Close() error`)
// to avoid pulling the syncer/mq packages into the plugin contract. To publish
// without widening that contract we runtime-assert that the value returned by
// Host.Bus() also satisfies a local `publisher` interface (Publish(ctx, queue,
// payload)). The internal/mq.Bus implementations all satisfy this shape.
// If the assertion fails (e.g. a stripped-down test host), Process logs once
// and degrades to a no-op dispatch — matching the Python behaviour of
// gracefully skipping when a configured action plugin is not loaded.
package notification

import (
	"context"
	_ "embed"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/timeconstraints"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// metaYAML is the raw metadata.yaml content embedded at build time.
//
//go:embed metadata.yaml
var metaYAML []byte

// collectionName is the database collection the plugin owns.
const collectionName = "notification"

// publisher is the slice of mq.Bus the dispatcher needs at runtime. The
// notification plugin asserts Host.Bus() satisfies this — see package doc.
type publisher interface {
	Publish(ctx context.Context, queue string, payload any) error
}

// Action is one entry in a notification's `actions` array on the wire. The
// Python code only stores the action name (a string) — this Go port accepts
// the same shape via UnmarshalJSON.
type actionRef = string

// Entry mirrors one row in the `notification` collection. Field names match
// the wire shape used by the Python backend.
type Entry struct {
	UID             string                `json:"uid,omitempty"`
	Name            string                `json:"name"`
	Enabled         *bool                 `json:"enabled,omitempty"`
	Condition       condition.Cond        `json:"condition,omitempty"`
	TimeConstraints timeconstraints.Group `json:"time_constraints,omitempty"`
	Actions         []actionRef           `json:"actions,omitempty"`
	Frequency       *Frequency            `json:"frequency,omitempty"`
}

// IsEnabled reports the effective enabled flag (default true if unset).
func (e Entry) IsEnabled() bool {
	if e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

// Frequency carries the throttle parameters for repeated delivery. The values
// are forwarded to the Notifier plugin in the bus payload — the dispatcher
// itself does not perform throttling.
type Frequency struct {
	Total int `json:"total,omitempty"`
	Delay int `json:"delay,omitempty"`
	Every int `json:"every,omitempty"`
}

// Payload is the structure the Notifier plugins receive over the bus. The
// shape mirrors the `action_obj` dict the Python implementation passes to
// `action_plugin.send`.
type Payload struct {
	NotificationName string             `json:"notification"`
	Action           string             `json:"action"`
	Record           snoozetypes.Record `json:"record"`
	Delay            int                `json:"delay,omitempty"`
	Every            int                `json:"every,omitempty"`
	Total            int                `json:"total,omitempty"`
	Retry            int                `json:"retry,omitempty"`
	Freq             int                `json:"freq,omitempty"`
}

// Plugin is the notification dispatcher.
//
// Lifecycle: Register → factory → PostInit (loads entries from the DB) →
// Process (per record) → Reload (re-reads entries on collection change).
//
// Concurrency: Process and Reload may race; entries are guarded by a RWMutex
// and missing-bus warnings are de-duplicated via warnedNoBus.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	mu      sync.RWMutex
	entries []Entry

	// warnedNoBus tracks whether we've already logged the "bus does not
	// satisfy publisher" warning, so the warning fires once per process
	// even when many records flow through Process.
	warnedNoBus atomic.Bool
}

// Name returns the registry key. Returned lowercase so it matches the
// HTTP path segment in api/openapi.yaml's PluginPath enum
// (metadata.yaml's `name:` is human-readable and may be capitalised).
func (p *Plugin) Name() string { return "notification" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host in and performs the initial entry load.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.host = host
	return p.Reload(ctx)
}

// Reload re-reads the notification collection from the database. Failures
// surface to the syncer; on transient errors the previous cache is retained
// to keep the pipeline running.
func (p *Plugin) Reload(ctx context.Context) error {
	if p.host == nil || p.host.DB() == nil {
		// Nothing to load against; leave the cache untouched.
		return nil
	}
	docs, _, err := p.host.DB().Search(ctx, collectionName, condition.Cond{}, db.Page{})
	if err != nil {
		return fmt.Errorf("notification: load entries: %w", err)
	}
	entries := make([]Entry, 0, len(docs))
	for _, d := range docs {
		e, ok := decodeEntry(d)
		if !ok {
			// A malformed row would otherwise wedge the cache; drop it
			// after logging and keep going. Python silently disables.
			if lg := p.host.Logger(); lg != nil {
				lg.Warn("notification: skipping malformed entry", "doc", d)
			}
			continue
		}
		entries = append(entries, e)
	}
	p.mu.Lock()
	p.entries = entries
	p.mu.Unlock()
	return nil
}

// Process inspects rec and publishes one message per matching action on the
// bus. The verdict is always ActionContinue: notification is a side-effect,
// not a pipeline gate.
//
// Records in the ack/close states are skipped to match Python.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	if rec.State == "ack" || rec.State == "close" {
		return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
	}

	p.mu.RLock()
	entries := p.entries
	p.mu.RUnlock()
	if len(entries) == 0 {
		return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
	}

	// Translate the typed record once for condition evaluation.
	recMap := recordToMap(rec)
	now := recordTime(rec)

	pub := p.publisher()
	for _, e := range entries {
		if !e.IsEnabled() {
			continue
		}
		if !condition.Match(recMap, e.Condition) {
			continue
		}
		if !e.TimeConstraints.Match(now) {
			continue
		}
		p.dispatch(ctx, pub, e, rec)
	}

	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

// dispatch publishes one message per configured action on the entry. Missing
// publisher (test wiring) or per-publish errors are logged once but do not
// fail the pipeline — delivery is at-most-once best-effort by design.
func (p *Plugin) dispatch(ctx context.Context, pub publisher, e Entry, rec snoozetypes.Record) {
	if len(e.Actions) == 0 {
		return
	}
	if pub == nil {
		if !p.warnedNoBus.Swap(true) {
			if lg := p.host.Logger(); lg != nil {
				lg.Warn("notification: bus does not satisfy publisher; dispatch is a no-op",
					"plugin", p.Name())
			}
		}
		return
	}

	freqTotal, freqDelay, freqEvery := 1, 0, 0
	if e.Frequency != nil {
		if e.Frequency.Total > 0 {
			freqTotal = e.Frequency.Total
		}
		freqDelay = e.Frequency.Delay
		freqEvery = e.Frequency.Every
	}

	retry, every := p.configDefaults()

	for _, action := range e.Actions {
		payload := Payload{
			NotificationName: e.Name,
			Action:           action,
			Record:           rec,
			Delay:            freqDelay,
			Every:            freqEvery,
			Total:            freqTotal,
			Retry:            retry,
			Freq:             every,
		}
		if err := pub.Publish(ctx, busQueue(action), payload); err != nil {
			if lg := p.host.Logger(); lg != nil {
				lg.Warn("notification: publish failed",
					"notification", e.Name,
					"action", action,
					"err", err)
			}
		}
	}
}

// configDefaults pulls the notification_retry and notification_freq fallbacks
// from the immutable bootstrap config. Returns Python defaults when the host
// or config is unavailable.
func (p *Plugin) configDefaults() (retry, everySeconds int) {
	retry, everySeconds = 3, int(time.Minute/time.Second)
	if p.host == nil {
		return
	}
	cfg := p.host.Config()
	if cfg == nil {
		return
	}
	if cfg.Notification.NotificationRetry > 0 {
		retry = cfg.Notification.NotificationRetry
	}
	if d := time.Duration(cfg.Notification.NotificationFreq); d > 0 {
		everySeconds = int(d / time.Second)
	}
	return
}

// publisher returns the host bus cast to the publisher contract, or nil if
// the bus is missing or does not satisfy it.
func (p *Plugin) publisher() publisher {
	if p.host == nil {
		return nil
	}
	b := p.host.Bus()
	if b == nil {
		return nil
	}
	pub, ok := any(b).(publisher)
	if !ok {
		return nil
	}
	return pub
}

// busQueue returns the topic an action's worker consumes. The naming scheme
// is "notification.<action-name>" — flat, predictable, and trivially
// shardable per action plugin.
func busQueue(action string) string {
	return "notification." + action
}

// factory is the plugins.Factory entry-point.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

func init() {
	plugins.Register("notification", metaYAML, factory)
}
