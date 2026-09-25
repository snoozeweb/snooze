package auth

import (
	"reflect"
	"sort"
	"testing"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestAPIKeyPerms_ValidateGrant(t *testing.T) {
	cases := []struct {
		name    string
		caller  []string
		want    []string
		wantBad []string
	}{
		{"literal ok", []string{"rw_record", "ro_rule"}, []string{"rw_record", "ro_rule"}, nil},
		{"wildcard grants anything", []string{"rw_all"}, []string{"rw_record", "ro_rule"}, nil},
		{"rw implies ro", []string{"rw_record"}, []string{"ro_record"}, nil},
		{"ro does not imply rw", []string{"ro_record"}, []string{"rw_record"}, []string{"rw_record"}},
		{"missing perm rejected", []string{"ro_rule"}, []string{"rw_record"}, []string{"rw_record"}},
		{"reserved rejected even with wildcard", []string{"rw_all"}, []string{"rw_tenant", "ro_tenant"}, []string{"rw_tenant", "ro_tenant"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ValidateGrant(c.caller, c.want)
			sort.Strings(got)
			sort.Strings(c.wantBad)
			if len(got) == 0 && len(c.wantBad) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.wantBad) {
				t.Fatalf("ValidateGrant() = %v, want %v", got, c.wantBad)
			}
		})
	}
}

func TestAPIKeyPerms_IntersectGrant(t *testing.T) {
	// Owner lost rw_record (now only has ro_rule); a key granted both shrinks.
	got := IntersectGrant([]string{"ro_rule"}, []string{"rw_record", "ro_rule"})
	if !reflect.DeepEqual(got, []string{"ro_rule"}) {
		t.Fatalf("IntersectGrant() = %v, want [ro_rule]", got)
	}
	// rw_all owner keeps everything non-reserved.
	got = IntersectGrant([]string{"rw_all"}, []string{"rw_record", "ro_rule"})
	if !reflect.DeepEqual(got, []string{"rw_record", "ro_rule"}) {
		t.Fatalf("IntersectGrant(rw_all) = %v", got)
	}
	// reserved perms never survive even if somehow stored.
	got = IntersectGrant([]string{"rw_all"}, []string{"rw_tenant"})
	if len(got) != 0 {
		t.Fatalf("IntersectGrant kept reserved perm: %v", got)
	}
}

func TestIdentityMethod(t *testing.T) {
	cases := []struct {
		name   string
		claims snoozetypes.Claims
		want   string
	}{
		{"session", snoozetypes.Claims{Method: "ldap"}, "ldap"},
		{"api key", snoozetypes.Claims{Method: APIKeyMethod, OwnerMethod: "local"}, "local"},
		// A key row without owner_method keeps the channel rather than inventing one.
		{"api key without owner method", snoozetypes.Claims{Method: APIKeyMethod}, APIKeyMethod},
		// OwnerMethod on a session claim set is ignored.
		{"session with stray owner method", snoozetypes.Claims{Method: "oidc", OwnerMethod: "local"}, "oidc"},
	}
	for _, c := range cases {
		if got := IdentityMethod(c.claims); got != c.want {
			t.Errorf("%s: IdentityMethod = %q, want %q", c.name, got, c.want)
		}
	}
}
