package schema

import "testing"

// TestDefaultSAML pins the canonical SAML defaults: disabled out of the box,
// assertions required-signed (the secure default), groups read from the
// "groups" attribute, and the method/labels matching the OIDC convention.
func TestDefaultSAML(t *testing.T) {
	d := DefaultSAML()
	if d.Enabled {
		t.Errorf("DefaultSAML().Enabled = true, want false (disabled by default)")
	}
	if !d.WantAssertionsSigned {
		t.Errorf("DefaultSAML().WantAssertionsSigned = false, want true")
	}
	if d.GroupsAttribute != "groups" {
		t.Errorf("DefaultSAML().GroupsAttribute = %q, want %q", d.GroupsAttribute, "groups")
	}
	if d.Method != "saml" {
		t.Errorf("DefaultSAML().Method = %q, want %q", d.Method, "saml")
	}
	if d.DisplayName != "SAML" {
		t.Errorf("DefaultSAML().DisplayName = %q, want %q", d.DisplayName, "SAML")
	}
	if d.Icon != "saml" {
		t.Errorf("DefaultSAML().Icon = %q, want %q", d.Icon, "saml")
	}
}
