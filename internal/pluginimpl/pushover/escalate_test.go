package pushover

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Pushover has no threading, so escalation is expressed through urgency. The
// priority only ever climbs, and stops at 2 (emergency), which forces retry
// until the recipient acknowledges — the point being that a re-escalated alert
// should not be dismissible by a glance at a lock screen.
func TestRaisePriorityOnlyClimbs(t *testing.T) {
	require.Equal(t, -1, raisePriority(-2))
	require.Equal(t, 0, raisePriority(-1))
	require.Equal(t, 1, raisePriority(0))
	require.Equal(t, 2, raisePriority(1))
	require.Equal(t, 2, raisePriority(2), "must clamp at emergency")
}
