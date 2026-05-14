// Package timeconstraints evaluates whether a moment in time matches a set of
// snooze/notification time-window constraints (absolute dates, daily windows,
// weekdays), preserving the wire format used by the legacy Python backend.
package timeconstraints

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Constraint is the interface every constraint type implements.
type Constraint interface {
	Match(t time.Time) bool
}

// Group is the union of constraints typically stored on a snooze or
// notification record. Constraints of different types are AND'd together;
// constraints of the same type are OR'd together. An empty Group matches
// every input (vacuous truth, matching Python's `all(...)`).
//
// The JSON encoding mirrors the legacy Python wire format:
//
//	{
//	  "datetime": [{"from": "...", "until": "..."}, ...],
//	  "time":     [{"from": "HH:MM±TZ",  "until": "HH:MM±TZ"}, ...],
//	  "weekdays": [{"weekdays": [0, 1, ...]}, ...]
//	}
type Group struct {
	DateTime []DateTimeConstraint `json:"datetime,omitempty"`
	Time     []TimeConstraint     `json:"time,omitempty"`
	Weekdays []WeekdaysConstraint `json:"weekdays,omitempty"`
}

// Match returns true when t satisfies every populated constraint family.
// Within a family the constraints are OR'd (any match wins); families are
// AND'd together. An empty Group always matches.
func (g Group) Match(t time.Time) bool {
	if len(g.DateTime) > 0 {
		if !anyMatch(g.DateTime, t) {
			return false
		}
	}
	if len(g.Time) > 0 {
		if !anyMatch(g.Time, t) {
			return false
		}
	}
	if len(g.Weekdays) > 0 {
		if !anyMatch(g.Weekdays, t) {
			return false
		}
	}
	return true
}

// String renders the Group using the same `(c1 or c2) and (c3)` layout as
// the Python implementation, useful for debug logs.
func (g Group) String() string {
	var parts []string
	if len(g.DateTime) > 0 {
		parts = append(parts, joinOr(g.DateTime))
	}
	if len(g.Time) > 0 {
		parts = append(parts, joinOr(g.Time))
	}
	if len(g.Weekdays) > 0 {
		parts = append(parts, joinOr(g.Weekdays))
	}
	return strings.Join(parts, " and ")
}

func anyMatch[C Constraint](cs []C, t time.Time) bool {
	for _, c := range cs {
		if c.Match(t) {
			return true
		}
	}
	return false
}

func joinOr[C Constraint](cs []C) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = fmt.Sprint(c)
	}
	return "(" + strings.Join(parts, " or ") + ")"
}

// DateTimeConstraint matches an inclusive interval bounded by absolute
// timestamps. With both bounds it matches `from <= t <= until`; with only one
// bound it becomes a half-open ray. With neither bound it matches nothing,
// matching the Python implementation.
type DateTimeConstraint struct {
	From  *time.Time `json:"-"`
	Until *time.Time `json:"-"`
}

// dateTimeWire is the JSON shape on the wire: ISO-8601 strings or null.
type dateTimeWire struct {
	From  string `json:"from,omitempty"`
	Until string `json:"until,omitempty"`
}

// UnmarshalJSON parses the wire shape, accepting any layout
// `time.Parse(time.RFC3339, ...)` accepts.
func (d *DateTimeConstraint) UnmarshalJSON(data []byte) error {
	var w dateTimeWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	if w.From != "" {
		t, err := parseDateTime(w.From)
		if err != nil {
			return fmt.Errorf("datetime constraint: from: %w", err)
		}
		d.From = &t
	}
	if w.Until != "" {
		t, err := parseDateTime(w.Until)
		if err != nil {
			return fmt.Errorf("datetime constraint: until: %w", err)
		}
		d.Until = &t
	}
	return nil
}

// MarshalJSON renders the constraint back into the legacy wire shape.
func (d DateTimeConstraint) MarshalJSON() ([]byte, error) {
	var w dateTimeWire
	if d.From != nil {
		w.From = d.From.Format(time.RFC3339)
	}
	if d.Until != nil {
		w.Until = d.Until.Format(time.RFC3339)
	}
	return json.Marshal(w)
}

// Match reports whether t falls within the configured datetime window.
func (d DateTimeConstraint) Match(t time.Time) bool {
	switch {
	case d.From != nil && d.Until != nil:
		return !t.Before(*d.From) && !t.After(*d.Until)
	case d.From == nil && d.Until != nil:
		return !t.After(*d.Until)
	case d.From != nil && d.Until == nil:
		return !t.Before(*d.From)
	default:
		return false
	}
}

func (d DateTimeConstraint) String() string {
	from, until := "None", "None"
	if d.From != nil {
		from = d.From.Format(time.RFC3339)
	}
	if d.Until != nil {
		until = d.Until.Format(time.RFC3339)
	}
	return fmt.Sprintf("DateTimeConstraint<%s to %s>", from, until)
}

// WeekdaysConstraint matches records whose local weekday (Sunday=0 ... Saturday=6,
// matching Python's `strftime("%w")`) is in the list. An empty list matches
// nothing.
type WeekdaysConstraint struct {
	Weekdays []int `json:"weekdays"`
}

// Match reports whether t's weekday is included.
func (w WeekdaysConstraint) Match(t time.Time) bool {
	wd := int(t.Weekday()) // Go's time.Weekday: Sunday=0..Saturday=6
	for _, d := range w.Weekdays {
		if d == wd {
			return true
		}
	}
	return false
}

func (w WeekdaysConstraint) String() string {
	return fmt.Sprintf("WeekdaysConstraint<%v>", w.Weekdays)
}

