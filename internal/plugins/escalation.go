package plugins

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// notifyRefPrefix is the record-field prefix under which a notifier persists
// the external handle it created for an alert (a JIRA issue key, a Slack
// message ts, a mail Message-ID …). One field per action, so two actions
// pointing at the same notifier keep independent handles.
const notifyRefPrefix = "notify_ref_"

// legacyRefPrefix is the pre-existing webhook `inject_response` field prefix.
// The Teams bridge threads follow-up replies through it (see
// internal/pluginimpl/webhook), so NotifyRef keeps reading it: a deployment
// that already has `response_<action>.message_ids` on its records must keep
// threading after this upgrade, without a migration.
const legacyRefPrefix = "response_"

// maxNotifyRefBytes bounds a stored handle. Handles are meant to be a couple
// of identifiers; anything larger is a notifier bug and would bloat every
// record it touches, so StoreNotifyRef drops it rather than persisting it.
const maxNotifyRefBytes = 4096

// Escalation describes why a notifier is being invoked again for an alert it
// has already delivered.
//
// Count == 0 means first delivery. Every notifier MUST treat the zero value as
// "first fire" so a record predating the escalation fields — or one flowing
// through a caller that doesn't populate this — behaves exactly as it did
// before escalation-awareness existed.
type Escalation struct {
	// Count is the number of re-escalations so far: 0 on a first delivery, 1
	// on the first re-escalation, and so on.
	Count int
	// Reason names the producer: "timeout", "manual", "watchlist", or "".
	Reason string
	// Actor is the login of the operator who escalated, on a manual
	// escalation only.
	Actor string
	// At is when the current escalation was stamped; zero when never
	// escalated.
	At time.Time
	// Duplicates is aggregaterule's occurrence counter for the alert. Distinct
	// from Count: an alert can recur many times without ever escalating.
	Duplicates int64
	// PreviousSeverity and Trend are aggregaterule's derived severity-movement
	// pair ("up" / "down" / "same"), used by notifiers that raise a ticket
	// priority only when severity actually rose.
	PreviousSeverity string
	Trend            string
}

// IsRe reports whether this delivery is a re-escalation rather than a first
// fire. The canonical branch for notifiers.
func (e Escalation) IsRe() bool { return e.Count > 0 }

// SeverityRose reports whether the alert's severity increased relative to its
// previous occurrence. Notifiers use it to decide whether to raise a priority
// on the external object.
func (e Escalation) SeverityRose() bool { return e.Trend == "up" }

// Ordinal renders the human-facing escalation number ("#3"), or "" on a first
// fire, for message bodies and ticket comments.
func (e Escalation) Ordinal() string {
	if e.Count <= 0 {
		return ""
	}
	return "#" + itoa(e.Count)
}

// Banner is the short human-facing marker a notifier prefixes onto a
// re-escalation message: "New escalation #3 (timeout)". Empty on a first fire,
// so a caller can prefix unconditionally.
//
// Deliberately plain text with no markup: the chat notifiers that use it have
// operator-configurable (or absent) markup modes, and a literal "*" showing up
// in a page is worse than an unstyled one.
func (e Escalation) Banner() string {
	if e.Count <= 0 {
		return ""
	}
	b := "New escalation " + e.Ordinal()
	if e.Reason != "" {
		b += " (" + e.Reason + ")"
	}
	return b
}

// PrefixMessage returns msg with the escalation Banner prepended on its own
// line, or msg unchanged on a first fire. The canonical way for a notifier that
// cannot thread to distinguish a re-escalation from a first delivery.
func (e Escalation) PrefixMessage(msg string) string {
	banner := e.Banner()
	switch {
	case banner == "":
		return msg
	case msg == "":
		return "⚠️ " + banner
	default:
		return "⚠️ " + banner + "\n" + msg
	}
}

