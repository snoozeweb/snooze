// Package apikey implements the "apikey" data-model plugin: the admin surface
// over user API keys. CRUD is auto-mounted at /api/v1/apikey gated on the
// implicit ro_apikey / rw_apikey permissions. Keys are MINTED via the
// self-service routes (/api/v1/user/me/apikeys), not through this plugin —
// GuardWrite blocks generic creates so subset-of-caller enforcement is never
// bypassed. The store and this plugin share auth.APIKeyCollection.
package apikey

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register(auth.APIKeyCollection, metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the apikey data-model plugin.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

var (
	_ plugins.DataModel    = (*Plugin)(nil)
	_ plugins.PrimaryKeyer = (*Plugin)(nil)
	_ plugins.WriteGuard   = (*Plugin)(nil)
)

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return auth.APIKeyCollection }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, h plugins.Host) error {
	p.host = h
	return nil
}

// Reload is a no-op: keys are not cached.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// PrimaryKey scopes uniqueness to (tenant, owner, name).
func (p *Plugin) PrimaryKey() []string { return []string{"tenant_id", "owner", "name"} }

// Schema returns the JSON Schema for an apikey document.
func (p *Plugin) Schema() any {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "number"}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"owner":        str,
			"owner_method": str,
			"name":         str,
			"key_prefix":   str,
			"permissions":  map[string]any{"type": "array", "items": str},
			"groups":       map[string]any{"type": "array", "items": str},
			"created_at":   num,
			"expires_at":   num,
			"revoked_at":   num,
		},
		"additionalProperties": true,
	}
}

// Validate accepts any well-formed map; integrity is enforced in GuardWrite.
func (p *Plugin) Validate(_ map[string]any) error { return nil }

// protectedFields may never be mutated through the admin CRUD route.
var protectedFields = []string{"owner", "owner_method", "key_hash", "key_prefix", "permissions", "tenant_id", "created_at"}

// GuardWrite forbids minting/replacing keys through the generic CRUD route and
// protects security-sensitive fields on edit. Admins may PATCH name/expires_at
// and revoke (revoked_at); revocation/deletion is the real admin lever.
func (p *Plugin) GuardWrite(_ context.Context, uid string, doc map[string]any, replace bool) error {
	if uid == "" {
		return errors.New("apikey: keys are created via POST /api/v1/user/me/apikeys, not here")
	}
	if replace {
		return errors.New("apikey: full replacement (PUT) is not allowed; use PATCH to edit name/expiry or DELETE to revoke")
	}
	for _, f := range protectedFields {
		if _, present := doc[f]; present {
			return fmt.Errorf("apikey: field %q is immutable", f)
		}
	}
	return nil
}
