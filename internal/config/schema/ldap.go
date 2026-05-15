package schema

// LDAP holds the configuration of the LDAP authentication backend. Required
// fields are only enforced when “Enabled“ is true; see the custom validator
// in “internal/config/validate.go“.
type LDAP struct {
	Enabled              bool   `koanf:"enabled"`
	BaseDN               string `koanf:"base_dn"`
	UserFilter           string `koanf:"user_filter"`
	BindDN               string `koanf:"bind_dn"`
	BindPassword         string `koanf:"bind_password"`
	Host                 string `koanf:"host"`
	Port                 int    `koanf:"port" validate:"min=1,max=65535"`
	GroupDN              string `koanf:"group_dn"`
	EmailAttribute       string `koanf:"email_attribute"`
	DisplayNameAttribute string `koanf:"display_name_attribute"`
	MemberAttribute      string `koanf:"member_attribute"`
}

// DefaultLDAP returns the Python defaults.
func DefaultLDAP() LDAP {
	return LDAP{
		Enabled:              false,
		Port:                 636,
		EmailAttribute:       "mail",
		DisplayNameAttribute: "cn",
		MemberAttribute:      "memberof",
	}
}
