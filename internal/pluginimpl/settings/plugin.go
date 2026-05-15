// Package settings implements the "settings" data-model plugin AND the
// production implementation of config.RuntimeStore. Each setting is
// stored as one document per section, keyed by {"section": <name>}. The
// document's "values" field holds the section payload as a JSON object;
// Get/Set/Replace operate on that field.
//
// Watch fans out RuntimeChange events by per-section subscriber channels.
// Notifications are best-effort: a slow subscriber is dropped on overflow,
// matching the Python "no back-pressure" semantics.
package settings

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/japannext/snooze/internal/config"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

// Collection is the storage collection name for settings documents.
const Collection = "settings"

func init() {
	plugins.Register("settings", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for runtime settings. It also implements
// config.RuntimeStore so the orchestrator can wire it as the live
// settings backend for other subsystems.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	subsMu sync.Mutex
	subs   map[string][]chan config.RuntimeChange
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "settings" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	p.subsMu.Lock()
	if p.subs == nil {
		p.subs = make(map[string][]chan config.RuntimeChange)
	}
	p.subsMu.Unlock()
	return nil
}

// Reload is a no-op: settings are read on demand from the database.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema descriptor for a settings document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"section": map[string]any{"type": "string"},
			"values":  map[string]any{"type": "object"},
		},
		"required":             []any{"section"},
		"additionalProperties": true,
	}
}

// Validate enforces a non-empty section name on full writes; partial PATCH
// updates that omit the section are tolerated.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["section"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("settings: section must not be empty")
		}
	}
	return nil
}

// --- config.RuntimeStore -------------------------------------------------

// Get implements config.RuntimeStore.
func (p *Plugin) Get(ctx context.Context, section, key string) (any, bool, error) {
	values, err := p.readSection(ctx, section)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	v, ok := values[key]
	return v, ok, nil
}

// GetSection implements config.RuntimeStore. dst must be a pointer; the
// section's value map is JSON-round-tripped into it.
func (p *Plugin) GetSection(ctx context.Context, section string, dst any) error {
	if dst == nil {
		return errors.New("settings: GetSection: nil dst")
	}
	values, err := p.readSection(ctx, section)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("settings: encode section %q: %w", section, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("settings: decode section %q: %w", section, err)
	}
	return nil
}

// Set implements config.RuntimeStore.
func (p *Plugin) Set(ctx context.Context, section, key string, value any) error {
	if section == "" {
		return errors.New("settings: section must not be empty")
	}
	if key == "" {
		return errors.New("settings: key must not be empty")
	}
	values, err := p.readSection(ctx, section)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	if values == nil {
		values = map[string]any{}
	}
	values[key] = value
	if err := p.upsert(ctx, section, values); err != nil {
		return err
	}
	p.fanout(section, key, value)
	return nil
}

// Replace implements config.RuntimeStore.
func (p *Plugin) Replace(ctx context.Context, section string, values map[string]any) error {
	if section == "" {
		return errors.New("settings: section must not be empty")
	}
	if values == nil {
		values = map[string]any{}
	}
	if err := p.upsert(ctx, section, values); err != nil {
		return err
	}
	// Emit a single change event keyed on the section, with an empty key.
	p.fanout(section, "", values)
	return nil
}

// Watch implements config.RuntimeStore. The returned channel is closed
// when ctx is cancelled. Sends are non-blocking; a slow subscriber may miss
// events (matching the Python broadcast semantics).
func (p *Plugin) Watch(ctx context.Context, section string) (<-chan config.RuntimeChange, error) {
	ch := make(chan config.RuntimeChange, 8)
	p.subsMu.Lock()
	if p.subs == nil {
		p.subs = make(map[string][]chan config.RuntimeChange)
	}
	p.subs[section] = append(p.subs[section], ch)
	p.subsMu.Unlock()

	go func() {
		<-ctx.Done()
		p.subsMu.Lock()
		list := p.subs[section]
		for i, c := range list {
			if c == ch {
				p.subs[section] = append(list[:i], list[i+1:]...)
				break
			}
		}
		p.subsMu.Unlock()
		close(ch)
	}()
	return ch, nil
}

