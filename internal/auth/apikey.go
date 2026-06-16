package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// DefaultAPIKeyMaxTTL bounds how far in the future a key may expire when the
// configured cap is unset.
const DefaultAPIKeyMaxTTL = 365 * 24 * time.Hour

var (
	ErrAPIKeyNotFound      = errors.New("api key not found")
	ErrAPIKeyExpired       = errors.New("api key expired")
	ErrAPIKeyRevoked       = errors.New("api key revoked")
	ErrAPIKeyOwnerInactive = errors.New("api key owner is disabled or missing")
	ErrAPIKeyForbiddenPerm = errors.New("requested permissions exceed your own")
	ErrAPIKeyExpiryTooFar  = errors.New("expiry exceeds the maximum allowed lifetime")
	ErrAPIKeyExpiryPast    = errors.New("expiry must be in the future")
	ErrAPIKeyDuplicateName = errors.New("an api key with this name already exists")
	ErrAPIKeyNameRequired  = errors.New("api key name is required")
)

// APIKeyStore issues, authenticates, lists and revokes user API keys. The raw
// key is APIKeyPrefix + 32 random bytes (base64url); only its SHA-256 hash is
// persisted. Effective permissions are computed live at Resolve time.
type APIKeyStore struct {
	driver   db.Driver
	resolver *RoleResolver
	maxTTL   time.Duration
	now      func() time.Time
}

// NewAPIKeyStore constructs a store with the given expiry cap. A zero or
// negative cap falls back to DefaultAPIKeyMaxTTL.
func NewAPIKeyStore(d db.Driver, maxTTL time.Duration) *APIKeyStore {
	if maxTTL <= 0 {
		maxTTL = DefaultAPIKeyMaxTTL
	}
	return &APIKeyStore{driver: d, resolver: NewRoleResolver(d), maxTTL: maxTTL, now: time.Now}
}

// MaxTTL exposes the configured cap (for handler validation messages).
func (s *APIKeyStore) MaxTTL() time.Duration { return s.maxTTL }

// Issue mints a key owned by owner.Subject/owner.Method carrying the requested
// permission subset. requested must be ⊆ owner.Permissions (ValidateGrant);
// expiresAt is clamped to (now, now+maxTTL] and defaults to now+maxTTL when
// zero. The raw key is returned exactly once; the returned doc omits key_hash.
func (s *APIKeyStore) Issue(ctx context.Context, owner snoozetypes.Claims, name string, requested []string, expiresAt time.Time) (string, db.Document, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, ErrAPIKeyNameRequired
	}
	if bad := ValidateGrant(owner.Permissions, requested); len(bad) > 0 {
		return "", nil, fmt.Errorf("%w: %s", ErrAPIKeyForbiddenPerm, strings.Join(bad, ", "))
	}
	now := s.now().UTC()
	if expiresAt.IsZero() {
		expiresAt = now.Add(s.maxTTL)
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(now) {
		return "", nil, ErrAPIKeyExpiryPast
	}
	if expiresAt.After(now.Add(s.maxTTL)) {
		return "", nil, ErrAPIKeyExpiryTooFar
	}
	raw, err := newAPIKey()
	if err != nil {
		return "", nil, fmt.Errorf("apikey: generate: %w", err)
	}
	doc := db.Document{
		"tenant_id":    owner.TenantID,
		"owner":        owner.Subject,
		"owner_method": owner.Method,
		"name":         name,
		"key_hash":     hashToken(raw),
		"key_prefix":   raw[:12],
		"permissions":  stringsToAny(requested),
		"groups":       stringsToAny(owner.Groups),
		"created_at":   float64(now.Unix()),
		"expires_at":   float64(expiresAt.Unix()),
		"revoked_at":   float64(0),
	}
	res, err := s.driver.Write(ctx, APIKeyCollection, []db.Document{doc}, db.WriteOptions{
		Primary:    []string{"tenant_id", "owner", "name"},
		UpdateTime: true,
		// reject so a duplicate (tenant,owner,name) is a 409, not a silent merge.
		DuplicatePolicy: "reject",
	})
	if err != nil {
		return "", nil, fmt.Errorf("apikey: persist: %w", err)
	}
	if len(res.Rejected) > 0 {
		return "", nil, ErrAPIKeyDuplicateName
	}
	out := sanitize(doc)
	if len(res.Added) > 0 {
		out["uid"] = res.Added[0]
	}
	return raw, out, nil
}

