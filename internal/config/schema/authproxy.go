package schema

// AuthProxy configures the trusted-header authentication mode: trust an
// upstream reverse proxy (oauth2-proxy, Pomerium, Apache mod_auth_openidc, …)
// that has already authenticated the user and forwards the identity in HTTP
// headers. When Enabled, the auth middleware reads a username header and an
// optional groups header, JIT-provisions a method="proxy" user, resolves roles
// via the existing RBAC resolver, and lets the request through with no
// Snooze-issued JWT or snz_ API key.
//
// SECURITY: trusting an identity header with no upstream proxy is a complete
// authentication bypass — any client could send X-Forwarded-User: root. The
// mode is therefore disabled by default and gated by an IP allowlist
// (TrustedProxies). Only enable it behind a proxy that STRIPS any
// client-supplied copies of these headers. See validateAuthProxy in
// internal/config/validate.go for the enabled-mode requirements. This is
// file-config only (infra tier) and is not runtime-editable.
type AuthProxy struct {
	Enabled bool `koanf:"enabled"`
	// UserHeader is the request header carrying the authenticated username,
	// e.g. "X-Forwarded-User". Required when Enabled.
	UserHeader string `koanf:"user_header"`
	// GroupsHeader is the request header carrying the user's groups, e.g.
	// "X-Forwarded-Groups". Optional; an empty header yields no groups.
	GroupsHeader string `koanf:"groups_header"`
	// GroupsSep separates the values inside GroupsHeader. Default ",".
	GroupsSep string `koanf:"groups_separator"`
	// AutoSignup controls JIT provisioning. When true, an unknown proxy user is
	// created on first sight; when false, an unknown user is rejected with 403.
	AutoSignup bool `koanf:"auto_signup"`
	// TrustedProxies is the IP/CIDR allowlist of the proxy's own address as
	// Snooze sees it. The proxy branch only trusts the identity headers when the
	// request's client IP falls within one of these entries. Empty => allow any
	// (logged as a loud boot WARN — a deliberate fail-open operator choice).
	TrustedProxies []string `koanf:"trusted_proxies"`
	// Method is the identity method tag stamped on the JIT user document and the
	// Claims.Method. Default "proxy".
	Method string `koanf:"method"`
}

// DefaultAuthProxy returns the canonical defaults: disabled, oauth2-proxy-style
// header names, comma-separated groups, JIT auto-signup on, method "proxy".
func DefaultAuthProxy() AuthProxy {
	return AuthProxy{
		Enabled:      false,
		UserHeader:   "X-Forwarded-User",
		GroupsHeader: "X-Forwarded-Groups",
		GroupsSep:    ",",
		AutoSignup:   true,
		Method:       "proxy",
	}
}
