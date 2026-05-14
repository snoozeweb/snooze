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
