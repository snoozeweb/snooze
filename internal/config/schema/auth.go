package schema

import "time"

// Auth holds the static authentication knobs that the Python codebase scattered
// across “CoreConfig“ and runtime defaults. The token signing key is the only
// strictly mandatory value.
type Auth struct {
	TokenSecret       string   `koanf:"token_secret"`
	TokenAlgorithm    string   `koanf:"token_algorithm" validate:"omitempty,oneof=HS256 HS384 HS512"`
	TokenLease        Duration `koanf:"token_lease"`
	RefreshTokenLease Duration `koanf:"refresh_token_lease"`
	TokenIssuer       string   `koanf:"token_issuer"`
	TokenAudience     string   `koanf:"token_audience"`
}

// DefaultAuth returns the canonical defaults: HS256, 1h access lease, 7d refresh lease.
func DefaultAuth() Auth {
	return Auth{
		TokenSecret:       "",
		TokenAlgorithm:    "HS256",
		TokenLease:        Duration(time.Hour),
		RefreshTokenLease: Duration(7 * 24 * time.Hour),
		TokenIssuer:       "snooze",
		TokenAudience:     "snooze",
	}
}