// Resolve authenticates a raw key and returns the live claims it grants.
func (s *APIKeyStore) Resolve(ctx context.Context, raw string) (snoozetypes.Claims, error) {
	if !strings.HasPrefix(raw, APIKeyPrefix) {
		return snoozetypes.Claims{}, ErrAPIKeyNotFound
	}
	// The key arrives pre-auth (naked context). The hash is globally unique, so
	// a platform-scoped lookup is safe; we then operate strictly under the
	// stored row's tenant (mirrors RefreshTokenStore.VerifyAndRotate).
	pctx := WithPlatformScope(ctx)
	doc, err := s.driver.GetOne(pctx, APIKeyCollection, db.Document{"key_hash": hashToken(raw)})
	if err != nil || doc == nil {
		return snoozetypes.Claims{}, ErrAPIKeyNotFound
	}
	if asUnix(doc["revoked_at"]) > 0 {
		return snoozetypes.Claims{}, ErrAPIKeyRevoked
	}
	if exp := asUnix(doc["expires_at"]); exp > 0 && s.now().UTC().Unix() >= exp {
		return snoozetypes.Claims{}, ErrAPIKeyExpired
	}
	tenant, _ := doc["tenant_id"].(string)
	owner, _ := doc["owner"].(string)
	method, _ := doc["owner_method"].(string)
	groups := anyToStrings(doc["groups"])
	stored := anyToStrings(doc["permissions"])

	tctx := WithTenant(ctx, tenant)
	user, err := s.driver.GetOne(tctx, LocalCollection, db.Document{"name": owner, "method": method})
	if err != nil || user == nil {
		return snoozetypes.Claims{}, ErrAPIKeyOwnerInactive
	}
	if enabled, ok := user["enabled"].(bool); ok && !enabled {
		return snoozetypes.Claims{}, ErrAPIKeyOwnerInactive
	}
	roles, livePerms, err := s.resolver.Resolve(tctx, Identity{Username: owner, Method: method, TenantID: tenant, Groups: groups})
	if err != nil {
		return snoozetypes.Claims{}, fmt.Errorf("apikey: resolve roles: %w", err)
	}
	return snoozetypes.Claims{
		Subject:     owner,
		Method:      APIKeyMethod,
		TenantID:    tenant,
		Roles:       roles,
		Permissions: IntersectGrant(livePerms, stored),
		Groups:      groups,
	}, nil
}

// ListByOwner returns the caller's keys, newest first, with key_hash stripped.
func (s *APIKeyStore) ListByOwner(ctx context.Context, owner, method string) ([]db.Document, error) {
	cond := condition.And(
		condition.Equals("owner", owner),
		condition.Equals("owner_method", method),
	)
	docs, _, err := s.driver.Search(ctx, APIKeyCollection, cond, db.Page{OrderBy: "created_at", Asc: false})
	if err != nil {
		return nil, err
	}
	out := make([]db.Document, 0, len(docs))
	for _, d := range docs {
		out = append(out, sanitize(d))
	}
	return out, nil
}

// DeleteByID removes a key only if it is owned by (owner, method).
func (s *APIKeyStore) DeleteByID(ctx context.Context, owner, method, uid string) (bool, error) {
	doc, err := s.driver.GetOne(ctx, APIKeyCollection, db.Document{"uid": uid})
	if err != nil || doc == nil {
		return false, nil
	}
	if o, _ := doc["owner"].(string); o != owner {
		return false, nil
	}
	if m, _ := doc["owner_method"].(string); m != method {
		return false, nil
	}
	n, err := s.driver.Delete(ctx, APIKeyCollection, condition.Equals("uid", uid), true)
	return n > 0, err
}

// Cleanup deletes rows whose expires_at has elapsed. Safe to call from a
// housekeeper job (not yet scheduled — see plan's Deferred note).
func (s *APIKeyStore) Cleanup(ctx context.Context) (int, error) {
	cutoff := float64(s.now().UTC().Unix())
	cond := condition.And(
		condition.Cond{Op: condition.OpGt, Field: "expires_at", Value: float64(0)},
		condition.Cond{Op: condition.OpLt, Field: "expires_at", Value: cutoff},
	)
	return s.driver.Delete(WithPlatformScope(ctx), APIKeyCollection, cond, true)
}

func sanitize(doc db.Document) db.Document {
	out := make(db.Document, len(doc))
	for k, v := range doc {
		if k == "key_hash" {
			continue
		}
		out[k] = v
	}
	return out
}

func newAPIKey() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return APIKeyPrefix + base64.RawURLEncoding.EncodeToString(buf[:]), nil
}
