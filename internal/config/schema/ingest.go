package schema

// Ingest holds optional, opt-in hardening for the inbound webhook receivers
// mounted under /api/v1/webhook/*. Every field defaults to "off" so existing
// deployments keep working with no ingest authentication (1.5.0 parity);
// operators opt in according to their threat model. Network isolation (a
// reverse proxy / restricted monitoring network) remains the recommended
// baseline — these knobs are defense-in-depth.
type Ingest struct {
	// Token, when non-empty, requires every inbound webhook request to present
	// it as `Authorization: Bearer <token>` or `?token=<token>`. Applies to
	// all webhook receivers uniformly.
	Token string `koanf:"token"`

	// SNSVerify enables AWS SNS message-signature verification on the
	// cloudwatch receiver (validates the SigningCertURL chain + signature).
	SNSVerify bool `koanf:"sns_verify"`

	// SentrySecret, when non-empty, enables HMAC-SHA256 verification of the
	// `sentry-hook-signature` header on the sentry receiver against the
	// configured Sentry client secret.
	SentrySecret string `koanf:"sentry_secret"`
}

// DefaultIngest returns the off-by-default ingest-hardening config.
func DefaultIngest() Ingest { return Ingest{} }
