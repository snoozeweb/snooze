package auth

import "strings"

// Constants shared by the APIKeyStore, the apikey plugin, and the auth
// middleware. APIKeyCollection MUST equal the apikey plugin's Name() so the
// implicit ro_apikey/rw_apikey gates line up with the collection.
const (
	APIKeyCollection = "apikey"
	APIKeyPrefix     = "snz_"
	// APIKeyMethod is stamped as Claims.Method for key-authenticated requests.
	// It is deliberately distinct from any login provider so sensitive
	// self-service routes (password change, key minting) can refuse keys by
	// checking Method, and so audit logs show the request came via a key.
	APIKeyMethod = "apikey"
)

// permits reports whether a permission set grants want, honoring the rw_all
// wildcard and the rw_X ⇒ ro_X implication used across the authorizer.
func permits(have map[string]struct{}, want string) bool {
	if _, ok := have[AllPermission]; ok {
		return true
	}
	if _, ok := have[want]; ok {
		return true
	}
	if rest, ok := strings.CutPrefix(want, "ro_"); ok {
		if _, ok := have["rw_"+rest]; ok {
			return true
		}
	}
	return false
}

func permSet(perms []string) map[string]struct{} {
	s := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		if p != "" {
			s[p] = struct{}{}
		}
	}
	return s
}

// ValidateGrant returns the subset of requested permissions the caller may NOT
// grant to a key: reserved platform permissions (never grantable) and any
// permission the caller does not itself hold. An empty result means every
// requested permission is allowed.
func ValidateGrant(caller, requested []string) []string {
	have := permSet(caller)
	var bad []string
	for _, w := range requested {
		if IsReservedPlatformPerm(w) || !permits(have, w) {
			bad = append(bad, w)
		}
	}
	return bad
}

// IntersectGrant filters granted down to the permissions the owner currently
// holds, dropping reserved platform permissions unconditionally. It is the
// live bound applied at every key authentication: a key can never exceed what
// its owner holds right now.
func IntersectGrant(owner, granted []string) []string {
	have := permSet(owner)
	out := make([]string, 0, len(granted))
	for _, g := range granted {
		if IsReservedPlatformPerm(g) {
			continue
		}
		if permits(have, g) {
			out = append(out, g)
		}
	}
	return out
}
