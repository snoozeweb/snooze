// Package ownership holds the alert-ownership primitives shared by every
// producer that changes who is working on a record: the comment plugin
// (ack/close/open/esc/assign/release), the aggregaterule processor (automatic
// re-open and re-escalation), the housekeeper's escalate-timeout sweep, the
// bulk endpoints and the one-shot `migrate owners` backfill.
//
// Ownership lives in five UNTYPED record keys (they ride in Record.Extra, not
// in typed snoozetypes.Record fields):
//
//	owner                  login of the current owner; "" / absent = unowned
//	owner_method           auth method of the owner (local, ldap, oidc, …)
//	owner_since            epoch seconds ownership was taken; 0 when unowned
//	previous_owner         the owner before the last clear (the UI's ghost)
//	previous_owner_method  its auth method
//
// Clearing always writes explicit empty values ("" / 0), never an unset. The
// pipeline persists aggregate duplicates with a merge write that leaves absent
// keys untouched, so an unset from the pipeline path is impossible and an
// absent key would resurrect the old owner on the next occurrence. The same
// reason makes aggregaterule.resetEscalation write zeros.
//
// Everything here is a pure function over map[string]any (a db.Document is
// one): no driver, no clock. Callers read the row, compute the patch and write
// it themselves, so each keeps its own write path and error handling.
package ownership

import (
	"errors"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
)

// The ownership keys on a record document.
const (
	FieldOwner               = "owner"
	FieldOwnerMethod         = "owner_method"
	FieldOwnerSince          = "owner_since"
	FieldPreviousOwner       = "previous_owner"
	FieldPreviousOwnerMethod = "previous_owner_method"
)

// Fields lists every ownership key, for callers that ferry them as a set
// (aggregaterule carries them forward onto a duplicate occurrence).
var Fields = []string{
	FieldOwner, FieldOwnerMethod, FieldOwnerSince,
	FieldPreviousOwner, FieldPreviousOwnerMethod,
}

// Take returns the patch that makes name (authenticated by method) the owner
// as of now. Taking ownership also resets the previous-owner ghost: the alert
// is being worked on again, so the faded avatar has nothing left to say.
func Take(name, method string, now int64) map[string]any {
	return map[string]any{
		FieldOwner:               name,
		FieldOwnerMethod:         method,
		FieldOwnerSince:          now,
		FieldPreviousOwner:       "",
		FieldPreviousOwnerMethod: "",
	}
}

// Clear returns the patch that drops the current owner of existing (the row as
// stored, BEFORE the patch). A current owner moves into previous_*; on a record
// that is already unowned previous_* is left out of the patch, so a second
// clear keeps the ghost rather than wiping it. The owner keys themselves are
// always written as explicit empties — see the package doc for why.
func Clear(existing map[string]any) map[string]any {
	patch := map[string]any{
		FieldOwner:       "",
		FieldOwnerMethod: "",
		FieldOwnerSince:  int64(0),
	}
	if IsOwned(existing) {
		patch[FieldPreviousOwner] = existing[FieldOwner]
		method, _ := existing[FieldOwnerMethod].(string)
		patch[FieldPreviousOwnerMethod] = method
	}
	return patch
}

// IsOwned reports whether doc has a current owner. A ghost-only record (just a
// previous_owner) is unowned.
func IsOwned(doc map[string]any) bool {
	owner, _ := doc[FieldOwner].(string)
	return owner != ""
}

// OwnedCond is the query-side twin of IsOwned: `owner` present and non-empty.
// Its negation is the "Unowned" bucket, OR(NOT EXISTS owner, owner = "").
func OwnedCond() condition.Cond {
	return condition.And(
		condition.Exists(FieldOwner),
		condition.Not(condition.Equals(FieldOwner, "")),
	)
}

// ReopenTimers returns the timed-lifecycle deadlines a record gets when it
// returns to `open`: the ack expiry is lifted, the escalation deadline is
// (re-)armed escalateAfter from now (or disarmed when escalation is off), and
// any timed shelve is lifted. It is the comment plugin's `open` stamping, kept
// here so a `release` of an acknowledged record — single or bulk — returns it
// to open on exactly the same clock.
func ReopenTimers(now int64, escalateAfter time.Duration) map[string]any {
	escalateAt := int64(0)
	if escalateAfter > 0 {
		escalateAt = now + int64(escalateAfter.Seconds())
	}
	return map[string]any{
		"ack_until":    int64(0),
		"escalate_at":  escalateAt,
		"shelve_until": int64(0),
	}
}

// Release returns the patch for an explicit release of existing: Clear, plus —
// when the record is acknowledged — a return to `open` with ReopenTimers.
// reopened reports that state change, so the caller also removes `acked_by`
// (an unset, which the Clear-style patch cannot express) exactly as a manual
// `open` does. Any other state keeps its state and timers.
func Release(existing map[string]any, now int64, escalateAfter time.Duration) (patch map[string]any, reopened bool) {
	patch = Clear(existing)
	if state, _ := existing["state"].(string); state != "ack" {
		return patch, false
	}
	patch["state"] = "open"
	for k, v := range ReopenTimers(now, escalateAfter) {
		patch[k] = v
	}
	return patch, true
}

// ErrUnknownAssignee is returned by MatchAssignee when no enabled user with the
// requested login (and method, when given) exists.
var ErrUnknownAssignee = errors.New("unknown or disabled assignee")

// ErrAmbiguousAssignee is returned by MatchAssignee when the login alone
// matches several enabled users (one per auth method) and no method was given.
var ErrAmbiguousAssignee = errors.New("ambiguous assignee: several users share this login, give assignee_method")

// UserCond is the `user` collection query that feeds MatchAssignee. The caller
// runs it under the request's tenant context, so only that tenant's users
// can be assignees.
func UserCond(name string) condition.Cond {
	return condition.Equals("name", name)
}

// MatchAssignee resolves an assignee against the user documents matching
// UserCond(name) and returns the auth method to stamp as owner_method.
//
// Only enabled users count; an absent `enabled` is enabled, mirroring the login
// providers, which only refuse an explicit false. With method given, that exact
// (name, method) user must exist. Without it the login must be unambiguous: one
// enabled user fills the method in, several are ErrAmbiguousAssignee.
func MatchAssignee(users []map[string]any, name, method string) (string, error) {
	if name == "" {
		return "", ErrUnknownAssignee
	}
	var candidates []string
	for _, u := range users {
		if n, _ := u["name"].(string); n != name {
			continue
		}
		if enabled, ok := u["enabled"].(bool); ok && !enabled {
			continue
		}
		m, _ := u["method"].(string)
		if method != "" && m != method {
			continue
		}
		candidates = append(candidates, m)
	}
	switch len(candidates) {
	case 0:
		return "", ErrUnknownAssignee
	case 1:
		return candidates[0], nil
	default:
		return "", ErrAmbiguousAssignee
	}
}
