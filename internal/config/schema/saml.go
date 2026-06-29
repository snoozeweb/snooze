package schema

// SAML holds the configuration of the SAML2 SP-initiated authentication
// backend (ADFS, Shibboleth, Okta, PingFederate, …). Required fields are only
// enforced when Enabled is true; see validateSAML in
// internal/config/validate.go. This is file-config only: it carries an SP
// private-key path + cert (infra tier) and the IdP-registered ACS URL, which
// must not be live-editable without a metadata re-exchange.
type SAML struct {
	Enabled bool `koanf:"enabled"`
	// IDPMetadataURL is a remote URL serving the IdP EntityDescriptor; fetched
	// once at first use. Exactly one of IDPMetadataURL / IDPMetadataXML is needed.
	IDPMetadataURL string `koanf:"idp_metadata_url"`
	// IDPMetadataXML is an inline alternative to IDPMetadataURL: either a path to
	// a local metadata file or the literal EntityDescriptor XML.
	IDPMetadataXML string `koanf:"idp_metadata_xml"`
	// EntityID is the SP entityID advertised in the metadata; defaults to the
	// metadata URL when empty.
	EntityID string `koanf:"entity_id"`
	// ACSURL is the absolute Assertion Consumer Service URL registered on the IdP
	// (e.g. https://snooze.example.com/api/v1/login/saml/acs).
	ACSURL string `koanf:"acs_url"`
	// SPCertFile / SPKeyFile are the SP signing/decryption PEM cert + private key.
	// Only required when SignRequests is true or the IdP encrypts assertions.
	SPCertFile string `koanf:"sp_cert_file"`
	SPKeyFile  string `koanf:"sp_key_file"`
	// SignRequests controls authn_requests_signed: when true the SP signs its
	// AuthnRequests (needs SPKeyFile/SPCertFile).
	SignRequests bool `koanf:"sign_requests"`
	// WantAssertionsSigned advertises want_assertions_signed in the SP metadata;
	// defaults true. The crewjam library always requires a signed assertion.
	WantAssertionsSigned bool `koanf:"want_assertions_signed"`
	// AllowUnsolicited tolerates IdP-initiated (unsolicited) responses. Default
	// false: the ACS requires a matching RelayState cookie, rejecting a stray
	// IdP-initiated POST.
	AllowUnsolicited bool `koanf:"allow_unsolicited"`
	// GroupsAttribute is the assertion attribute carrying group memberships
	// (default "groups"); its values feed Identity.Groups.
	GroupsAttribute string `koanf:"groups_attribute"`
	// RolesAttribute is an optional second attribute whose values are unioned
	// into Identity.Groups (parity with OIDC's roles_claim).
	RolesAttribute string `koanf:"roles_attribute"`
	// Method is the identity method + login URL segment + JWT method claim
	// (default "saml"). Not runtime-editable — changing it orphans provisioned
	// users.
	Method string `koanf:"method"`
	// DisplayName is the login button label; Icon is the login button icon key.
	DisplayName string `koanf:"display_name"`
	Icon        string `koanf:"icon"`
	// AdminRoleValue is the group/role attribute value that maps to the Snooze
	// "admin" role (parity with OIDC). On a fresh DB the seeded admin role's
	// groups[] is populated with this value.
	AdminRoleValue string `koanf:"admin_role_value"`
}

// DefaultSAML returns the canonical defaults (disabled, assertions required to
// be signed, groups read from the "groups" attribute).
func DefaultSAML() SAML {
	return SAML{
		Enabled:              false,
		WantAssertionsSigned: true,
		GroupsAttribute:      "groups",
		Method:               "saml",
		DisplayName:          "SAML",
		Icon:                 "saml",
		AdminRoleValue:       "Admin",
	}
}
