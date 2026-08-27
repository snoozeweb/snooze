package script

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// A script is the one notifier whose escalation behaviour Snooze cannot decide,
// so it gets the context and makes its own call.
func TestAddEscalationEnv(t *testing.T) {
	env := map[string]string{}
	addEscalationEnv(env, snoozetypes.Record{State: "esc"}, plugins.Escalation{
		Count: 2, Reason: "manual", Actor: "alice", PreviousSeverity: "warning",
	})

	require.Equal(t, "esc", env["SNOOZE_STATE"])
	require.Equal(t, "2", env["SNOOZE_ESCALATION_COUNT"])
	require.Equal(t, "manual", env["SNOOZE_ESCALATION_REASON"])
	require.Equal(t, "alice", env["SNOOZE_ESCALATION_ACTOR"])
	require.Equal(t, "warning", env["SNOOZE_PREVIOUS_SEVERITY"])
}

// The zero forms are exported too, so a script can branch on the value without
// having to distinguish "absent" from "not escalated".
func TestAddEscalationEnvOnFirstDelivery(t *testing.T) {
	env := map[string]string{}
	addEscalationEnv(env, snoozetypes.Record{}, plugins.Escalation{})
	require.Equal(t, "0", env["SNOOZE_ESCALATION_COUNT"])
	require.Contains(t, env, "SNOOZE_ESCALATION_REASON")
	require.Empty(t, env["SNOOZE_ESCALATION_REASON"])
}

// An operator who set one of these names explicitly keeps their value.
func TestUserEnvWins(t *testing.T) {
	env := map[string]string{"SNOOZE_ESCALATION_COUNT": "mine"}
	addEscalationEnv(env, snoozetypes.Record{}, plugins.Escalation{Count: 5})
	require.Equal(t, "mine", env["SNOOZE_ESCALATION_COUNT"])
}
