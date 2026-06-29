package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ProxyAuthenticator JIT-provisions and resolves a trusted-header (auth-proxy)
// user. It is the in-process engine behind the auth middleware's proxy branch:
// given an Identity already parsed from the request headers, it ensures a
// method="proxy" user document exists (when auto-signup is on), keeps its
// groups in sync with the upstream IdP, resolves roles/permissions via the
// shared RoleResolver, and returns ephemeral per-request Claims that are never
// signed or persisted (the proxy is the session authority).
type ProxyAuthenticator struct {
	DB       db.Driver
	Resolver *RoleResolver
	// Clock is injected so tests can pin created_at/updated_at. Defaults to
	// time.Now when nil.
	Clock func() time.Time
	// Method is the identity method tag stamped on the user document and the
	// Claims.Method (cfg.AuthProxy.Method, default "proxy").
	Method string
}

// NewProxyAuthenticator wires a provisioner to a driver + resolver. method is
// the identity method tag (cfg.AuthProxy.Method).
func NewProxyAuthenticator(driver db.Driver, resolver *RoleResolver, method string) *ProxyAuthenticator {
	return &ProxyAuthenticator{DB: driver, Resolver: resolver, Method: method}
}

// now returns the injected clock or wall time.
func (p *ProxyAuthenticator) now() time.Time {
	if p.Clock != nil {
		return p.Clock()
	}
	return time.Now()
}

// Authenticate provisions/syncs the proxy user from id and returns the Claims
// to stamp on the request context.
//
// Flow (mirrors EnsureRoot's upsert shape):
//  1. Look up the user by {name, method}.
//  2. If absent and !autoSignup → ErrUserNotProvisioned (no write).
//  3. If absent and autoSignup → create the user (no password field).
//  4. On every call re-sync groups + updated_at (idempotent upsert) so RBAC
//     tracks the IdP.
//  5. Resolve roles/permissions and build the ephemeral Claims (no exp/iss).
func (p *ProxyAuthenticator) Authenticate(ctx context.Context, id Identity, autoSignup bool) (snoozetypes.Claims, error) {
	if p.DB == nil {
		return snoozetypes.Claims{}, errors.New("proxy auth: nil db driver")
	}
	if p.Resolver == nil {
		return snoozetypes.Claims{}, errors.New("proxy auth: nil resolver")
	}

	method := p.Method
	if method == "" {
		method = schema.DefaultAuthProxy().Method
	}
	tenantID := id.TenantID
	if tenantID == "" {
		tenantID = snoozetypes.DefaultTenant
	}

	existing, err := p.DB.GetOne(ctx, LocalCollection, db.Document{
		"name":   id.Username,
		"method": method,
	})
	switch {
	case err == nil && existing != nil:
		// Known user: re-sync groups (and updated_at via UpdateTime) so the
		// role mapping tracks the IdP. The upsert is keyed on {name, method},
		// so only groups change — created_at/password are left untouched.
		if syncErr := p.upsertUser(ctx, db.Document{
			"name":   id.Username,
			"method": method,
			"groups": normalizeGroups(id.Groups),
		}); syncErr != nil {
			return snoozetypes.Claims{}, syncErr
		}
	case errors.Is(err, db.ErrNotFound):
		if !autoSignup {
			return snoozetypes.Claims{}, ErrUserNotProvisioned
		}
		// JIT-provision a fresh proxy user. No password field — proxy users
		// never authenticate locally.
		if createErr := p.upsertUser(ctx, db.Document{
			"tenant_id":  tenantID,
			"name":       id.Username,
			"method":     method,
			"enabled":    true,
			"groups":     normalizeGroups(id.Groups),
			"created_at": p.now().UTC().Format(time.RFC3339),
		}); createErr != nil {
			return snoozetypes.Claims{}, createErr
		}
	default:
		return snoozetypes.Claims{}, fmt.Errorf("proxy auth: lookup user: %w", err)
	}

	// Resolve roles/permissions from the (now synced) user + group mapping. The
	// resolver reads the same {name, method} document and the role collection.
	resolveID := Identity{Username: id.Username, Method: method, TenantID: tenantID, Groups: id.Groups}
	roles, perms, err := p.Resolver.Resolve(ctx, resolveID)
	if err != nil {
		return snoozetypes.Claims{}, fmt.Errorf("proxy auth: resolve roles: %w", err)
	}

	return snoozetypes.Claims{
		Subject:     id.Username,
		Method:      method,
		TenantID:    tenantID,
		Roles:       roles,
		Permissions: perms,
		Groups:      id.Groups,
		// No exp/iss/iat: ephemeral per-request claims, never signed.
	}, nil
}

// upsertUser writes doc to the local user collection keyed on {name, method},
// stamping updated_at via UpdateTime. Idempotent.
func (p *ProxyAuthenticator) upsertUser(ctx context.Context, doc db.Document) error {
	if _, err := p.DB.Write(ctx, LocalCollection, []db.Document{doc}, db.WriteOptions{
		Primary:    []string{"name", "method"},
		UpdateTime: true,
	}); err != nil {
		return fmt.Errorf("proxy auth: write user: %w", err)
	}
	return nil
}

// normalizeGroups returns a non-nil slice so the stored doc carries an explicit
// (possibly empty) groups field rather than a missing key.
func normalizeGroups(groups []string) []string {
	if groups == nil {
		return []string{}
	}
	return groups
}

// IdentityFromProxyHeaders parses the upstream-proxy identity headers into an
// auth.Identity. It returns ok=false when the user header is absent or blank
// (the caller falls through to the normal Bearer/key path). Groups are split on
// cfg.GroupsSep with surrounding whitespace trimmed and empty entries dropped.
// TenantID is left at DefaultTenant here; Plan 29 layers attribute->tenant
// resolution on top.
func IdentityFromProxyHeaders(r *http.Request, cfg schema.AuthProxy) (Identity, bool) {
	user := strings.TrimSpace(r.Header.Get(cfg.UserHeader))
	if user == "" {
		return Identity{}, false
	}
	method := cfg.Method
	if method == "" {
		method = schema.DefaultAuthProxy().Method
	}
	return Identity{
		Username: user,
		Method:   method,
		TenantID: snoozetypes.DefaultTenant,
		Groups:   parseGroups(r.Header.Get(cfg.GroupsHeader), cfg.GroupsSep),
	}, true
}

// parseGroups splits raw on sep, trimming whitespace and dropping empties.
func parseGroups(raw, sep string) []string {
	if raw == "" {
		return nil
	}
	if sep == "" {
		sep = ","
	}
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if g := strings.TrimSpace(p); g != "" {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
