package plugins

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestOrderedFields_PreservesYAMLOrder(t *testing.T) {
	src := []byte(`---
zebra:
    display_name: Zebra
alpha:
    display_name: Alpha
mango:
    display_name: Mango
`)
	var o OrderedFields
	require.NoError(t, yaml.Unmarshal(src, &o))

	require.Equal(t, []string{"zebra", "alpha", "mango"},
		[]string{o.Pairs[0].Key, o.Pairs[1].Key, o.Pairs[2].Key},
		"YAML mapping order must be preserved through unmarshal")

	// JSON marshal must produce the keys in the same order. We assert on
	// the literal byte layout because the whole point of the type is that
	// downstream consumers (the React frontend) iterate in this order.
	b, err := json.Marshal(o)
	require.NoError(t, err)
	js := string(b)
	zPos := strings.Index(js, `"zebra"`)
	aPos := strings.Index(js, `"alpha"`)
	mPos := strings.Index(js, `"mango"`)
	require.True(t, zPos >= 0 && aPos > zPos && mPos > aPos,
		"JSON key order must mirror YAML order, got %s", js)
}

func TestOrderedFields_LookupViaGet(t *testing.T) {
	src := []byte(`---
url:
    display_name: URL
method:
    display_name: Method
`)
	var o OrderedFields
	require.NoError(t, yaml.Unmarshal(src, &o))

	v, ok := o.Get("url")
	require.True(t, ok)
	m, ok := v.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "URL", m["display_name"])

	_, ok = o.Get("missing")
	require.False(t, ok)
}

func TestOrderedFields_EmptyMarshalsToNull(t *testing.T) {
	// An empty (or absent) action_form should JSON-marshal to null so the
	// parent's `omitempty` tag drops it from the wire payload entirely.
	var o OrderedFields
	b, err := json.Marshal(o)
	require.NoError(t, err)
	require.Equal(t, "null", string(b))
}

func TestOrderedFields_RoundTripsThroughJSON(t *testing.T) {
	src := []byte(`---
url:
    display_name: URL
method:
    display_name: Method
`)
	var o OrderedFields
	require.NoError(t, yaml.Unmarshal(src, &o))

	b, err := json.Marshal(o)
	require.NoError(t, err)

	var back OrderedFields
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, []string{"url", "method"},
		[]string{back.Pairs[0].Key, back.Pairs[1].Key})
}

func TestOrderedFields_NullYAMLIsZeroValue(t *testing.T) {
	src := []byte(`---
action_form: ~
`)
	var meta struct {
		ActionForm OrderedFields `yaml:"action_form"`
	}
	require.NoError(t, yaml.Unmarshal(src, &meta))
	require.Equal(t, 0, meta.ActionForm.Len())
}
