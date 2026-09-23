package comment

import (
	"errors"
	"fmt"
)

// ErrInvalidTransition is the sentinel returned (wrapped with the offending
// state/action pair) when a state-changing comment is not legal from the linked
// record's current state. Plans 27 (acked-by denormalisation) and 36 (chat-ops
// ack shortcuts) import it by name to detect rejected transitions via errors.Is.
var ErrInvalidTransition = errors.New("invalid state transition")

// allowedTransitions encodes the (current_state, action) validity table from
// Plan 05's Design section, keyed [currentState][action]. A true value means the
// action is legal from that state. A state absent from the map, or an action
// absent from a present state's inner map, is treated as fail-open by
// ValidateTransition (returns nil).
var allowedTransitions = map[string]map[string]bool{
	// "" = fresh / open: may be acked or closed; nothing to re-open or esc yet.
	"": {"ack": true, "close": true, "open": false, "esc": false},
	// acked: may be closed, re-opened, or escalated; double-ack is rejected.
	"ack": {"ack": false, "close": true, "open": true, "esc": true},
	// escalated: may be acked, closed, or re-opened; double-esc is rejected.
	"esc": {"ack": true, "close": true, "open": true, "esc": false},
	// closed: only re-open makes sense.
	"close": {"ack": false, "close": false, "open": true, "esc": false},
	// re-opened: may be acked or closed; re-open and esc are rejected.
	"open": {"ack": true, "close": true, "open": false, "esc": false},
}

// stateChangingActions is the set of comment types that drive a record state
// transition. An action outside this set is a free-form comment, not a
// transition, and ValidateTransition lets it pass through.
var stateChangingActions = map[string]bool{
	"ack": true, "close": true, "open": true, "esc": true,
	"shelve": true, "unshelve": true,
}

// ownershipActions is the set of comment types that change who owns the linked
// record without a state transition of their own: `assign` makes the comment's
// `assignee` the owner and `release` clears the owner. They are deliberately
// NOT in stateChangingActions — ValidateTransition passes them through and the
// validity table has no row for them; GuardWrite applies their own
// preconditions (see guardOwnership). A `release` of an acknowledged record
// does move it back to `open`, but as a consequence of dropping the owner, not
// as a transition the table governs.
var ownershipActions = map[string]bool{
	"assign": true, "release": true,
}

// stateForAction maps a state-changing comment type to the record `state` it
// drives. Most actions are their own state (ack→"ack"); the timed-shelve pair
// is the exception — "shelve" parks the record in "shelved" and "unshelve"
// returns it to "open". A missing entry means action==state.
var stateForAction = map[string]string{
	"shelve":   "shelved",
	"unshelve": "open",
}

// alwaysAllowedActions are state-changing actions with no per-state validity
// table: they are legal from any current state and so fail-open in
// ValidateTransition. The timed-shelve pair belongs here — an operator may
// shelve a noisy alert (or unshelve it) regardless of its ack/open/close
// posture, mirroring Alerta's permissive shelve action.
var alwaysAllowedActions = map[string]bool{
	"shelve": true, "unshelve": true,
}

// ValidateTransition returns ErrInvalidTransition (wrapped with the specific
// state/action pair) when action is not legal from currentState, and nil
// otherwise. It is a pure function with no DB dependency so sibling packages can
// reuse it. Unknown currentState values and non-state-changing actions fail open
// (return nil), consistent with Alerta's permissive ACT-1 rule.
func ValidateTransition(currentState, action string) error {
	if !stateChangingActions[action] {
		return nil
	}
	if alwaysAllowedActions[action] {
		return nil // shelve/unshelve are legal from any state (fail-open)
	}
	actions, known := allowedTransitions[currentState]
	if !known {
		return nil // fail-open on an unknown current state
	}
	if actions[action] {
		return nil
	}
	return fmt.Errorf("%w: %q from state %q", ErrInvalidTransition, action, currentState)
}
