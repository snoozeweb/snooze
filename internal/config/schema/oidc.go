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

	// Provider is the optional preset key. When set, ResolvePreset (in
	// internal/auth) fills Issuer from a built-in table before validation and
	// discovery. Recognized values: "google", "azure", "cognito", "keycloak",
	// "gitlab". An explicitly-set Issuer always wins over the preset.
	Provider string `koanf:"provider"`

	// ProviderParams carries preset-specific template variables consumed by
	// ResolvePreset:
	//   azure:    {"tenant": "<tenant-id>"}
	//   cognito:  {"region": "us-east-1", "pool_id": "us-east-1_abc"}
	//   keycloak: {"url": "https://idp.corp.example", "realm": "snooze"}
	//   gitlab:   {"url": "https://gitlab.com"}  (defaults to gitlab.com)
	ProviderParams map[string]string `koanf:"provider_params"`
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

// DefaultGoogleOIDC returns a disabled OIDC entry pre-filled for Google
// Workspace. The issuer is resolved by ResolvePreset from the "google" preset;
// the operator supplies client_id/client_secret/redirect_url.
func DefaultGoogleOIDC() OIDC {
	return OIDC{
		Provider:    "google",
		Scopes:      []string{"openid", "profile", "email"},
		Method:      "google",
		DisplayName: "Google",
		Icon:        "google",
		GroupsClaim: "groups",
	}
}

// DefaultAzureOIDC returns a disabled OIDC entry pre-filled for Microsoft Entra
// (Azure AD). The issuer is resolved from the "azure" preset, which requires a
// provider_params["tenant"].
func DefaultAzureOIDC() OIDC {
	return OIDC{
		Provider:    "azure",
		Scopes:      []string{"openid", "profile", "email"},
		Method:      "azure",
		DisplayName: "Microsoft Entra",
		Icon:        "microsoft",
		RolesClaim:  "roles",
		GroupsClaim: "groups",
	}
}

// DefaultKeycloakOIDC returns a disabled OIDC entry pre-filled for Keycloak.
// The issuer is resolved from the "keycloak" preset, which requires
// provider_params["url"] and provider_params["realm"].
func DefaultKeycloakOIDC() OIDC {
	return OIDC{
		Provider:    "keycloak",
		Scopes:      []string{"openid", "profile", "email"},
		Method:      "keycloak",
		DisplayName: "Keycloak",
		Icon:        "keycloak",
		RolesClaim:  "roles",
		GroupsClaim: "groups",
	}
}

// DefaultGitLabOIDC returns a disabled OIDC entry pre-filled for GitLab. The
// issuer is resolved from the "gitlab" preset, which defaults to
// https://gitlab.com unless provider_params["url"] is set.
func DefaultGitLabOIDC() OIDC {
	return OIDC{
		Provider:    "gitlab",
		Scopes:      []string{"openid", "profile", "email"},
		Method:      "gitlab",
		DisplayName: "GitLab",
		Icon:        "gitlab",
		GroupsClaim: "groups",
	}
}

// DefaultCognitoOIDC returns a disabled OIDC entry pre-filled for AWS Cognito.
// The issuer is resolved from the "cognito" preset, which requires
// provider_params["region"] and provider_params["pool_id"].
func DefaultCognitoOIDC() OIDC {
	return OIDC{
		Provider:    "cognito",
		Scopes:      []string{"openid", "profile", "email"},
		Method:      "cognito",
		DisplayName: "AWS Cognito",
		Icon:        "aws",
		GroupsClaim: "groups",
	}
}