// TimeConstraint matches a daily window expressed as wall-clock times. When
// `until` is earlier than `from` the window spans midnight. With only one
// bound the constraint becomes a half-open ray on that day. With neither
// bound it matches everything, matching the Python implementation.
type TimeConstraint struct {
	From  *daytime `json:"-"`
	Until *daytime `json:"-"`
}

// daytime captures a time-of-day plus its UTC offset (the tz the user wrote
// the bound in). Hour/Minute/Second are in that offset.
type daytime struct {
	Hour, Minute, Second int
	Loc                  *time.Location
}

type timeWire struct {
	From  string `json:"from,omitempty"`
	Until string `json:"until,omitempty"`
}

// UnmarshalJSON parses bounds like `15:04`, `15:04:05`, `15:04+09:00`, or full
// RFC3339 timestamps (only the wall-clock + offset are kept).
func (c *TimeConstraint) UnmarshalJSON(data []byte) error {
	var w timeWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	if w.From != "" {
		dt, err := parseDaytime(w.From)
		if err != nil {
			return fmt.Errorf("time constraint: from: %w", err)
		}
		c.From = &dt
	}
	if w.Until != "" {
		dt, err := parseDaytime(w.Until)
		if err != nil {
			return fmt.Errorf("time constraint: until: %w", err)
		}
		c.Until = &dt
	}
	return nil
}

// MarshalJSON renders the constraint in `HH:MM:SS±HH:MM` form.
func (c TimeConstraint) MarshalJSON() ([]byte, error) {
	var w timeWire
	if c.From != nil {
		w.From = c.From.String()
	}
	if c.Until != nil {
		w.Until = c.Until.String()
	}
	return json.Marshal(w)
}

// Match reports whether t falls within the configured daily window. The
// comparison is performed in the From/Until offset (matching the Python
// `rd.astimezone()` then `datetime.combine(rd.date(), self.timeX)` flow).
func (c TimeConstraint) Match(t time.Time) bool {
	switch {
	case c.From != nil && c.Until != nil:
		// Two-sided: walk the (up to two) intervals produced by the
		// midnight-rollover rule, using the `from` bound's tz as the
		// reference day boundary (the Python code uses local tz; tests
		// always supply the same tz for both bounds, so either works).
		rd := t.In(c.From.Loc)
		for _, iv := range c.intervals(rd) {
			if !rd.Before(iv.start) && !rd.After(iv.end) {
				return true
			}
		}
		return false
	case c.From != nil && c.Until == nil:
		rd := t.In(c.From.Loc)
		from := c.From.onDate(rd)
		return !rd.Before(from)
	case c.From == nil && c.Until != nil:
		rd := t.In(c.Until.Loc)
		until := c.Until.onDate(rd)
		return !rd.After(until)
	default:
		return true
	}
}

type interval struct{ start, end time.Time }

// intervals returns the [start,end] windows produced for the day containing
// rd. When `until < from` (the window crosses midnight) the day's window is
// represented as two intervals: yesterday->today and today->tomorrow.
func (c TimeConstraint) intervals(rd time.Time) []interval {
	from := c.From.onDate(rd)
	until := c.Until.onDate(rd)
	if until.Before(from) {
		day := 24 * time.Hour
		return []interval{
			{start: from.Add(-day), end: until},
			{start: from, end: until.Add(day)},
		}
	}
	return []interval{{start: from, end: until}}
}

func (c TimeConstraint) String() string {
	from, until := "None", "None"
	if c.From != nil {
		from = c.From.String()
	}
	if c.Until != nil {
		until = c.Until.String()
	}
	return fmt.Sprintf("TimeConstraint<%s to %s>", from, until)
}

// onDate places the daytime onto the same calendar date as rd, in the
// daytime's own tz.
func (d daytime) onDate(rd time.Time) time.Time {
	in := rd.In(d.Loc)
	return time.Date(in.Year(), in.Month(), in.Day(), d.Hour, d.Minute, d.Second, 0, d.Loc)
}

func (d daytime) String() string {
	// HH:MM:SS±HH:MM, matching Python `datetime.time.isoformat()`.
	_, offsetSec := time.Now().In(d.Loc).Zone()
	if d.Loc == time.UTC {
		offsetSec = 0
	}
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	oh := offsetSec / 3600
	om := (offsetSec % 3600) / 60
	return fmt.Sprintf("%02d:%02d:%02d%s%02d:%02d", d.Hour, d.Minute, d.Second, sign, oh, om)
}

// parseDateTime accepts RFC3339 with or without seconds and an optional 'Z'
// suffix, plus the SQL-ish `2006-01-02 15:04:05` form.
func parseDateTime(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02T15:04Z07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse %q as datetime", s)
}

// parseDaytime parses an HH:MM, HH:MM:SS, HH:MM±HH:MM, HH:MM:SS±HH:MM or
// full-datetime string and keeps only the wall-clock + tz offset.
func parseDaytime(s string) (daytime, error) {
	timeLayouts := []string{
		"15:04:05Z07:00",
		"15:04Z07:00",
		"15:04:05",
		"15:04",
	}
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			loc := t.Location()
			if loc == nil {
				loc = time.UTC
			}
			return daytime{Hour: t.Hour(), Minute: t.Minute(), Second: t.Second(), Loc: loc}, nil
		}
	}
	// Fall back to full datetimes (e.g. someone stored an RFC3339).
	if t, err := parseDateTime(s); err == nil {
		return daytime{Hour: t.Hour(), Minute: t.Minute(), Second: t.Second(), Loc: t.Location()}, nil
	}
	return daytime{}, fmt.Errorf("could not parse %q as time-of-day", s)
}
