package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// RefreshCollection is the DB collection that backs RefreshTokenStore. The
// driver-specific table/collection is created lazily on first Write.
const RefreshCollection = "refresh_token"

// DefaultRefreshLease is the lease applied when the configured value is zero.
const DefaultRefreshLease = 7 * 24 * time.Hour

// Sentinel errors. Callers identify outcomes via errors.Is.
var (
	ErrRefreshNotFound = errors.New("refresh token not found")
	ErrRefreshExpired  = errors.New("refresh token expired")
	ErrRefreshRevoked  = errors.New("refresh token revoked")
)

// RefreshTokenStore issues, verifies, rotates and revokes long-lived refresh
// tokens. The raw token is a 32-byte random value, base64-url encoded; only
// its SHA-256 hash is persisted, so a DB compromise does not yield usable
// tokens.
type RefreshTokenStore struct {
	driver db.Driver
	lease  time.Duration
	now    func() time.Time
}

// NewRefreshTokenStore constructs a store with the given lease. A zero or
// negative lease falls back to DefaultRefreshLease.
func NewRefreshTokenStore(d db.Driver, lease time.Duration) *RefreshTokenStore {
	if lease <= 0 {
		lease = DefaultRefreshLease
	}
	return &RefreshTokenStore{driver: d, lease: lease, now: time.Now}
}

// Lease returns the configured refresh-token lease.
func (s *RefreshTokenStore) Lease() time.Duration { return s.lease }

// Issue mints a fresh refresh token tied to c. The raw token is returned
// alongside its absolute expiry time.
func (s *RefreshTokenStore) Issue(ctx context.Context, c snoozetypes.Claims) (string, time.Time, error) {
	raw, err := newRefreshToken()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("refresh: generate: %w", err)
	}
	now := s.now().UTC()
	exp := now.Add(s.lease)
	doc := db.Document{
		"token_hash":  hashToken(raw),
		"subject":     c.Subject,
		"method":      c.Method,
		"roles":       stringsToAny(c.Roles),
		"permissions": stringsToAny(c.Permissions),
		"groups":      stringsToAny(c.Groups),
		"issued_at":   float64(now.Unix()),
		"expires_at":  float64(exp.Unix()),
		"revoked_at":  float64(0),
	}
	if _, err := s.driver.Write(ctx, RefreshCollection, []db.Document{doc}, db.WriteOptions{
		Primary:    []string{"token_hash"},
		UpdateTime: true,
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("refresh: persist: %w", err)
	}
	return raw, exp, nil
}

// VerifyAndRotate validates raw, revokes it, and mints a fresh refresh token
// carrying the same claims. The returned claims are reconstructed from the
// stored record — they do not include Issuer / Audience / NotBefore (those
// belong to the access-token JWT, not the refresh record).
func (s *RefreshTokenStore) VerifyAndRotate(ctx context.Context, raw string) (snoozetypes.Claims, string, time.Time, error) {
	doc, err := s.lookup(ctx, raw)
	if err != nil {
		return snoozetypes.Claims{}, "", time.Time{}, err
	}
	if err := s.checkActive(doc); err != nil {
		return snoozetypes.Claims{}, "", time.Time{}, err
	}
	claims := claimsFromDoc(doc)
	if err := s.markRevoked(ctx, raw); err != nil {
		return snoozetypes.Claims{}, "", time.Time{}, fmt.Errorf("refresh: revoke old: %w", err)
	}
	newRaw, exp, err := s.Issue(ctx, claims)
	if err != nil {
		return snoozetypes.Claims{}, "", time.Time{}, err
	}
	return claims, newRaw, exp, nil
}

// Revoke marks raw as revoked. Unknown or already-revoked tokens are not
// treated as errors so logout against a stale token never returns 500.
func (s *RefreshTokenStore) Revoke(ctx context.Context, raw string) error {
	_, err := s.lookup(ctx, raw)
	if err != nil {
		if errors.Is(err, ErrRefreshNotFound) {
			return nil
		}
		return err
	}
	return s.markRevoked(ctx, raw)
}

// --- internals --------------------------------------------------------------

func (s *RefreshTokenStore) lookup(ctx context.Context, raw string) (db.Document, error) {
	if raw == "" {
		return nil, ErrRefreshNotFound
	}
	hash := hashToken(raw)
	doc, err := s.driver.GetOne(ctx, RefreshCollection, db.Document{"token_hash": hash})
	if err != nil || doc == nil {
		return nil, ErrRefreshNotFound
	}
	return doc, nil
}

func (s *RefreshTokenStore) checkActive(doc db.Document) error {
	if asUnix(doc["revoked_at"]) > 0 {
		return ErrRefreshRevoked
	}
	if exp := asUnix(doc["expires_at"]); exp > 0 && s.now().UTC().Unix() >= exp {
		return ErrRefreshExpired
	}
	return nil
}

func (s *RefreshTokenStore) markRevoked(ctx context.Context, raw string) error {
	hash := hashToken(raw)
	patch := db.Document{
		"token_hash": hash,
		"revoked_at": float64(s.now().UTC().Unix()),
	}
	_, err := s.driver.Write(ctx, RefreshCollection, []db.Document{patch}, db.WriteOptions{
		Primary:    []string{"token_hash"},
		UpdateTime: true,
	})
	return err
}

// Cleanup removes refresh-token rows whose expires_at has elapsed. Returns
// the number of deleted rows. Safe to call from the housekeeper.
func (s *RefreshTokenStore) Cleanup(ctx context.Context) (int, error) {
	cutoff := s.now().UTC().Unix()
	cond := condition.Cond{Op: condition.OpLt, Field: "expires_at", Value: float64(cutoff)}
	return s.driver.Delete(ctx, RefreshCollection, cond, true)
}

func newRefreshToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func asUnix(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case nil:
		return 0
	default:
		return 0
	}
}

func stringsToAny(in []string) []any {
	if len(in) == 0 {
		return nil
	}
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func anyToStrings(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func claimsFromDoc(doc db.Document) snoozetypes.Claims {
	sub, _ := doc["subject"].(string)
	method, _ := doc["method"].(string)
	return snoozetypes.Claims{
		Subject:     sub,
		Method:      method,
		Roles:       anyToStrings(doc["roles"]),
		Permissions: anyToStrings(doc["permissions"]),
		Groups:      anyToStrings(doc["groups"]),
	}
}
