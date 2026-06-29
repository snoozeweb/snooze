package auth

import (
	"errors"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TenantMatchCollection is the global (NOT tenant-scoped) collection holding
// the attribute→tenant routing rules. It mirrors the tenantmatch plugin's
// Collection constant and lets the server wire-up reference the name without
// importing the concrete plugin package.
const TenantMatchCollection = "tenant_match"

// ErrNoTenantMatch is returned by TenantMatchResolver.Resolve when fail_closed
// is enabled and no rule matched the authenticated identity. The login handlers
// map it to a 403 ("no organization matched your account"). With fail_closed
// disabled (the default) an unmatched identity falls through to DefaultTenant
// and this error is never produced.
var ErrNoTenantMatch = errors.New("no tenant match rule found for this identity")

// matchRule is one attribute→tenant routing entry, projected from a
// `tenant_match` document. MatchType is "group", "domain", or "login"; Match is
// the literal value compared (case-insensitively); TenantID is the target
// tenant slug; Priority orders evaluation (lower first, ties broken by UID).
type matchRule struct {
	MatchType string
	Match     string
	TenantID  string
	Priority  int
	UID       string
}

// TenantMatchResolver holds an in-memory snapshot of the tenant_match registry
// and resolves an authenticated Identity to a tenant slug. The snapshot is held
// in a lock-free atomic.Value (mirroring middleware.TenantResolver) so the
// read-mostly Resolve path never blocks a Load. The zero value is usable: an
// un-Loaded resolver behaves as an empty, open (fail_closed=false) registry.
//
// Lookup is O(N) over the rule count. The registry is expected to stay small
// (≤500 rules); a map-per-match-type index can be added if that ever degrades.
type TenantMatchResolver struct {
	rules  atomic.Value // []matchRule, pre-sorted by (priority ASC, uid ASC)
	closed atomic.Bool  // fail_closed flag
}

// NewTenantMatchResolver returns an empty resolver ready for Load.
func NewTenantMatchResolver() *TenantMatchResolver {
	r := &TenantMatchResolver{}
	r.rules.Store([]matchRule(nil))
	return r
}

// Load atomically replaces the rule snapshot and the fail_closed flag. Rules
// are sorted by (Priority ASC, UID ASC) so Resolve can scan in evaluation order
// and ties are deterministic. Safe to call concurrently with Resolve.
func (r *TenantMatchResolver) Load(rules []matchRule, failClosed bool) {
	cp := make([]matchRule, len(rules))
	copy(cp, rules)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Priority != cp[j].Priority {
			return cp[i].Priority < cp[j].Priority
		}
		return cp[i].UID < cp[j].UID
	})
	r.rules.Store(cp)
	r.closed.Store(failClosed)
}

// LoadFromDocs projects raw tenant_match documents (as returned by db.Driver
// Search) into matchRules and atomically replaces the snapshot. It keeps the
// matchRule type unexported while letting the plugin / reloader feed the
// resolver from DB rows. Documents missing match_type, match, or tenant_id are
// skipped. priority is read as a number (JSON decodes to float64) and defaults
// to 0.
func (r *TenantMatchResolver) LoadFromDocs(docs []map[string]any, failClosed bool) {
	rules := make([]matchRule, 0, len(docs))
	for _, d := range docs {
		mt, _ := d["match_type"].(string)
		m, _ := d["match"].(string)
		tid, _ := d["tenant_id"].(string)
		if mt == "" || m == "" || tid == "" {
			continue
		}
		uid, _ := d["uid"].(string)
		rules = append(rules, matchRule{
			MatchType: mt,
			Match:     m,
			TenantID:  tid,
			Priority:  asInt(d["priority"]),
			UID:       uid,
		})
	}
	r.Load(rules, failClosed)
}

// asInt coerces a JSON-decoded numeric value (float64/int/int64) to int.
func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

// HasRules reports whether the registry currently holds at least one rule. The
// login index uses it to surface tenant_match_enabled to the SPA.
func (r *TenantMatchResolver) HasRules() bool {
	return len(r.snapshot()) > 0
}

// snapshot returns the current rule slice (never nil to deref).
func (r *TenantMatchResolver) snapshot() []matchRule {
	v := r.rules.Load()
	if v == nil {
		return nil
	}
	rules, _ := v.([]matchRule)
	return rules
}

// Resolve maps an authenticated Identity to a tenant slug.
//
//  1. If requestedOrg is a non-empty, non-default slug the user was explicit:
//     the registry is skipped and requestedOrg is returned verbatim. An explicit
//     org always wins, even under fail_closed.
//  2. Otherwise the rules are evaluated in (priority, uid) order. The first hit
//     wins: group → any of id.Groups equals Match (case-insensitive); domain →
//     id.Email ends with "@"+Match (case-insensitive); login → id.Username
//     equals Match (case-insensitive).
//  3. On no hit: fail_closed → ("", ErrNoTenantMatch); otherwise
//     (DefaultTenant, nil) — the safe migration default.
func (r *TenantMatchResolver) Resolve(requestedOrg string, id Identity) (string, error) {
	// 1. Explicit, non-default org bypasses the registry entirely.
	if requestedOrg != "" && requestedOrg != snoozetypes.DefaultTenant {
		return requestedOrg, nil
	}

	// 2. Evaluate rules in order; first hit wins.
	for _, rule := range r.snapshot() {
		if matchRuleHit(rule, id) {
			return rule.TenantID, nil
		}
	}

	// 3. No hit.
	if r.closed.Load() {
		return "", ErrNoTenantMatch
	}
	return snoozetypes.DefaultTenant, nil
}

// matchRuleHit reports whether rule applies to id. Comparisons are
// case-insensitive across all three match types.
func matchRuleHit(rule matchRule, id Identity) bool {
	switch rule.MatchType {
	case "group":
		for _, g := range id.Groups {
			if strings.EqualFold(g, rule.Match) {
				return true
			}
		}
		return false
	case "domain":
		if id.Email == "" {
			return false
		}
		return strings.HasSuffix(strings.ToLower(id.Email), "@"+strings.ToLower(rule.Match))
	case "login":
		return strings.EqualFold(id.Username, rule.Match)
	default:
		return false
	}
}
