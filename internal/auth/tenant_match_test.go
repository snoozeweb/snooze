package auth

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestTenantMatchResolver_GroupMatch(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	r.Load([]matchRule{
		{MatchType: "group", Match: "Ops-Team", TenantID: "acme", Priority: 0, UID: "u1"},
	}, false)
	// Group comparison is case-insensitive.
	got, err := r.Resolve("", Identity{Username: "alice", Groups: []string{"ops-team"}})
	require.NoError(t, err)
	require.Equal(t, "acme", got)
}

func TestTenantMatchResolver_DomainMatch(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	r.Load([]matchRule{
		{MatchType: "domain", Match: "Example.COM", TenantID: "globex", Priority: 0, UID: "u1"},
	}, false)
	got, err := r.Resolve("", Identity{Username: "bob", Email: "Bob@example.com"})
	require.NoError(t, err)
	require.Equal(t, "globex", got)
}

func TestTenantMatchResolver_LoginMatch(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	r.Load([]matchRule{
		{MatchType: "login", Match: "Carol", TenantID: "initech", Priority: 0, UID: "u1"},
	}, false)
	got, err := r.Resolve("", Identity{Username: "carol"})
	require.NoError(t, err)
	require.Equal(t, "initech", got)
}

func TestTenantMatchResolver_PriorityOrder(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	// Two rules both match alice's group; the lower-priority rule must win.
	r.Load([]matchRule{
		{MatchType: "group", Match: "ops", TenantID: "high", Priority: 10, UID: "uB"},
		{MatchType: "group", Match: "ops", TenantID: "low", Priority: 1, UID: "uA"},
	}, false)
	got, err := r.Resolve("", Identity{Username: "alice", Groups: []string{"ops"}})
	require.NoError(t, err)
	require.Equal(t, "low", got, "lower priority value must be evaluated first")
}

func TestTenantMatchResolver_NoMatch_OpenMode(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	r.Load([]matchRule{
		{MatchType: "group", Match: "ops", TenantID: "acme", Priority: 0, UID: "u1"},
	}, false) // fail_closed = false
	got, err := r.Resolve("", Identity{Username: "nobody", Groups: []string{"other"}})
	require.NoError(t, err)
	require.Equal(t, snoozetypes.DefaultTenant, got)
}

func TestTenantMatchResolver_NoMatch_FailClosed(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	r.Load([]matchRule{
		{MatchType: "group", Match: "ops", TenantID: "acme", Priority: 0, UID: "u1"},
	}, true) // fail_closed = true
	got, err := r.Resolve("", Identity{Username: "nobody", Groups: []string{"other"}})
	require.ErrorIs(t, err, ErrNoTenantMatch)
	require.Equal(t, "", got)
}

func TestTenantMatchResolver_ExplicitOrg_Skipped(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	// A rule that would route to acme; but the user supplied an explicit,
	// non-default org, so the resolver must skip the lookup entirely.
	r.Load([]matchRule{
		{MatchType: "group", Match: "ops", TenantID: "acme", Priority: 0, UID: "u1"},
	}, true) // even fail_closed must not deny an explicit org
	got, err := r.Resolve("other", Identity{Username: "alice", Groups: []string{"ops"}})
	require.NoError(t, err)
	require.Equal(t, "other", got, "explicit non-default org always wins")
}

func TestTenantMatchResolver_DefaultOrg_NotExplicit(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	r.Load([]matchRule{
		{MatchType: "group", Match: "ops", TenantID: "acme", Priority: 0, UID: "u1"},
	}, false)
	// requestedOrg == DefaultTenant is NOT treated as explicit: the registry
	// still gets a chance to route the user.
	got, err := r.Resolve(snoozetypes.DefaultTenant, Identity{Username: "alice", Groups: []string{"ops"}})
	require.NoError(t, err)
	require.Equal(t, "acme", got)
}

func TestTenantMatchResolver_Empty_OpenMode(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	// No rules loaded at all, open mode: unmatched user lands in default.
	got, err := r.Resolve("", Identity{Username: "alice"})
	require.NoError(t, err)
	require.Equal(t, snoozetypes.DefaultTenant, got)
}

func TestTenantMatchResolver_PrecedenceGroupOverDomain(t *testing.T) {
	t.Parallel()
	r := NewTenantMatchResolver()
	// A group rule (priority 0) and a domain rule (priority 1) both could match;
	// the lower priority (group) wins. This documents the group→domain→login
	// precedence achieved purely through priority ordering.
	r.Load([]matchRule{
		{MatchType: "domain", Match: "example.com", TenantID: "by-domain", Priority: 1, UID: "uDom"},
		{MatchType: "group", Match: "ops", TenantID: "by-group", Priority: 0, UID: "uGrp"},
	}, false)
	got, err := r.Resolve("", Identity{Username: "alice", Groups: []string{"ops"}, Email: "alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, "by-group", got)
}
