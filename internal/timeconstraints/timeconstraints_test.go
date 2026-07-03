package timeconstraints

import (
	"encoding/json"
	"testing"
	"time"

	// Embed the zone database so LoadLocation("Europe/Paris") works regardless
	// of the test runner's /usr/share/zoneinfo (mirrors the server binary).
	_ "time/tzdata"

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

// ---------------------------------------------------------------------------
// Named timezone (Group.tz) — recurring families resolve in a named zone
// ---------------------------------------------------------------------------

// gFromJSON unmarshals a whole Group (exercising Group.UnmarshalJSON, incl. tz
// resolution) the way a snooze record arrives on the wire.
func gFromJSON(t *testing.T, payload string) Group {
	t.Helper()
	var g Group
	require.NoError(t, json.Unmarshal([]byte(payload), &g))
	return g
}

// TestGroupTZ_DailyWindowInZone: a bare "09:00–17:00" daily window with
// tz=Europe/Paris is evaluated in Paris wall-clock, not UTC. At 08:00 UTC the
// UTC reading (08:00) is before the window, but the Paris reading (10:00 in
// summer) is inside — so tz flips the result.
func TestGroupTZ_DailyWindowInZone(t *testing.T) {
	paris := gFromJSON(t, `{"tz":"Europe/Paris","time":[{"from":"09:00","until":"17:00"}]}`)
	utc := gFromJSON(t, `{"time":[{"from":"09:00","until":"17:00"}]}`)

	summer := mustParse(t, "2026-07-15T08:00:00Z") // 10:00 Paris (+02:00)
	require.True(t, paris.Match(summer), "10:00 Paris is inside 09:00–17:00")
	require.False(t, utc.Match(summer), "08:00 UTC is before the window when read as UTC")
}

// TestGroupTZ_DailyWindowDSTSafe: the SAME 09:00 wall-clock boundary maps to a
// different UTC instant across DST — 07:00Z in summer (+02:00) vs 08:00Z in
// winter (+01:00) — and the named zone tracks that automatically.
func TestGroupTZ_DailyWindowDSTSafe(t *testing.T) {
	g := gFromJSON(t, `{"tz":"Europe/Paris","time":[{"from":"09:00","until":"17:00"}]}`)

	// Summer: 09:00 Paris == 07:00Z.
	require.True(t, g.Match(mustParse(t, "2026-07-15T07:00:00Z")), "09:00 Paris (summer)")
	require.False(t, g.Match(mustParse(t, "2026-07-15T06:30:00Z")), "08:30 Paris (summer) is before 09:00")

	// Winter: 09:00 Paris == 08:00Z.
	require.True(t, g.Match(mustParse(t, "2026-01-15T08:00:00Z")), "09:00 Paris (winter)")
	require.False(t, g.Match(mustParse(t, "2026-01-15T07:30:00Z")), "08:30 Paris (winter) is before 09:00")
}

// TestGroupTZ_WeekdayInZone: a Monday-only rule with tz=Europe/Paris matches an
// instant that is still Sunday in UTC but already Monday in Paris.
func TestGroupTZ_WeekdayInZone(t *testing.T) {
	g := gFromJSON(t, `{"tz":"Europe/Paris","weekdays":[{"weekdays":[1]}]}`)
	utc := gFromJSON(t, `{"weekdays":[{"weekdays":[1]}]}`)

	// 2026-06-28 is a Sunday; 22:30Z is 00:30 Monday in Paris (+02:00 summer).
	instant := mustParse(t, "2026-06-28T22:30:00Z")
	require.True(t, g.Match(instant), "Monday 00:30 in Paris")
	require.False(t, utc.Match(instant), "still Sunday in UTC")
}

// TestGroupTZ_ExplicitOffsetNotClobbered: a bound the author zoned explicitly
// (09:00+00:00) is left as authored even when the group carries a tz — so it
// stays a 09:00-UTC boundary, not 09:00 Paris.
func TestGroupTZ_ExplicitOffsetNotClobbered(t *testing.T) {
	g := gFromJSON(t, `{"tz":"Europe/Paris","time":[{"from":"09:00+00:00","until":"17:00+00:00"}]}`)
	// 08:00Z is 10:00 Paris. If tz had overridden the explicit bounds it would
	// match; because the bounds stay UTC (09:00Z), 08:00Z is before the window.
	require.False(t, g.Match(mustParse(t, "2026-07-15T08:00:00Z")))
	require.True(t, g.Match(mustParse(t, "2026-07-15T10:00:00Z")), "10:00 UTC is inside 09:00–17:00 UTC")
}

// TestGroupTZ_Invalid: an unknown zone name is a hard unmarshal error, so a
// malformed rule is treated as bad (skipped by the loader / fail-safe by the
// projector) rather than silently mis-evaluated.
func TestGroupTZ_Invalid(t *testing.T) {
	var g Group
	err := json.Unmarshal([]byte(`{"tz":"Mars/Olympus_Mons","time":[{"from":"09:00","until":"17:00"}]}`), &g)
	require.Error(t, err)
}
