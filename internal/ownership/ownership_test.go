package ownership

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
)

func TestTake(t *testing.T) {
	got := Take("alice", "ldap", 1_700_000_000)
	require.Equal(t, map[string]any{
		FieldOwner:               "alice",
		FieldOwnerMethod:         "ldap",
		FieldOwnerSince:          int64(1_700_000_000),
		FieldPreviousOwner:       "",
		FieldPreviousOwnerMethod: "",
	}, got, "taking ownership resets the ghost")
}

func TestClear_OwnedMovesOwnerToPrevious(t *testing.T) {
	existing := map[string]any{
		FieldOwner:               "alice",
		FieldOwnerMethod:         "ldap",
		FieldOwnerSince:          int64(42),
		FieldPreviousOwner:       "bob",
		FieldPreviousOwnerMethod: "local",
	}
	require.Equal(t, map[string]any{
		FieldOwner:               "",
		FieldOwnerMethod:         "",
		FieldOwnerSince:          int64(0),
		FieldPreviousOwner:       "alice",
		FieldPreviousOwnerMethod: "ldap",
	}, Clear(existing))
}

// A second clear must keep the ghost: previous_* is only rewritten when there
// is a current owner to move into it, and the explicit empties are still
// written so a merge write can never resurrect an owner.
func TestClear_UnownedKeepsPrevious(t *testing.T) {
	for name, existing := range map[string]map[string]any{
		"empty owner":   {FieldOwner: "", FieldPreviousOwner: "alice", FieldPreviousOwnerMethod: "ldap"},
		"absent owner":  {FieldPreviousOwner: "alice"},
		"nil record":    nil,
		"non-string":    {FieldOwner: 12},
		"no ownership":  {"state": "open"},
		"ghost present": {FieldOwner: "", FieldPreviousOwner: "carol"},
	} {
		t.Run(name, func(t *testing.T) {
			got := Clear(existing)
			require.Equal(t, map[string]any{
				FieldOwner:       "",
				FieldOwnerMethod: "",
				FieldOwnerSince:  int64(0),
			}, got)
		})
	}
}

func TestIsOwned(t *testing.T) {
	require.True(t, IsOwned(map[string]any{FieldOwner: "alice"}))
	require.False(t, IsOwned(map[string]any{FieldOwner: ""}))
	require.False(t, IsOwned(map[string]any{FieldPreviousOwner: "alice"}))
	require.False(t, IsOwned(map[string]any{}))
	require.False(t, IsOwned(nil))
}

func TestOwnedCondMatchesIsOwned(t *testing.T) {
	cond := OwnedCond()
	for _, doc := range []map[string]any{
		{FieldOwner: "alice"},
		{FieldOwner: ""},
		{FieldPreviousOwner: "alice"},
		{},
	} {
		require.Equal(t, IsOwned(doc), condition.Match(doc, cond), "doc %v", doc)
	}
}

func TestReopenTimers(t *testing.T) {
	require.Equal(t, map[string]any{
		"ack_until":    int64(0),
		"escalate_at":  int64(1_000 + 600),
		"shelve_until": int64(0),
	}, ReopenTimers(1_000, 10*time.Minute))
	require.Equal(t, map[string]any{
		"ack_until":    int64(0),
		"escalate_at":  int64(0),
		"shelve_until": int64(0),
	}, ReopenTimers(1_000, 0), "escalation disabled: nothing armed")
}

func TestRelease(t *testing.T) {
	acked := map[string]any{"state": "ack", FieldOwner: "alice", FieldOwnerMethod: "local"}
	patch, reopened := Release(acked, 1_000, 0)
	require.True(t, reopened)
	require.Equal(t, "open", patch["state"])
	require.Equal(t, "alice", patch[FieldPreviousOwner])
	require.Equal(t, "", patch[FieldOwner])
	require.Equal(t, int64(0), patch["ack_until"])

	esc := map[string]any{"state": "esc", FieldOwner: "alice"}
	patch, reopened = Release(esc, 1_000, time.Hour)
	require.False(t, reopened, "only an acknowledged record changes state")
	require.NotContains(t, patch, "state")
	require.NotContains(t, patch, "ack_until", "timers are untouched without a state change")
	require.Equal(t, "alice", patch[FieldPreviousOwner])
}

func TestMatchAssignee(t *testing.T) {
	users := []map[string]any{
		{"name": "alice", "method": "local", "enabled": true},
		{"name": "alice", "method": "ldap"}, // absent enabled = enabled
		{"name": "bob", "method": "local", "enabled": false},
		{"name": "carol", "method": "oidc"},
	}

	m, err := MatchAssignee(users, "alice", "ldap")
	require.NoError(t, err)
	require.Equal(t, "ldap", m)

	_, err = MatchAssignee(users, "alice", "")
	require.True(t, errors.Is(err, ErrAmbiguousAssignee), "two enabled alices")

	m, err = MatchAssignee(users, "carol", "")
	require.NoError(t, err)
	require.Equal(t, "oidc", m, "unique login fills the method in")

	_, err = MatchAssignee(users, "bob", "")
	require.True(t, errors.Is(err, ErrUnknownAssignee), "disabled user")
	_, err = MatchAssignee(users, "bob", "local")
	require.True(t, errors.Is(err, ErrUnknownAssignee), "disabled user, explicit method")
	_, err = MatchAssignee(users, "carol", "local")
	require.True(t, errors.Is(err, ErrUnknownAssignee), "method mismatch")
	_, err = MatchAssignee(users, "dave", "")
	require.True(t, errors.Is(err, ErrUnknownAssignee))
	_, err = MatchAssignee(users, "", "")
	require.True(t, errors.Is(err, ErrUnknownAssignee), "empty login")
}

func TestUserCond(t *testing.T) {
	require.Equal(t, condition.Equals("name", "alice"), UserCond("alice"))
}
