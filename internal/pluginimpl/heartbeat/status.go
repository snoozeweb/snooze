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
	// StatusSlow means the heartbeat is still within interval+grace but its
	// most recent ping latency exceeded max_latency — an early-warning signal.
	StatusSlow HeartbeatStatus = "slow"
)

// overdue is the single source of truth for the overdue predicate. A heartbeat
// is overdue when now is strictly After its deadline (lastSeen + window); at the
// exact deadline it is still "ok". A never-pinged heartbeat (zero LastSeen) has
// its deadline anchored far in the past and is therefore overdue. Both
// computeStatus and (*Plugin).isOverdue call this so the read-time status and
// the scanner can never diverge.
func overdue(hb heartbeat, now time.Time) bool {
	deadline := hb.LastSeen.Add(hb.window())
	return now.After(deadline)
}

// isSlow reports whether the heartbeat is "slow": latency tracking is enabled
// (MaxLatency > 0), the most recent ping latency exceeded the threshold, and the
// heartbeat is still within its window. A negative LastLatency (clock skew)
// never trips the threshold. Slow and overdue are mutually exclusive — overdue
// takes priority — so isSlow is false whenever the heartbeat is overdue. Pure
// function, no receiver.
func isSlow(hb heartbeat, now time.Time) bool {
	return hb.MaxLatency > 0 && hb.LastLatency > hb.MaxLatency && !overdue(hb, now)
}

// computeStatus returns the read-time health status for a heartbeat as of now.
// It is a pure function (no plugin state, no clock). Priority order: overdue
// first (the dead-man's-switch has expired), then slow (within window but
// latency over threshold), else ok. This keeps "slow" and "overdue" mutually
// exclusive and consistent with the scanner.
func computeStatus(hb heartbeat, now time.Time) HeartbeatStatus {
	if overdue(hb, now) {
		return StatusOverdue
	}
	if isSlow(hb, now) {
		return StatusSlow
	}
	return StatusOK
}
