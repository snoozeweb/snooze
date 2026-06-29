// Package tenantmatch implements the "tenant_match" data-model plugin. The
// tenant_match registry is a global (NOT tenant-scoped) collection: each
// document maps an IdP group, an email domain, or a login (username) to a
// target tenant slug. It is a platform-level routing table consulted by the
// login flow (see internal/auth/tenant_match.go and the login routes), so it
// sits above tenant scoping and is registered global via
// db.RegisterGlobalCollection. See alerta-port-plans/29-attribute-tenant-resolution.md.
package tenantmatch

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

// Collection is the global (NOT tenant-scoped) collection holding tenant_match
// routing rules.
const Collection = "tenant_match"

// validMatchTypes is the closed set of match_type values the registry accepts.
var validMatchTypes = map[string]struct{}{
	"group":  {},
	"domain": {},
	"login":  {},
}

func init() {
	plugins.Register(Collection, metaYAML, func(meta plugins.Metadata) (plugins.Plugin, error) {
		return &Plugin{meta: meta}, nil
	})
}

// New returns a bare Plugin for tests (not going through the init registry).
func New() *Plugin {
	meta, _ := plugins.ParseMetadata(metaYAML)
	return &Plugin{meta: meta}
}

// Plugin is the data-model plugin for the tenant_match registry.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// resolver, when wired by the server at boot (SetResolver), receives the
	// in-memory rule snapshot on PostInit and every Reload. Nil in unit tests
	// that exercise validation only.
	resolver *auth.TenantMatchResolver
	// failClosed reads the tenant_match.fail_closed runtime setting. Nil ⇒ the
	// safe default (open: unmatched users fall through to DefaultTenant).
	failClosed func(ctx context.Context) bool
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return Collection }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// SetResolver wires the login-flow resolver this plugin keeps in sync. Called
// once at server wire-up, before PostInit/Reload run the initial load.
func (p *Plugin) SetResolver(r *auth.TenantMatchResolver) { p.resolver = r }

// SetFailClosedReader wires the function that reads the tenant_match.fail_closed
// runtime setting. Called once at server wire-up.
func (p *Plugin) SetFailClosedReader(fn func(ctx context.Context) bool) { p.failClosed = fn }

// PostInit captures the host, registers tenant_match as a global collection
// (must happen at boot before the HTTP server starts, so the driver never
// injects a tenant_id predicate on this platform-level routing table), and
// performs the initial resolver load when a resolver has been wired.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.host = host
	db.RegisterGlobalCollection(Collection)
	return p.Reload(ctx)
}

// Reload refreshes the in-memory resolver snapshot from the DB. It is invoked
// on PostInit and whenever the syncer reports a change to the tenant_match
// collection. A no-op when no resolver is wired (validation-only test plugins).
func (p *Plugin) Reload(ctx context.Context) error {
	if p.resolver == nil || p.host == nil {
		return nil
	}
	pctx := auth.WithPlatformScope(ctx)
	docs, _, err := p.host.DB().Search(pctx, Collection, condition.Cond{Op: condition.OpAlwaysTrue}, db.Page{})
	if err != nil {
		// Missing collection (no rules yet) is fine — load an empty registry.
		if errors.Is(err, db.ErrNotFound) {
			p.resolver.LoadFromDocs(nil, p.readFailClosed(pctx))
			return nil
		}
		return fmt.Errorf("tenant_match: reload: %w", err)
	}
	p.resolver.LoadFromDocs(docs, p.readFailClosed(pctx))
	return nil
}

// readFailClosed reads the runtime fail_closed flag, defaulting to false (open).
func (p *Plugin) readFailClosed(ctx context.Context) bool {
	if p.failClosed == nil {
		return false
	}
	return p.failClosed(ctx)
}

// PrimaryKey satisfies plugins.PrimaryKeyer: a rule is uniquely identified by
// the (match_type, match) pair. The generic CRUD createHandler enforces the
// duplicate_policy against this key.
func (p *Plugin) PrimaryKey() []string { return []string{"match_type", "match"} }

// Schema returns the JSON Schema for a tenant_match document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"match_type": map[string]any{"type": "string", "enum": []any{"group", "domain", "login"}},
			"match":      map[string]any{"type": "string"},
			"tenant_id":  map[string]any{"type": "string"},
			"priority":   map[string]any{"type": "integer", "default": 0},
		},
		"required":             []any{"match_type", "match", "tenant_id"},
		"additionalProperties": true,
	}
}

// Validate runs structural validation on a write body: match_type must be one
// of group/domain/login, and (when match_type is present, i.e. this is not a
// bare PATCH partial) match and tenant_id must be non-empty. Referential
// integrity (tenant must exist) and duplicate rejection are context- and
// DB-aware and live in TransformWrite. An empty body (PATCH partial) is OK.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	mt, hasMT := obj["match_type"]
	if !hasMT {
		// PATCH partial that does not touch match_type: nothing structural to
		// check here.
		return nil
	}
	mtStr, _ := mt.(string)
	if _, ok := validMatchTypes[mtStr]; !ok {
		return fmt.Errorf("tenant_match: match_type must be one of group, domain, login (got %q)", mtStr)
	}
	if m, _ := obj["match"].(string); m == "" {
		return errors.New("tenant_match: match must not be empty")
	}
	if tid, _ := obj["tenant_id"].(string); tid == "" {
		return errors.New("tenant_match: tenant_id must not be empty")
	}
	return nil
}

// TransformWrite is the DB-aware integrity guard. It runs after Validate with
// the trusted request context (platform scope for this global collection):
//   - referential integrity — tenant_id must name an existing tenant;
//   - duplicate rejection — no existing rule may share the (match_type, match)
//     pair (belt-and-braces over the CRUD duplicate_policy, and the only place
//     a clear domain error is produced).
//
// It does not mutate doc. A bare PATCH partial that omits match_type/match is
// tolerated (nothing DB-aware to check).
func (p *Plugin) TransformWrite(ctx context.Context, doc map[string]any) error {
	if len(doc) == 0 || p.host == nil {
		return nil
	}
	mt, _ := doc["match_type"].(string)
	m, _ := doc["match"].(string)
	tid, _ := doc["tenant_id"].(string)
	if mt == "" && m == "" {
		// PATCH partial not touching the natural key — skip DB checks.
		return nil
	}

	pctx := auth.WithPlatformScope(ctx)

	// Referential integrity: the target tenant must exist.
	if tid != "" {
		t, err := p.host.DB().GetOne(pctx, "tenant", db.Document{"id": tid})
		if err != nil || t == nil {
			return fmt.Errorf("tenant_match: tenant_id %q does not name an existing tenant", tid)
		}
	}

	// Duplicate rejection on the (match_type, match) pair.
	if mt != "" && m != "" {
		existing, _, err := p.host.DB().Search(pctx, Collection,
			condition.And(condition.Equals("match_type", mt), condition.Equals("match", m)),
			db.Page{PerPage: 1})
		if err == nil && len(existing) > 0 {
			return fmt.Errorf("tenant_match: a rule for (%s, %s) already exists", mt, m)
		}
	}
	return nil
}

// compile-time assertions that Plugin satisfies the expected interfaces.
var (
	_ plugins.Plugin           = (*Plugin)(nil)
	_ plugins.DataModel        = (*Plugin)(nil)
	_ plugins.PrimaryKeyer     = (*Plugin)(nil)
	_ plugins.WriteTransformer = (*Plugin)(nil)
)
