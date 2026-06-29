package heartbeat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestComputeStatusOK: a heartbeat last seen 10s ago with a 60s interval is
// well within its window → "ok".
func TestComputeStatusOK(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:     "hb1",
		Interval: 60,
		LastSeen: now.Add(-10 * time.Second),
	}
	require.Equal(t, StatusOK, computeStatus(hb, now))
}

// TestComputeStatusOverdue: a heartbeat last seen 5m ago with a 60s interval is
// long past its window → "overdue".
func TestComputeStatusOverdue(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:     "hb1",
		Interval: 60,
		LastSeen: now.Add(-5 * time.Minute),
	}
	require.Equal(t, StatusOverdue, computeStatus(hb, now))
}

// TestComputeStatusNeverSeen: a heartbeat that has never been pinged (zero
// LastSeen) is overdue regardless of interval — its deadline anchors at the
// zero time, far in the past.
func TestComputeStatusNeverSeen(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:     "hb1",
		Interval: 60,
		// LastSeen is the zero time (never pinged).
	}
	require.Equal(t, StatusOverdue, computeStatus(hb, now))
}

// TestComputeStatusAtExactDeadline: when now == lastSeen + window the boundary
// is After (not AfterOrEqual), matching isOverdue, so the heartbeat is still
// "ok" exactly at its deadline.
func TestComputeStatusAtExactDeadline(t *testing.T) {
	lastSeen := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:     "hb1",
		Interval: 60,
		LastSeen: lastSeen,
	}
	now := lastSeen.Add(hb.window()) // exactly the deadline
	require.Equal(t, StatusOK, computeStatus(hb, now))
}
