package heartbeat

import "time"

// HeartbeatStatus is the computed, read-time health signal projected onto every
// heartbeat document in the list/get response. It is an open string enum (an
// alias for string) so a future plan can add values (e.g. "slow") without
// reshaping the field. The name is intentionally explicit rather than the
// terser Status to read clearly at the StatusOK/StatusOverdue call sites and in
// the documented extension point for Plan 32.
//
//nolint:revive // intentional explicit name; alias for string, no stutter cost
type HeartbeatStatus = string

const (
	// StatusOK means the heartbeat has been pinged within interval+grace.
	StatusOK HeartbeatStatus = "ok"
	// StatusOverdue means the heartbeat has been silent longer than
	// interval+grace (or has never been pinged).
	StatusOverdue HeartbeatStatus = "overdue"
)

// computeStatus returns the read-time health status for a heartbeat as of now.
// It is a pure function (no plugin state, no clock) that mirrors the deadline
// logic in (*Plugin).isOverdue exactly: overdue when now is strictly After the
// deadline (lastSeen + window). At the exact deadline the heartbeat is still
// "ok". A never-pinged heartbeat (zero LastSeen) is overdue.
//
// Plan 32 will extend this with a "slow" branch ahead of the overdue check
// without changing the signature.
func computeStatus(hb heartbeat, now time.Time) HeartbeatStatus {
	deadline := hb.LastSeen.Add(hb.window())
	if now.After(deadline) {
		return StatusOverdue
	}
	return StatusOK
}
