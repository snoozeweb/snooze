package ntfy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// sendEsc dispatches sampleRecord() at the given escalation to a recording
// server, returning what it captured.
func sendEsc(t *testing.T, esc plugins.Escalation) *captured {
	t.Helper()
	srv, capt := newRecordingServer(t)
	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta:       map[string]any{"server": srv.URL, "topic": "alerts"},
		Escalation: esc,
	}))
	capt.mu.Lock()
	defer capt.mu.Unlock()
	return &captured{body: capt.body, priority: capt.priority, tags: capt.tags}
}

// ntfy has no threading, so escalation is expressed through urgency. The
// priority must only ever go up, and must stop at ntfy's maximum.
func TestRaisePriorityOnlyClimbs(t *testing.T) {
	require.Equal(t, "2", raisePriority("1"))
	require.Equal(t, "4", raisePriority("3"))
	require.Equal(t, "5", raisePriority("4"))
	require.Equal(t, "5", raisePriority("5"), "must clamp at ntfy's maximum")
	require.Equal(t, "5", raisePriority("9"), "an out-of-range value must not climb further")
	// An absent or unparseable priority errs loud: the caller only asks for a
	// raise on a re-escalation.
	require.Equal(t, "5", raisePriority(""))
	require.Equal(t, "5", raisePriority("high"))
}

func TestAddTag(t *testing.T) {
	require.Equal(t, "rotating_light", addTag("", "rotating_light"))
	require.Equal(t, "warning,rotating_light", addTag("warning", "rotating_light"))
	require.Equal(t, "warning, rotating_light",
		addTag("warning, rotating_light", "rotating_light"),
		"an existing tag must not be duplicated")
}

func TestEscalationHeaders(t *testing.T) {
	base := sendEsc(t, plugins.Escalation{})
	esc := sendEsc(t, plugins.Escalation{Count: 2, Reason: "timeout"})

	require.Greater(t, esc.priority, base.priority,
		"an escalation must be strictly louder than the delivery before it")
	require.Contains(t, esc.tags, escalationTag)
	require.Contains(t, esc.body, "New escalation #2")
	require.Contains(t, esc.body, "(timeout)")
}

func TestFirstDeliveryUnchanged(t *testing.T) {
	base := sendEsc(t, plugins.Escalation{})
	require.NotContains(t, base.tags, escalationTag)
	require.NotContains(t, base.body, "New escalation")
}
