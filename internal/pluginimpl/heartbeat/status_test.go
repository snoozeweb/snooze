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

// TestIsSlowFalseWhenNoMaxLatency: with MaxLatency == 0 latency tracking is off,
// so isSlow is always false regardless of LastLatency.
func TestIsSlowFalseWhenNoMaxLatency(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:        "hb1",
		Interval:    60,
		LastSeen:    now.Add(-10 * time.Second),
		MaxLatency:  0,
		LastLatency: 99999,
	}
	require.False(t, isSlow(hb, now))
}

// TestIsSlowFalseWhenUnderThreshold: latency below the threshold is not slow.
func TestIsSlowFalseWhenUnderThreshold(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:        "hb1",
		Interval:    60,
		LastSeen:    now.Add(-10 * time.Second),
		MaxLatency:  2000,
		LastLatency: 1500,
	}
	require.False(t, isSlow(hb, now))
}

// TestIsSlowTrueWhenOverThreshold: latency above the threshold while within the
// window is slow.
func TestIsSlowTrueWhenOverThreshold(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:        "hb1",
		Interval:    60,
		LastSeen:    now.Add(-10 * time.Second),
		MaxLatency:  2000,
		LastLatency: 2500,
	}
	require.True(t, isSlow(hb, now))
}

// TestIsSlowFalseWhenOverdue: an overdue heartbeat is never slow — overdue takes
// priority and the two are mutually exclusive.
func TestIsSlowFalseWhenOverdue(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:        "hb1",
		Interval:    60,
		LastSeen:    now.Add(-5 * time.Minute), // overdue
		MaxLatency:  2000,
		LastLatency: 5000,
	}
	require.False(t, isSlow(hb, now))
}

// TestComputeStatusSlow: within window but over the latency threshold → slow.
func TestComputeStatusSlow(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:        "hb1",
		Interval:    60,
		LastSeen:    now.Add(-10 * time.Second),
		MaxLatency:  2000,
		LastLatency: 3000,
	}
	require.Equal(t, StatusSlow, computeStatus(hb, now))
}

// TestComputeStatusSlowVsOverdue: overdue takes priority over slow.
func TestComputeStatusSlowVsOverdue(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	hb := heartbeat{
		Name:        "hb1",
		Interval:    60,
		LastSeen:    now.Add(-5 * time.Minute), // overdue
		MaxLatency:  2000,
		LastLatency: 9000,
	}
	require.Equal(t, StatusOverdue, computeStatus(hb, now))
}