// itoa avoids pulling strconv into every caller of Ordinal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// EscalationFrom projects the escalation context carried by rec. The typed
// fields supply Count/Reason/Actor/At; Duplicates and the severity-trend pair
// come from the untyped aggregaterule counters in Extra.
func EscalationFrom(rec snoozetypes.Record) Escalation {
	e := Escalation{
		Count:            rec.EscalationCount,
		Reason:           rec.EscalationReason,
		Actor:            rec.EscalationActor,
		Duplicates:       extraInt64(rec.Extra, "duplicates"),
		PreviousSeverity: extraString(rec.Extra, "previous_severity"),
		Trend:            extraString(rec.Extra, "trend_indication"),
	}
	if rec.EscalatedAt != 0 {
		e.At = time.Unix(rec.EscalatedAt, 0).UTC()
	}
	// A record read straight out of the driver may carry the typed fields in
	// Extra instead (the drivers decode numbers as int64/float64 and the
	// housekeeper's doc→record hop goes through JSON), so fall back to Extra
	// for anything the typed fields didn't supply.
	if e.Count == 0 {
		e.Count = int(extraInt64(rec.Extra, "escalation_count"))
	}
	if e.Reason == "" {
		e.Reason = extraString(rec.Extra, "escalation_reason")
	}
	if e.Actor == "" {
		e.Actor = extraString(rec.Extra, "escalation_actor")
	}
	if e.At.IsZero() {
		if at := extraInt64(rec.Extra, "escalated_at"); at != 0 {
			e.At = time.Unix(at, 0).UTC()
		}
	}
	return e
}

// NotifyRef returns the external handle a previous Send stored for actionName,
// or nil when there is none.
//
// Lookup order: the canonical `notify_ref_<action>` field first, then the
// legacy `response_<action>` written by webhook's inject_response. Returning
// nil for a first fire, an unknown action, or a malformed value is deliberate —
// every notifier's "no handle" branch is the create path, which is also the
// correct behaviour when a handle was lost.
func NotifyRef(rec snoozetypes.Record, actionName string) map[string]any {
	if actionName == "" || rec.Extra == nil {
		return nil
	}
	for _, prefix := range []string{notifyRefPrefix, legacyRefPrefix} {
		if ref, ok := rec.Extra[prefix+actionName].(map[string]any); ok && len(ref) > 0 {
			return ref
		}
	}
	return nil
}

// NotifyRefString reads one string field out of the handle, tolerating every
// absent layer.
func NotifyRefString(rec snoozetypes.Record, actionName, key string) string {
	ref := NotifyRef(rec, actionName)
	if ref == nil {
		return ""
	}
	s, _ := ref[key].(string)
	return s
}

// StoreNotifyRef persists ref as this action's external handle via the
// payload's Inject callback, so the next delivery for the same alert can find
// it. A nil Inject (direct callers, tests) is a silent no-op, as is an empty
// action name or an empty ref.
//
// The write is whole-field: a notifier that wants to preserve earlier keys
// should merge them into ref itself (see MergeNotifyRef).
func StoreNotifyRef(payload NotificationPayload, actionName string, ref map[string]any) {
	if actionName == "" || len(ref) == 0 {
		return
	}
	raw, err := json.Marshal(ref)
	if err != nil || len(raw) > maxNotifyRefBytes {
		return
	}
	InjectField(payload.Inject, notifyRefPrefix+actionName, ref)
}

// MergeNotifyRef returns the record's existing handle with the given keys
// overlaid, so a notifier can add a field without dropping one another code
// path stored. The returned map is always a fresh copy.
func MergeNotifyRef(rec snoozetypes.Record, actionName string, add map[string]any) map[string]any {
	out := make(map[string]any, len(add)+4)
	for k, v := range NotifyRef(rec, actionName) {
		out[k] = v
	}
	for k, v := range add {
		out[k] = v
	}
	return out
}

// IsNotifyRefField reports whether a record field name is one of the handle
// fields that must survive an aggregaterule duplicate merge.
func IsNotifyRefField(name string) bool {
	return strings.HasPrefix(name, notifyRefPrefix) || strings.HasPrefix(name, legacyRefPrefix)
}

// extraString reads a string out of the untyped Extra map.
func extraString(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	s, _ := extra[key].(string)
	return s
}

// extraInt64 reads a number out of the untyped Extra map, tolerating the
// int / int64 / float64 shapes the different drivers and the JSON hop produce.
func extraInt64(extra map[string]any, key string) int64 {
	if extra == nil {
		return 0
	}
	switch v := extra[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return 0
	}
}
