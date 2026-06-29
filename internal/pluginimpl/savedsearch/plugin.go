// Package savedsearch implements the "savedsearch" data-model plugin: named
// alert-filter bookmarks. A saved search is a human label (`name`) paired with
// a raw Snooze condition-DSL string (`query`). CRUD is auto-mounted at
// /api/v1/savedsearch and scoped per (tenant_id, owner, name): the tenant_id is
// injected by the multitenancy middleware, and the owner is stamped from the
// JWT subject (TransformWrite) so a client cannot forge another operator's
// bookmarks. GuardWrite / GuardDelete block cross-user mutation, with an admin
// exception so an administrator can curate stale entries.
package savedsearch

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("savedsearch", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for the savedsearch collection.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Compile-time guarantees that the plugin keeps satisfying the optional
// interfaces the CRUD layer detects by assertion — so a signature drift fails
// the build rather than silently disabling the hook.
var (
	_ plugins.DataModel        = (*Plugin)(nil)
	_ plugins.PrimaryKeyer     = (*Plugin)(nil)
	_ plugins.WriteTransformer = (*Plugin)(nil)
	_ plugins.WriteGuard       = (*Plugin)(nil)
	_ plugins.DeleteGuard      = (*Plugin)(nil)
)

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "savedsearch" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: saved searches are not cached.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// PrimaryKey scopes uniqueness to (tenant, owner, name) so two operators (or
// two tenants) may each have a bookmark called "prod criticals", but a single
// operator cannot silently overwrite their own (duplicate_policy: reject).
func (p *Plugin) PrimaryKey() []string { return []string{"tenant_id", "owner", "name"} }

// Schema returns the JSON Schema for a savedsearch document. `name` and `query`
// are required non-empty strings; `additionalProperties: true` leaves room for
// forward-compatible fields (e.g. a future `shared` flag).
func (p *Plugin) Schema() any {
	str := map[string]any{"type": "string", "minLength": 1}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uid":       map[string]any{"type": "string"},
			"name":      str,
			"query":     str,
			"owner":     map[string]any{"type": "string"},
			"tenant_id": map[string]any{"type": "string"},
		},
		"required":             []any{"name", "query"},
		"additionalProperties": true,
	}
}

// Validate enforces non-empty (name, query) on full writes; partial PATCH
// updates that omit either field are tolerated (only the present fields are
// checked).
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["name"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("savedsearch: name must not be empty")
		}
	}
	if v, ok := obj["query"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("savedsearch: query must not be empty")
		}
	}
	return nil
}

// TransformWrite stamps the authenticated principal as `owner`. The owner is
// server-authoritative — any client-supplied value is overwritten with the
// request's verified subject so a caller cannot create bookmarks attributed to
// another operator. Unauthenticated contexts leave `owner` untouched (the
// guard then rejects the write).
func (p *Plugin) TransformWrite(ctx context.Context, doc map[string]any) error {
	if claims, ok := auth.ClaimsFrom(ctx); ok && claims.Subject != "" {
		doc["owner"] = claims.Subject
	}
	return nil
}

// GuardWrite blocks a create/replace/patch whose target `owner` differs from
// the caller's verified subject, unless the caller is an admin. It runs after
// TransformWrite, so on a normal owner write the stamped `owner` already equals
// the subject; this guard is the defense against an admin-route caller (or a
// future code path) supplying a foreign owner.
func (p *Plugin) GuardWrite(ctx context.Context, _ string, doc map[string]any, _ bool) error {
	subject, isAdmin := principal(ctx)
	if isAdmin {
		return nil
	}
	owner, _ := doc["owner"].(string)
	if owner != "" && owner != subject {
		return fmt.Errorf("savedsearch: cannot modify saved searches owned by %q", owner)
	}
	return nil
}

// GuardDelete blocks deleting another operator's saved searches unless the
// caller is an admin. It loads each target document and compares its `owner`
// to the caller's subject.
func (p *Plugin) GuardDelete(ctx context.Context, uids []string) error {
	subject, isAdmin := principal(ctx)
	if isAdmin {
		return nil
	}
	if p.host == nil || p.host.DB() == nil {
		return nil
	}
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		doc, err := p.host.DB().GetOne(ctx, "savedsearch", db.Document{"uid": uid})
		if err != nil {
			return fmt.Errorf("savedsearch: lookup %s: %w", uid, err)
		}
		if doc == nil {
			continue
		}
		owner, _ := doc["owner"].(string)
		if owner != "" && owner != subject {
			return fmt.Errorf("savedsearch: cannot delete saved searches owned by %q", owner)
		}
	}
	return nil
}

// principal returns the caller's verified subject and whether they hold the
// admin or platform_admin role.
func principal(ctx context.Context) (subject string, isAdmin bool) {
	claims, ok := auth.ClaimsFrom(ctx)
	if !ok {
		return "", false
	}
	admin := slices.Contains(claims.Roles, "admin") ||
		slices.Contains(claims.Roles, auth.PlatformAdminRole)
	return claims.Subject, admin
}
