// Package kv implements the "kv" data-model plugin: a simple key-value
// store keyed by (dict, key). The rule plugin's KV_SET op consumes values
// from this collection via a Get helper.
//
// Items are cached in memory after each Reload so reads don't touch the
// database on the hot path; PostInit hydrates the cache and the syncer's
// auto_reload triggers a Reload when the collection changes elsewhere.
//
// The kv collection is tenant-scoped, so the cache is keyed by tenant and
// every read must name one. A flat cache would let whichever tenant reloaded
// last serve its values to all the others.
package kv

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sync"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("kv", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for the key-value collection.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	mu   sync.RWMutex
	data map[string]map[string]map[string]any // tenantID -> dict -> key -> value
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "kv" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host and hydrates the in-memory cache.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.host = host
	return p.Reload(ctx)
}

// Reload refreshes the cached bucket for the tenant in ctx from the database.
// A context with no tenant is silently skipped, matching every other
// tenant-scoped plugin's Reload: the syncer deliberately includes a tenant-less
// entry in its reload fan-out for the benefit of global collections, and kv is
// not one of them.
//
// Only the ctx tenant's bucket is replaced. Rebuilding the whole map here would
// evict every other tenant on each reload.
func (p *Plugin) Reload(ctx context.Context) error {
	if p.host == nil || p.host.DB() == nil {
		// Host not yet wired (e.g. unit-test plugin without PostInit): keep
		// the current cache. PostInit is the canonical hydration point.
		return nil
	}
	tenantID, ok := auth.TenantFrom(ctx)
	if !ok || tenantID == "" {
		return nil
	}
	docs, _, err := p.host.DB().Search(ctx, "kv", condition.Cond{}, db.Page{})
	if err != nil {
		return fmt.Errorf("kv: reload: %w", err)
	}
	next := make(map[string]map[string]any, len(docs))
	for _, d := range docs {
		dict, _ := d["dict"].(string)
		key, _ := d["key"].(string)
		if dict == "" || key == "" {
			continue
		}
		if next[dict] == nil {
			next[dict] = map[string]any{}
		}
		next[dict][key] = d["value"]
	}
	p.mu.Lock()
	if p.data == nil {
		p.data = make(map[string]map[string]map[string]any, 1)
	}
	p.data[tenantID] = next
	p.mu.Unlock()
	return nil
}

// Schema returns the JSON Schema for a kv document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dict":  map[string]any{"type": "string"},
			"key":   map[string]any{"type": "string"},
			"value": map[string]any{},
		},
		"required":             []any{"dict", "key"},
		"additionalProperties": true,
	}
}

// Validate enforces non-empty (dict, key) on full writes; partial PATCH
// updates that omit either field are tolerated.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["dict"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("kv: dict must not be empty")
		}
	}
	if v, ok := obj["key"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("kv: key must not be empty")
		}
	}
	return nil
}

// Get returns the cached value for (dict, key) within ctx's tenant. The bool
// is false if the pair has not been written for that tenant.
//
// A hit is only ever served from the caller's own tenant bucket. Callers must
// consult TenantLoaded first to tell a genuine miss from an unhydrated tenant.
func (p *Plugin) Get(ctx context.Context, dict, key string) (any, bool) {
	tenantID, ok := auth.TenantFrom(ctx)
	if !ok || tenantID == "" {
		return nil, false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	d, ok := p.data[tenantID][dict]
	if !ok {
		return nil, false
	}
	v, ok := d[key]
	return v, ok
}

// TenantLoaded reports whether ctx's tenant has been hydrated into the cache.
//
// It distinguishes "this key does not exist" from "this tenant's bucket was
// never loaded", so a caller can fall back to the database in the second case
// rather than treating it as a definitive miss. The gap it covers is a tenant
// created after boot, whose bucket stays empty until the next kv write or the
// syncer's periodic full reload.
func (p *Plugin) TenantLoaded(ctx context.Context) bool {
	tenantID, ok := auth.TenantFrom(ctx)
	if !ok || tenantID == "" {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, loaded := p.data[tenantID]
	return loaded
}
