package teams

// parser.go is the small grammar the bridge needs to interpret two free-form
// chat-command arguments:
//
//   - The duration token in `snooze <duration> [condition]`.
//   - The leading modification list in
//     `esc field = value field += other rest is a comment`.
//
// The Python plugin used pyparsing for both; we keep the Go port intentionally
// narrower — just the cases real operators type. Every helper here is pure so
// it can be unit-tested without a snoozeclient or a Graph stub.

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// durationRE captures (number)(unit) tokens, plus the literal "forever".
// Matches the Python regex `^(forever|(\d+)(m(?:ins?)?|h(?:ours?)?|...)$`
// — slightly tightened so a typo like "6 hours" still matches. (?i)
// keeps the match case-insensitive ("FOREVER" works just like "forever").
//
// Alternation order matters: Go's RE2 uses leftmost-first semantics, so
// longer literals must come before their shorter prefixes ("months"
// before "month" before "m") or the engine commits to the short prefix
// and treats "onths" as the rest of the input.
var durationRE = regexp.MustCompile(`(?i)^\s*(forever|(\d+)\s*(minutes|minute|mins|min|months|month|hours|hour|days|day|weeks|week|years|year|m|h|d|w|y))\s*(.*)$`)

// parseSnoozeArgs parses the argument tail of a `snooze` command. It
// returns:
//   - duration: a human label ("6 hour(s)", "Forever") used in the
//     confirmation reply.
//   - until: the absolute UTC instant the snooze expires, or nil for
//     "forever".
//   - rest: anything after the duration token — the operator's optional
//     condition expression (e.g. `host = srv-x`).
//
// An unparseable duration returns ErrBadDuration so the caller can post a
// helpful error back to the channel.
func parseSnoozeArgs(args string) (duration string, until *time.Time, rest string, err error) {
	m := durationRE.FindStringSubmatch(strings.TrimSpace(args))
	if m == nil {
		return "", nil, "", ErrBadDuration
	}
	if strings.EqualFold(m[1], "forever") {
		return "Forever", nil, strings.TrimSpace(m[4]), nil
	}
	n, convErr := strconv.Atoi(m[2])
	if convErr != nil || n <= 0 {
		return "", nil, "", ErrBadDuration
	}
	unit := strings.ToLower(m[3])
	now := time.Now()
	var dur time.Duration
	var label string
	// Order the switch arms by specificity — "month" must come before
	// "m" so a typo like "1m" picks minutes, not months. The regex
	// guarantees `unit` is one of the literals listed, so unknown values
	// can't reach the default branch (kept defensively in case the regex
	// gains another unit).
	switch {
	case strings.HasPrefix(unit, "month"):
		// Calendar months vary; approximate as 30 days like the Python plugin.
		dur = time.Duration(n) * 30 * 24 * time.Hour
		label = strconv.Itoa(n) + " month(s)"
	case strings.HasPrefix(unit, "min"), unit == "m":
		dur = time.Duration(n) * time.Minute
		label = strconv.Itoa(n) + " minute(s)"
	case strings.HasPrefix(unit, "hour"), unit == "h":
		dur = time.Duration(n) * time.Hour
		label = strconv.Itoa(n) + " hour(s)"
	case strings.HasPrefix(unit, "day"), unit == "d":
		dur = time.Duration(n) * 24 * time.Hour
		label = strconv.Itoa(n) + " day(s)"
	case strings.HasPrefix(unit, "week"), unit == "w":
		dur = time.Duration(n) * 7 * 24 * time.Hour
		label = strconv.Itoa(n) + " week(s)"
	case strings.HasPrefix(unit, "year"), unit == "y":
		dur = time.Duration(n) * 365 * 24 * time.Hour
		label = strconv.Itoa(n) + " year(s)"
	default:
		return "", nil, "", ErrBadDuration
	}
	t := now.Add(dur)
	return label, &t, strings.TrimSpace(m[4]), nil
}

// ErrBadDuration is returned when parseSnoozeArgs can't make sense of the
// supplied tail. Callers use it to post the Python-equivalent "Invalid
// Snooze filter duration syntax" reply.
var ErrBadDuration = errors.New("teams: invalid snooze duration")

// timeConstraints renders the AdaptiveCard/Snooze time_constraints window
// for a {now, until} pair. Mirrors the Python plugin's payload shape — a
// single "datetime" entry with from/until ISO-8601 strings. Returns nil
// when until is nil (forever snooze).
func timeConstraints(now time.Time, until *time.Time) map[string]any {
	if until == nil {
		return nil
	}
	return map[string]any{
		"datetime": []map[string]any{
			{
				"from":  now.Format(time.RFC3339),
				"until": until.Format(time.RFC3339),
			},
		},
	}
}

// modificationRE matches one leading `field op value` chunk. Supported
// operators (matching Python's bot_parser intent):
//
//   - `=`  → ["SET", field, value]
//   - `+=` → ["ARRAY_APPEND", field, value]
//   - `-=` → ["ARRAY_DELETE", field, value]
//
// The DELETE/`~field` form from Python is intentionally skipped because no
// known deployment uses it from Teams; if it returns, add a separate
// regex line rather than overload this one.
var modificationRE = regexp.MustCompile(`^\s*([a-zA-Z_][a-zA-Z0-9_.]*)\s*(\+=|-=|=)\s*("[^"]*"|'[^']*'|[^\s]+)`)

// parseModifications consumes leading `field op value` chunks from args
// and returns them as the [[op, field, value], …] payload the Snooze
// comment endpoint expects for `type=esc`. Everything left over after
// the last modification chunk is the operator's free-form comment.
//
// Quoted strings are unquoted; bare tokens are treated as strings (no
// number/bool coercion to keep the wire shape predictable).
func parseModifications(args string) (mods [][]any, comment string) {
	cursor := args
parse:
	for {
		m := modificationRE.FindStringSubmatch(cursor)
		if m == nil {
			break
		}
		field, opTok, valTok := m[1], m[2], m[3]
		valTok = unquote(valTok)
		var op string
		switch opTok {
		case "=":
			op = "SET"
		case "+=":
			op = "ARRAY_APPEND"
		case "-=":
			op = "ARRAY_DELETE"
		default:
			// Defensive — the regex grouping above only accepts the three
			// operators we handle, so reaching here means the regex changed
			// without this switch keeping pace. Bail out of the loop rather
			// than appending a modification with an empty op.
			break parse
		}
		mods = append(mods, []any{op, field, valTok})
		cursor = cursor[len(m[0]):]
	}
	return mods, strings.TrimSpace(cursor)
}

// unquote strips a single layer of matching quotes ("…" or '…') from s.
// Used by parseModifications so operators can type `severity = "critical"`
// when they want spaces or special chars in the value.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
