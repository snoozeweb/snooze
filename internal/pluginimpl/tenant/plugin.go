// Package tenant implements the "tenant" data-model plugin. The tenant
// registry is a global (NOT tenant-scoped) collection: every document here
// represents one organization; the id field (immutable slug) is stamped as
// tenant_id on every scoped document. See docs/superpowers/specs/2026-06-05-multitenancy-design.md
// decisions D1, D9.
package tenant

import (
	"context"
	_ "embed"
	"errors"
	"regexp"

	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

// Collection is the global (NOT tenant-scoped) collection holding tenant
// registry documents.
const Collection = "tenant"

// Status enumerates a tenant's lifecycle state.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// Tenant is one organization. ID is the immutable URL/login-safe slug used as
// the tenant_id stamped on every scoped document; DisplayName is mutable (D9).
type Tenant struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	IngestToken string `json:"ingest_token,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
	UpdatedAt   int64  `json:"updated_at,omitempty"`
}

// slugRE matches valid tenant slugs: lowercase alphanumeric and hyphens,
// starting and ending with an alphanumeric character.
var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*[a-z0-9]$|^[a-z0-9]$`)

func init() {
	plugins.Register("tenant", metaYAML, func(meta plugins.Metadata) (plugins.Plugin, error) {
		return &Plugin{meta: meta}, nil
	})
}

// New returns a bare Plugin for tests (not going through the init registry).
func New() *Plugin {
	meta, _ := plugins.ParseMetadata(metaYAML)
	return &Plugin{meta: meta}
}

// Plugin is the data-model plugin for the tenant registry.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

func (p *Plugin) Name() string                   { return "tenant" }
func (p *Plugin) Metadata() plugins.Metadata     { return p.meta }
func (p *Plugin) Reload(_ context.Context) error { return nil }

// PostInit captures the host; the tenant collection has no in-memory cache.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// PrimaryKey returns [id]; the generic CRUD createHandler enforces uniqueness.
func (p *Plugin) PrimaryKey() []string { return []string{"id"} }

// Schema returns the JSON Schema for a tenant document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":           map[string]any{"type": "string"},
			"display_name": map[string]any{"type": "string"},
			"status":       map[string]any{"type": "string", "enum": []any{"active", "suspended"}},
			"ingest_token": map[string]any{"type": "string"},
		},
		"required":             []any{"id"},
		"additionalProperties": true,
	}
}

// Validate enforces the id slug format on writes. PATCH partials without an
// id field are tolerated; id="" is always rejected.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	v, hasID := obj["id"]
	if !hasID {
		// PATCH partial: id absent is OK.
		return nil
	}
	id, _ := v.(string)
	if id == "" {
		return errors.New("tenant: id must not be empty")
	}
	if !slugRE.MatchString(id) {
		return errors.New("tenant: id must be a lowercase URL-safe slug (letters, digits, hyphens)")
	}
	return nil
}

// AfterCreate seeds the default roles for the newly created tenant. It runs
// best-effort: an error is logged but does not roll back the create.
func (p *Plugin) AfterCreate(ctx context.Context, docs []map[string]any) error {
	if p.host == nil {
		return nil
	}
	for _, doc := range docs {
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		if err := seedDefaultRoles(ctx, p.host, id); err != nil {
			p.host.Logger().Error("tenant: seed default roles failed",
				"tenant_id", id, "err", err)
		}
	}
	return nil
}
