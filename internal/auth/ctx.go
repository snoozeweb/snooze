package auth

import (
	"context"

	"github.com/japannext/snooze/pkg/snoozetypes"
)

// ctxKey is the unexported key type for claims storage; the unexported nature
// prevents accidental collisions with other packages keying off context.Value.
type ctxKey struct{}

// WithClaims returns a derived context carrying c. The HTTP auth middleware
// is the canonical caller.
func WithClaims(ctx context.Context, c snoozetypes.Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// ClaimsFrom returns the claims previously attached by WithClaims. The bool
// is false when no claims are present (anonymous or pre-auth contexts).
func ClaimsFrom(ctx context.Context) (snoozetypes.Claims, bool) {
	v, ok := ctx.Value(ctxKey{}).(snoozetypes.Claims)
	return v, ok
}
