package timeconstraints

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// jst is the +09:00 fixed offset used throughout the Python test suite.
var jst = time.FixedZone("JST", 9*60*60)

// mustParse parses an RFC3339 record date and fails the test if it doesn't.
func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return ts
}

// fromJSON builds a constraint of the requested type via UnmarshalJSON,
// matching how snooze records arrive on the wire.
func dtFromJSON(t *testing.T, payload string) DateTimeConstraint {
	t.Helper()
	var c DateTimeConstraint
	require.NoError(t, json.Unmarshal([]byte(payload), &c))
	return c
}

func tcFromJSON(t *testing.T, payload string) TimeConstraint {
	t.Helper()
	var c TimeConstraint
	require.NoError(t, json.Unmarshal([]byte(payload), &c))
	return c
}

// ---------------------------------------------------------------------------
// MultiConstraint / Group
// ---------------------------------------------------------------------------

func TestGroupMatchTrue(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	g := Group{
		DateTime: []DateTimeConstraint{dtFromJSON(t, `{"until":"2021-07-01T14:30:00+09:00"}`)},
		Weekdays: []WeekdaysConstraint{{Weekdays: []int{1, 2, 3, 4}}},
		Time:     []TimeConstraint{tcFromJSON(t, `{"from":"11:00+09:00","until":"15:00+09:00"}`)},
	}
	require.True(t, g.Match(rd))
}

func TestGroupMatchFalse(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	g := Group{
		DateTime: []DateTimeConstraint{dtFromJSON(t, `{"until":"2021-07-01T14:30:00+09:00"}`)},
		Weekdays: []WeekdaysConstraint{{Weekdays: []int{6, 7}}},
	}
	require.False(t, g.Match(rd))
}

func TestGroupMatchAnySameType(t *testing.T) {
	rd := mustParse(t, "2021-07-01T23:00:00+09:00")
	g := Group{
		Time: []TimeConstraint{
			tcFromJSON(t, `{"from":"00:00+09:00","until":"02:00+09:00"}`),
			tcFromJSON(t, `{"from":"22:00+09:00","until":"23:59+09:00"}`),
		},
	}
	require.True(t, g.Match(rd))
}

// ---------------------------------------------------------------------------
// DateTimeConstraint
// ---------------------------------------------------------------------------

func TestDateTimeUntilTrue(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	c := dtFromJSON(t, `{"until":"2021-07-01T14:30:00+09:00"}`)
	require.True(t, c.Match(rd))
}

func TestDateTimeUntilFalse(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	c := dtFromJSON(t, `{"until":"2021-07-01T11:30:00+09:00"}`)
	require.False(t, c.Match(rd))
}

func TestDateTimeFromTrue(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	c := dtFromJSON(t, `{"from":"2021-07-01T10:30:00+09:00"}`)
	require.True(t, c.Match(rd))
}

func TestDateTimeFromFalse(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	c := dtFromJSON(t, `{"from":"2021-07-01T12:30:00+09:00"}`)
	require.False(t, c.Match(rd))
}

// ---------------------------------------------------------------------------
// WeekdaysConstraint
// ---------------------------------------------------------------------------

func TestWeekdaysTrue(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00") // Thursday = 4
	c := WeekdaysConstraint{Weekdays: []int{4}}
	require.True(t, c.Match(rd))
}

func TestWeekdaysFalse(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00") // Thursday = 4
	c := WeekdaysConstraint{Weekdays: []int{6, 7}}
	require.False(t, c.Match(rd))
}

// ---------------------------------------------------------------------------
// TimeConstraint
// ---------------------------------------------------------------------------

func TestTimeFromTrue(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	require.True(t, tcFromJSON(t, `{"from":"10:00+09:00"}`).Match(rd))
	require.True(t, tcFromJSON(t, `{"from":"12:00+09:00"}`).Match(rd))
}

func TestTimeFromFalse(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	require.False(t, tcFromJSON(t, `{"from":"14:00+09:00"}`).Match(rd))
}

func TestTimeUntilTrue(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	require.True(t, tcFromJSON(t, `{"until":"14:00+09:00"}`).Match(rd))
	require.True(t, tcFromJSON(t, `{"until":"12:00+09:00"}`).Match(rd))
}

func TestTimeUntilFalse(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	require.False(t, tcFromJSON(t, `{"until":"10:00+09:00"}`).Match(rd))
}

func TestTimeRangeTrue(t *testing.T) {
	rd := mustParse(t, "2021-07-01T12:00:00+09:00")
	require.True(t, tcFromJSON(t, `{"from":"10:00+09:00","until":"14:00+09:00"}`).Match(rd))
}

func TestTimeRangeFalse(t *testing.T) {
	rd := mustParse(t, "2021-07-01T08:00:00+09:00")
	require.False(t, tcFromJSON(t, `{"from":"10:00+09:00","until":"14:00+09:00"}`).Match(rd))
}

func TestTimeOverMidnight(t *testing.T) {
	rd := mustParse(t, "2021-07-01T01:00:00+09:00")
	require.True(t, tcFromJSON(t, `{"from":"23:00+09:00","until":"02:00+09:00"}`).Match(rd))
}

func TestTimeOverMidnightMiss(t *testing.T) {
	rd := mustParse(t, "2021-07-01T03:00:00+09:00")
	require.False(t, tcFromJSON(t, `{"from":"23:00+09:00","until":"02:00+09:00"}`).Match(rd))
}

// ---------------------------------------------------------------------------
// Misc — empty Group, empty DateTime, empty TimeConstraint
// ---------------------------------------------------------------------------

func TestEmptyGroupAlwaysMatches(t *testing.T) {
	require.True(t, Group{}.Match(time.Now()))
}

func TestEmptyDateTimeNeverMatches(t *testing.T) {
	require.False(t, DateTimeConstraint{}.Match(time.Now().In(jst)))
}

func TestEmptyTimeConstraintAlwaysMatches(t *testing.T) {
	require.True(t, TimeConstraint{}.Match(time.Now().In(jst)))
}
