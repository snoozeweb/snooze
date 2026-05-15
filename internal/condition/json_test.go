package condition

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObjectFormRoundTrip(t *testing.T) {
	in := Cond{
		Op: OpAnd,
		Children: []Cond{
			{Op: OpEq, Field: "host", Value: "foo"},
			{Op: OpExists, Field: "tags"},
		},
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, `{
	  "op":"AND",
	  "children":[
	    {"op":"=","field":"host","value":"foo"},
	    {"op":"EXISTS","field":"tags"}
	  ]
	}`, string(data))
	var out Cond
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, in, out)
}

func TestObjectFormAlwaysTrue(t *testing.T) {
	in := Cond{}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, `{"op":""}`, string(data))
}

func TestListFormUnmarshal(t *testing.T) {
	data := []byte(`["AND", ["=", "host", "foo"], ["EXISTS", "tags"]]`)
	var c Cond
	require.NoError(t, json.Unmarshal(data, &c))
	require.Equal(t, OpAnd, c.Op)
	require.Len(t, c.Children, 2)
	require.Equal(t, OpEq, c.Children[0].Op)
	require.Equal(t, "host", c.Children[0].Field)
	require.Equal(t, "foo", c.Children[0].Value)
	require.Equal(t, OpExists, c.Children[1].Op)
}

func TestListFormRoundTrip(t *testing.T) {
	in := Cond{
		Op: OpAnd,
		Children: []Cond{
			{Op: OpEq, Field: "host", Value: "foo"},
			{Op: OpExists, Field: "tags"},
		},
	}
	data, err := in.MarshalListJSON()
	require.NoError(t, err)
	var out Cond
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, in, out)
}

func TestListFormAlwaysTrue(t *testing.T) {
	data := []byte(`[]`)
	var c Cond
	require.NoError(t, json.Unmarshal(data, &c))
	require.Equal(t, Cond{}, c)
}

func TestListFormNull(t *testing.T) {
	data := []byte(`null`)
	var c Cond
	require.NoError(t, json.Unmarshal(data, &c))
	require.Equal(t, Cond{}, c)
}

func TestListFormIn(t *testing.T) {
	data := []byte(`["IN", [1, 2, 3], "tags"]`)
	var c Cond
	require.NoError(t, json.Unmarshal(data, &c))
	require.Equal(t, OpIn, c.Op)
	require.Equal(t, "tags", c.Field)
	// Note: JSON numbers decode as float64.
	require.Equal(t, []any{float64(1), float64(2), float64(3)}, c.Value)
}

func TestToListIn(t *testing.T) {
	c := Cond{Op: OpIn, Field: "tags", Value: []any{"a", "b"}}
	require.Equal(t, []any{"IN", []any{"a", "b"}, "tags"}, c.ToList())
}

