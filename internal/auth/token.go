package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// MinSecretBytes is the minimum length of an HS256 signing key, matching the
// SHA-256 block size. NewTokenEngine rejects anything shorter.
const MinSecretBytes = 32

// DefaultLease is the lease applied when the configured value is zero.
const DefaultLease = time.Hour

// Errors surfaced by the token engine. They wrap the upstream jwt errors so
// callers can use errors.Is.
var (
	ErrTokenExpired   = errors.New("token expired")
	ErrTokenInvalid   = errors.New("token invalid")
	ErrTokenSignature = errors.New("token signature mismatch")
)

// TokenEngine mints and verifies HS256 JWTs carrying snoozetypes.Claims.
type TokenEngine struct {
	secret []byte
	iss    string
	aud    []string
	lease  time.Duration
}

// NewTokenEngine validates the secret length and constructs an engine using
// the issuer/audience/lease values pulled from cfg. The configured audience
// is treated as a comma-separated list (matching the OAuth/OIDC convention).
func NewTokenEngine(secret []byte, cfg schema.Auth) (*TokenEngine, error) {
	if len(secret) < MinSecretBytes {
		return nil, fmt.Errorf("token secret must be at least %d bytes (got %d)", MinSecretBytes, len(secret))
	}
	if cfg.TokenAlgorithm != "" && cfg.TokenAlgorithm != "HS256" {
		return nil, fmt.Errorf("token engine: unsupported algorithm %q (only HS256)", cfg.TokenAlgorithm)
	}
	lease := time.Duration(cfg.TokenLease)
	if lease <= 0 {
		lease = DefaultLease
	}
	iss := cfg.TokenIssuer
	if iss == "" {
		iss = "snooze"
	}
	aud := splitAudience(cfg.TokenAudience)
	cp := make([]byte, len(secret))
	copy(cp, secret)
	return &TokenEngine{secret: cp, iss: iss, aud: aud, lease: lease}, nil
}

// splitAudience parses the comma-separated audience config into a slice. An
// empty string yields a single-element slice with "snooze" to match Python.
func splitAudience(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{"snooze"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"snooze"}
	}
	return out
}

// Lease returns the configured token lease.
func (e *TokenEngine) Lease() time.Duration { return e.lease }

// DeriveKey returns a 32-byte key derived from the signing secret and a label
// via HMAC-SHA256. It lets other subsystems (e.g. the OIDC state-cookie codec)
// authenticate data with a key that is stable across a deploy and shared across
// cluster nodes — without exposing the raw signing secret.
func (e *TokenEngine) DeriveKey(label string) []byte {
	h := hmac.New(sha256.New, e.secret)
	h.Write([]byte(label))
	return h.Sum(nil)
}

// jwtClaims is the in-flight claim payload. We translate to/from
// snoozetypes.Claims at the engine boundary.
type jwtClaims struct {
	Method      string   `json:"method"`
	TenantID    string   `json:"tenant_id,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	jwt.RegisteredClaims
}

// Sign mints a fresh JWT for c. The Subject, Method and friends are taken
// from c verbatim; Issuer, Audience, IssuedAt, NotBefore, ExpiresAt and a
// random JTI are injected by the engine. The exp wall-clock time is returned
// alongside the token for the API layer's response envelope.
func (e *TokenEngine) Sign(c snoozetypes.Claims) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(e.lease)

	jti, err := randomID()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign: jti: %w", err)
	}

	claims := jwtClaims{
		Method:      c.Method,
		TenantID:    c.TenantID,
		Roles:       c.Roles,
		Permissions: c.Permissions,
		Groups:      c.Groups,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.Subject,
			Issuer:    e.iss,
			Audience:  jwt.ClaimStrings(e.aud),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        jti,
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(e.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign: %w", err)
	}
	return signed, exp, nil
}

// Verify parses, signature-checks and time-checks token. It returns the
// snoozetypes.Claims payload on success. Errors are mapped to the package
// sentinels (ErrTokenExpired / ErrTokenInvalid / ErrTokenSignature).
func (e *TokenEngine) Verify(token string) (snoozetypes.Claims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(e.iss),
		jwt.WithExpirationRequired(),
	)
	var c jwtClaims
	parsed, err := parser.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		// Defence in depth: the WithValidMethods option also pins this.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: unexpected alg %v", ErrTokenSignature, t.Header["alg"])
		}
		return e.secret, nil
	})
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return snoozetypes.Claims{}, fmt.Errorf("%w: %v", ErrTokenExpired, err)
		case errors.Is(err, jwt.ErrTokenSignatureInvalid):
			return snoozetypes.Claims{}, fmt.Errorf("%w: %v", ErrTokenSignature, err)
		default:
			return snoozetypes.Claims{}, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
		}
	}
	if !parsed.Valid {
		return snoozetypes.Claims{}, ErrTokenInvalid
	}
	if !audienceMatches(c.Audience, e.aud) {
		return snoozetypes.Claims{}, fmt.Errorf("%w: audience mismatch", ErrTokenInvalid)
	}

	out := snoozetypes.Claims{
		Subject:     c.Subject,
		Method:      c.Method,
		TenantID:    c.TenantID,
		Roles:       c.Roles,
		Permissions: c.Permissions,
		Groups:      c.Groups,
		Issuer:      c.Issuer,
		Audience:    []string(c.Audience),
		ID:          c.ID,
	}
	if c.ExpiresAt != nil {
		out.ExpiresAt = c.ExpiresAt.Unix()
	}
	if c.NotBefore != nil {
		out.NotBefore = c.NotBefore.Unix()
	}
	if c.IssuedAt != nil {
		out.IssuedAt = c.IssuedAt.Unix()
	}
	return out, nil
}

// audienceMatches returns true when at least one configured audience is
// present in the token's audience list. Constant-time comparison guards
// against side-channel leaks even though the audience values are not secret.
func audienceMatches(got, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		wb := []byte(w)
		for _, g := range got {
			gb := []byte(g)
			if len(wb) == len(gb) && subtle.ConstantTimeCompare(wb, gb) == 1 {
				return true
			}
		}
	}
	return false
}

// RootClaims returns the canonical claim set carried by the unix-socket
// /api/root_token bootstrap token. It grants rw_all and identifies the
// principal as method="root" so audit logs can distinguish it from a regular
// login.
//
// The token is scoped to the default tenant and carries the LITERAL platform
// permissions (ro_tenant + rw_tenant) so the privileged unix-socket operator can
// actually manage the tenant registry. RequirePlatformPerm is strict on two axes
// — default-tenant origin AND a literal platform permission (rw_all is NOT
// honored) — so without these the socket-minted root token would be locked out
// of /api/v1/tenant despite being the most privileged principal in the system.
func (e *TokenEngine) RootClaims() snoozetypes.Claims {
	return snoozetypes.Claims{
		Subject:     "root",
		Method:      "root",
		TenantID:    snoozetypes.DefaultTenant,
		Roles:       []string{"admin", PlatformAdminRole},
		Permissions: []string{AllPermission, PermReadTenant, PermWriteTenant},
	}
}

// randomID returns a 128-bit hex-encoded random identifier suitable for a JTI.
func randomID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
