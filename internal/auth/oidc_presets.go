package auth

import (
	"fmt"
	"strings"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// ResolvePreset fills o.Issuer from o.Provider's built-in preset when Provider
// is set and Issuer has not been set explicitly by the operator. It mirrors
// Alerta's OIDC_ISSUER_URL_BY_PROVIDER: an explicit Issuer always wins, an
// empty Provider is a no-op, and an unrecognised Provider (e.g. "github", which
// has no id_token and is out of OIDC scope) is an error.
//
// Provider-specific template variables come from o.ProviderParams:
//
//	azure:    {"tenant": "<tenant-id>"}                       (tenant required)
//	cognito:  {"region": "us-east-1", "pool_id": "..._abc"}   (both required)
//	keycloak: {"url": "https://idp", "realm": "snooze"}       (both required)
//	gitlab:   {"url": "https://gitlab.com"}                   (defaults gitlab.com)
//	google:   (no params)
//
// It is called at config load time (validateOIDCEntries, for early error
// messages) and again at runtime inside OIDCProvider.ensureInit (on a copy of
// the live config) so a provider_params edit takes effect on the next sign-in.
func ResolvePreset(o *schema.OIDC) error {
	provider := strings.ToLower(strings.TrimSpace(o.Provider))
	if provider == "" {
		return nil
	}
	issuer, err := presetIssuer(provider, o.ProviderParams)
	if err != nil {
		return err
	}
	// An explicit issuer set by the operator always wins; the preset is advisory.
	if strings.TrimSpace(o.Issuer) == "" {
		o.Issuer = issuer
	}
	return nil
}

// presetIssuer computes the issuer URL for a recognised provider preset from
// its template params. It returns an error for an unknown provider or a missing
// required param.
func presetIssuer(provider string, params map[string]string) (string, error) {
	switch provider {
	case "google":
		return "https://accounts.google.com", nil
	case "azure":
		tenant := params["tenant"]
		if strings.TrimSpace(tenant) == "" {
			return "", fmt.Errorf("provider %q requires provider_params.tenant", provider)
		}
		return fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", tenant), nil
	case "cognito":
		region := params["region"]
		poolID := params["pool_id"]
		if strings.TrimSpace(region) == "" || strings.TrimSpace(poolID) == "" {
			return "", fmt.Errorf("provider %q requires provider_params.region and provider_params.pool_id", provider)
		}
		return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, poolID), nil
	case "keycloak":
		url := strings.TrimRight(params["url"], "/")
		realm := params["realm"]
		if strings.TrimSpace(url) == "" || strings.TrimSpace(realm) == "" {
			return "", fmt.Errorf("provider %q requires provider_params.url and provider_params.realm", provider)
		}
		return fmt.Sprintf("%s/realms/%s", url, realm), nil
	case "gitlab":
		url := strings.TrimRight(strings.TrimSpace(params["url"]), "/")
		if url == "" {
			url = "https://gitlab.com"
		}
		return url, nil
	default:
		return "", fmt.Errorf("unknown OIDC provider preset %q (recognized: google, azure, cognito, keycloak, gitlab)", provider)
	}
}
