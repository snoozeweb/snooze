package schema

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDuration_UnmarshalText(t *testing.T) {
	cases := map[string]time.Duration{
		"":         0,
		"5m":       5 * time.Minute,
		"172800s":  48 * time.Hour,
		"172800":   48 * time.Hour, // bare seconds
		"172800.0": 48 * time.Hour,
		"1h30m":    time.Hour + 30*time.Minute,
	}
	for in, want := range cases {
		var d Duration
		require.NoError(t, d.UnmarshalText([]byte(in)), "input %q", in)
		require.Equal(t, want, d.AsDuration(), "input %q", in)
	}
}

func TestDuration_UnmarshalText_BadInput(t *testing.T) {
	var d Duration
	require.Error(t, d.UnmarshalText([]byte("not-a-duration")))
}

func TestDuration_JSONRoundTrip(t *testing.T) {
	d := Duration(5 * time.Minute)
	out, err := json.Marshal(d)
	require.NoError(t, err)
	require.JSONEq(t, `"5m0s"`, string(out))

	var back Duration
	require.NoError(t, json.Unmarshal(out, &back))
	require.Equal(t, d, back)

	require.NoError(t, json.Unmarshal([]byte("300"), &back))
	require.Equal(t, 5*time.Minute, back.AsDuration())
}
