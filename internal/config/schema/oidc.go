package schema

// OIDC holds the configuration of the OpenID Connect authentication backend
// (used for Microsoft 365 / Entra ID, but generic to any OIDC provider).
// Required fields are only enforced when Enabled is true; see validateOIDC in
// internal/config/validate.go. This is file-config only: it carries
// client_secret (infra tier) and is not runtime-editable.
type OIDC struct {
	Enabled      bool     `koanf:"enabled"`
	Issuer       string   `koanf:"issuer"` // e.g. https://login.microsoftonline.com/<tenant>/v2.0
	ClientID     string   `koanf:"client_id"`
	ClientSecret string   `koanf:"client_secret"`
	RedirectURL  string   `koanf:"redirect_url"` // absolute; must be a registered redirect URI on the IdP
	Scopes       []string `koanf:"scopes"`
	Method       string   `koanf:"method"`       // identity method + URL segment + JWT method claim
	DisplayName  string   `koanf:"display_name"` // login button label
	Icon         string   `koanf:"icon"`         // login button icon key
	RolesClaim   string   `koanf:"roles_claim"`  // ID-token claim with app roles -> Identity.Groups
	GroupsClaim  string   `koanf:"groups_claim"` // optional second claim -> Identity.Groups
	// AdminRoleValue is the role/group value that, when present, maps to the
	// Snooze "admin" role. On a fresh DB the seeded admin role's groups[] is
	// populated with this value (turnkey admin->admin). Existing installs add
	// it to the admin role via the Roles UI.
	AdminRoleValue string `koanf:"admin_role_value"`
}

// DefaultOIDC returns the canonical defaults (disabled, Microsoft-flavoured).
func DefaultOIDC() OIDC {
	return OIDC{
		Enabled:        false,
		Scopes:         []string{"openid", "profile", "email"},
		Method:         "microsoft",
		DisplayName:    "Microsoft 365",
		Icon:           "microsoft",
		RolesClaim:     "roles",
		GroupsClaim:    "groups",
		AdminRoleValue: "Admin",
	}
}
