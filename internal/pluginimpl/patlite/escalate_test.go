package patlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// A tower light has no message and no history, so the only way it can express a
// re-escalation is by getting harder to ignore.
func TestEscalateActionRaisesTheLightState(t *testing.T) {
	esc := plugins.Escalation{Count: 1}

	require.Equal(t, "blink1", escalateAction(SeverityAction{Color: "red", State: "on"}, esc).State)
	require.Equal(t, "blink1", escalateAction(SeverityAction{Color: "red"}, esc).State,
		"an empty state means on, so it advances to blink1")
	require.Equal(t, "blink2", escalateAction(SeverityAction{Color: "red", State: "blink1"}, esc).State)
	require.Equal(t, "blink2", escalateAction(SeverityAction{Color: "red", State: "blink2"}, esc).State,
		"blink2 is the most insistent state the firmware offers")
}

// The colour is the severity's to report; an escalation must not rewrite it.
func TestEscalateActionLeavesColourAlone(t *testing.T) {
	got := escalateAction(SeverityAction{Color: "amber", State: "on"}, plugins.Escalation{Count: 3})
	require.Equal(t, "amber", got.Color)
}

// An alert being escalated is never expressed by turning the lamp off.
func TestEscalateActionIgnoresClear(t *testing.T) {
	for _, colour := range []string{"", "clear", "off"} {
		action := SeverityAction{Color: colour}
		require.Equal(t, action, escalateAction(action, plugins.Escalation{Count: 2}))
	}
}

func TestFirstDeliveryUnchanged(t *testing.T) {
	action := SeverityAction{Color: "red", State: "on"}
	require.Equal(t, action, escalateAction(action, plugins.Escalation{}))
}