// GetSettings returns the entire stored payload for a section as a fresh
// map. It is a convenience wrapper around Get/GetSection used by the
// orchestrator's settings export route.
func (p *Plugin) GetSettings(ctx context.Context, section string) (map[string]any, error) {
	values, err := p.readSection(ctx, section)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if values == nil {
		return map[string]any{}, nil
	}
	// Defensive copy so callers can mutate freely.
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out, nil
}

// SetSettings replaces the entire stored payload for a section. Equivalent
// to Replace; kept under a friendlier name for the import-settings route.
func (p *Plugin) SetSettings(ctx context.Context, section string, values map[string]any) error {
	return p.Replace(ctx, section, values)
}

// --- internal helpers -------------------------------------------------------

func (p *Plugin) readSection(ctx context.Context, section string) (map[string]any, error) {
	if p.host == nil || p.host.DB() == nil {
		return nil, errors.New("settings: host not initialised")
	}
	doc, err := p.host.DB().GetOne(ctx, Collection, db.Document{"section": section})
	if err != nil {
		return nil, err
	}
	switch v := doc["values"].(type) {
	case map[string]any:
		return v, nil
	case nil:
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("settings: section %q: values is %T, want object", section, v)
	}
}

func (p *Plugin) upsert(ctx context.Context, section string, values map[string]any) error {
	if p.host == nil || p.host.DB() == nil {
		return errors.New("settings: host not initialised")
	}
	doc := db.Document{"section": section, "values": values}
	// ReplaceOne with upsert=true so the first Set/Replace creates the row.
	_, err := p.host.DB().ReplaceOne(ctx, Collection, db.Document{"section": section}, doc, true)
	return err
}

func (p *Plugin) fanout(section, key string, value any) {
	p.subsMu.Lock()
	subs := append([]chan config.RuntimeChange(nil), p.subs[section]...)
	p.subsMu.Unlock()
	evt := config.RuntimeChange{Section: section, Key: key, Value: value}
	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			// Drop on overflow. Subscribers that need every event must
			// drain promptly.
		}
	}
}

// AfterCreate implements plugins.CreateHook. Every successful POST to
// /api/v1/settings invalidates the runtime cache so the new key/value
// pair is visible to the next subsystem read (LDAP backend, housekeeper,
// notification scheduler, ...). The hook is best-effort; a missing host
// or runtime resolver is silently tolerated.
func (p *Plugin) AfterCreate(_ context.Context, _ []map[string]any) error {
	p.invalidateRuntime()
	return nil
}

// AfterUpdate implements plugins.UpdateHook for the same reason as
// AfterCreate.
func (p *Plugin) AfterUpdate(_ context.Context, _ string, _ map[string]any) error {
	p.invalidateRuntime()
	return nil
}

// AfterDelete implements plugins.DeleteHook for the same reason as
// AfterCreate.
func (p *Plugin) AfterDelete(_ context.Context, _ []string) error {
	p.invalidateRuntime()
	return nil
}

// invalidateRuntime drops the cached runtime settings snapshot, if a
// RuntimeSettingsHost is wired up. No-op for tests that supply a bare
// plugins.Host fixture.
func (p *Plugin) invalidateRuntime() {
	if p.host == nil {
		return
	}
	rsh, ok := p.host.(plugins.RuntimeSettingsHost)
	if !ok {
		return
	}
	rs := rsh.RuntimeSettings()
	if rs == nil {
		return
	}
	rs.Invalidate()
}

// Compile-time interface checks.
var (
	_ plugins.Plugin      = (*Plugin)(nil)
	_ plugins.DataModel   = (*Plugin)(nil)
	_ plugins.CreateHook  = (*Plugin)(nil)
	_ plugins.UpdateHook  = (*Plugin)(nil)
	_ plugins.DeleteHook  = (*Plugin)(nil)
	_ config.RuntimeStore = (*Plugin)(nil)
)