// TestUnmarshalFrontendWireShape pins the wire compatibility the React
// editor relies on. The frontend keys its discriminated-union Condition on
// `type`, spells operators out (ALWAYS_TRUE / EQUALS / NOT_EQUALS / LT / LE
// / GT / GE), uses `args` for AND/OR children and `arg` for the single NOT
// child. Cond.UnmarshalJSON must normalise all of those into the canonical
// Go shape — otherwise every frontend-posted condition degrades to
// AlwaysTrue silently and matches everything.
func TestUnmarshalFrontendWireShape(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Cond
	}{
		{
			name: "type alias for op",
			in:   `{"type":"=","field":"host","value":"foo"}`,
			want: Cond{Op: OpEq, Field: "host", Value: "foo"},
		},
		{
			name: "spelled-out ALWAYS_TRUE",
			in:   `{"type":"ALWAYS_TRUE"}`,
			want: Cond{},
		},
		{
			name: "spelled-out EQUALS",
			in:   `{"type":"EQUALS","field":"severity","value":"critical"}`,
			want: Cond{Op: OpEq, Field: "severity", Value: "critical"},
		},
		{
			name: "spelled-out NOT_EQUALS",
			in:   `{"type":"NOT_EQUALS","field":"state","value":"close"}`,
			want: Cond{Op: OpNeq, Field: "state", Value: "close"},
		},
		{
			name: "spelled-out comparators",
			in: `{"type":"AND","args":[
				{"type":"GT","field":"port","value":1024},
				{"type":"LE","field":"retries","value":3}
			]}`,
			want: Cond{Op: OpAnd, Children: []Cond{
				{Op: OpGt, Field: "port", Value: float64(1024)},
				{Op: OpLte, Field: "retries", Value: float64(3)},
			}},
		},
		{
			name: "MATCHES with anchor (regression: previously degraded to AlwaysTrue)",
			in:   `{"type":"MATCHES","field":"host","value":"^srv-prod-"}`,
			want: Cond{Op: OpMatches, Field: "host", Value: "^srv-prod-"},
		},
		{
			name: "AND with args alias",
			in: `{"type":"AND","args":[
				{"type":"=","field":"a","value":"1"},
				{"type":"=","field":"b","value":"2"}
			]}`,
			want: Cond{Op: OpAnd, Children: []Cond{
				{Op: OpEq, Field: "a", Value: "1"},
				{Op: OpEq, Field: "b", Value: "2"},
			}},
		},
		{
			name: "NOT with single-arg shape",
			in:   `{"type":"NOT","arg":{"type":"EXISTS","field":"tags"}}`,
			want: Cond{Op: OpNot, Children: []Cond{
				{Op: OpExists, Field: "tags"},
			}},
		},
		{
			name: "canonical op key still works",
			in:   `{"op":"=","field":"host","value":"foo"}`,
			want: Cond{Op: OpEq, Field: "host", Value: "foo"},
		},
		{
			name: "op takes precedence over type when both present",
			in:   `{"op":"=","type":"EQUALS","field":"host","value":"foo"}`,
			want: Cond{Op: OpEq, Field: "host", Value: "foo"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got Cond
			require.NoError(t, json.Unmarshal([]byte(c.in), &got))
			require.Equal(t, c.want, got)
		})
	}
}

// TestUnmarshalFrontendShapeIsActuallyEvaluated guards the end-to-end
// behaviour the screenshot tour caught: a MATCHES condition the frontend
// posts must filter records, not match-everything.
func TestUnmarshalFrontendShapeIsActuallyEvaluated(t *testing.T) {
	var c Cond
	require.NoError(t, json.Unmarshal(
		[]byte(`{"type":"MATCHES","field":"host","value":"^srv-prod-"}`),
		&c,
	))
	require.True(t, Match(map[string]any{"host": "srv-prod-1"}, c))
	require.False(t, Match(map[string]any{"host": "srv-stage-1"}, c))
	require.False(t, Match(map[string]any{"host": "noisy-1"}, c))
}

// FuzzCondition round-trips Parse → MarshalListJSON → Unmarshal so any
// crash, drift, or mismatch surfaces. The corpus is seeded with the parser
// test corpus.
func FuzzCondition(f *testing.F) {
	seeds := []string{
		"hello",
		"key = value",
		"key1=value1 AND key2=value2",
		"key1=value1|key2=value2",
		"NOT (key1=value1 AND key2=value2)",
		"mail_queue > 100",
		"port < 1024",
		"myrule in rules",
		"[1, 2, 3] in myarray",
		"message MATCHES \"[aA]lert\"",
		"custom_field?",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	// Round-trip: Parse → MarshalListJSON → Unmarshal → MarshalListJSON.
	// We compare the second-round JSON to the first so the test is
	// idempotent across the lossy int→float JSON conversion.
	f.Fuzz(func(t *testing.T, src string) {
		c, err := Parse(src)
		if err != nil {
			return
		}
		blob1, err := c.MarshalListJSON()
		require.NoError(t, err)
		var back Cond
		require.NoError(t, json.Unmarshal(blob1, &back))
		blob2, err := back.MarshalListJSON()
		require.NoError(t, err)
		require.JSONEq(t, string(blob1), string(blob2))
	})
}
