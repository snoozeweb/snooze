package teams

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseSnoozeArgs(t *testing.T) {
	type want struct {
		label    string
		isFinite bool
		rest     string
	}
	cases := []struct {
		in   string
		want want
	}{
		{"6h", want{"6 hour(s)", true, ""}},
		{"30m host = srv-x", want{"30 minute(s)", true, "host = srv-x"}},
		{"2d  ", want{"2 day(s)", true, ""}},
		{"1week", want{"1 week(s)", true, ""}},
		{"3 months", want{"3 month(s)", true, ""}},
		{"forever host = srv-x", want{"Forever", false, "host = srv-x"}},
		{"FOREVER", want{"Forever", false, ""}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			label, until, rest, err := parseSnoozeArgs(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want.label, label)
			require.Equal(t, tc.want.rest, rest)
			require.Equal(t, tc.want.isFinite, until != nil)
			if until != nil {
				require.True(t, until.After(time.Now()),
					"finite snooze must expire in the future")
			}
		})
	}
}

func TestParseSnoozeArgs_Invalid(t *testing.T) {
	for _, in := range []string{"", "garbage", "1xyz", "h", "0h", "-1d"} {
		t.Run(in, func(t *testing.T) {
			_, _, _, err := parseSnoozeArgs(in)
			require.ErrorIs(t, err, ErrBadDuration)
		})
	}
}

func TestParseModifications(t *testing.T) {
	cases := []struct {
		in         string
		wantMods   [][]any
		wantComment string
	}{
		{
			"severity = critical Please check",
			[][]any{{"SET", "severity", "critical"}},
			"Please check",
		},
		{
			`tags += "high-priority"`,
			[][]any{{"ARRAY_APPEND", "tags", "high-priority"}},
			"",
		},
		{
			`a = 1 b += two c -= 3 extra trailing comment`,
			[][]any{
				{"SET", "a", "1"},
				{"ARRAY_APPEND", "b", "two"},
				{"ARRAY_DELETE", "c", "3"},
			},
			"extra trailing comment",
		},
		{
			"only a comment with no mods",
			nil,
			"only a comment with no mods",
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			gotMods, gotComment := parseModifications(tc.in)
			require.Equal(t, tc.wantMods, gotMods)
			require.Equal(t, tc.wantComment, gotComment)
		})
	}
}

func TestTimeConstraints_ForeverIsNil(t *testing.T) {
	require.Nil(t, timeConstraints(time.Now(), nil))
}

func TestTimeConstraints_FiniteCarriesISO(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	until := now.Add(6 * time.Hour)
	tc := timeConstraints(now, &until)
	require.NotNil(t, tc)
	dt := tc["datetime"].([]map[string]any)
	require.Len(t, dt, 1)
	require.Equal(t, "2026-05-25T12:00:00Z", dt[0]["from"])
	require.Equal(t, "2026-05-25T18:00:00Z", dt[0]["until"])
}
