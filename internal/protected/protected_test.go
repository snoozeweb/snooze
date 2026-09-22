package protected

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsProtectedRecursive(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"agentic", true},
		{"agentic.root_cause", true},
		{"agentic.root_cause.summary", true},
		{"agentic.remediation_plan.steps", true},
		{" agentic ", true},
		{"agentics", false},
		{"my_agentic", false},
		{"host", false},
		{"", false},
		{".", false},
	}
	for _, c := range cases {
		require.Equalf(t, c.want, IsProtected(c.path), "IsProtected(%q)", c.path)
	}
}

func TestFieldsIncludesAgentic(t *testing.T) {
	require.Contains(t, Fields(), AgenticField)
}

func TestRegisterIgnoresBlank(t *testing.T) {
	before := len(Fields())
	Register("   ")
	require.Len(t, Fields(), before)
}

func TestNamesAndStrip(t *testing.T) {
	doc := map[string]any{
		"host":    "srv-1",
		"agentic": map[string]any{"root_cause": map[string]any{"summary": "x"}},
		"message": "boom",
	}
	require.Equal(t, []string{"agentic"}, Names(doc))

	removed := Strip(doc)
	require.Equal(t, []string{"agentic"}, removed)
	require.NotContains(t, doc, "agentic")
	// Non-protected fields survive untouched.
	require.Equal(t, "srv-1", doc["host"])
	require.Equal(t, "boom", doc["message"])

	// Stripping a clean document is a no-op reporting nothing.
	require.Nil(t, Strip(doc))
}

func TestStripNilAndEmpty(t *testing.T) {
	require.Nil(t, Strip(nil))
	require.Nil(t, Names(nil))
	require.Nil(t, Names(map[string]any{}))
}

func TestCarryForwardsProtectedOnly(t *testing.T) {
	stored := map[string]any{
		"host":    "old-host",
		"agentic": map[string]any{"root_cause": map[string]any{"summary": "kept"}},
	}
	replacement := map[string]any{"host": "new-host"}

	Carry(replacement, stored)

	require.Equal(t, "new-host", replacement["host"], "non-protected fields must not be carried")
	agentic, ok := replacement["agentic"].(map[string]any)
	require.True(t, ok, "protected field carried forward")
	require.Equal(t, map[string]any{"summary": "kept"}, agentic["root_cause"])
}

func TestCarryOverwritesClientSuppliedProtectedValue(t *testing.T) {
	stored := map[string]any{"agentic": map[string]any{"root_cause": "stored"}}
	replacement := map[string]any{"agentic": map[string]any{"root_cause": "forged"}}

	Carry(replacement, stored)

	require.Equal(t, map[string]any{"root_cause": "stored"}, replacement["agentic"])
}

func TestCarryNilDestination(t *testing.T) {
	require.NotPanics(t, func() { Carry(nil, map[string]any{"agentic": 1}) })
}

func TestSameNormalisesThroughJSON(t *testing.T) {
	cases := []struct {
		name string
		a, b any
		want bool
	}{
		{"identical maps", map[string]any{"x": "y"}, map[string]any{"x": "y"}, true},
		{"int64 vs float64", map[string]any{"n": int64(3)}, map[string]any{"n": float64(3)}, true},
		{"int vs json float", 7, 7.0, true},
		{"key order is irrelevant",
			map[string]any{"a": 1, "b": 2}, map[string]any{"b": 2, "a": 1}, true},
		{"nested equality",
			map[string]any{"r": map[string]any{"s": []any{"a", "b"}}},
			map[string]any{"r": map[string]any{"s": []any{"a", "b"}}}, true},
		{"both nil", nil, nil, true},
		{"different values", map[string]any{"x": "y"}, map[string]any{"x": "z"}, false},
		{"extra key", map[string]any{"x": "y"}, map[string]any{"x": "y", "z": 1}, false},
		{"slice order matters", []any{"a", "b"}, []any{"b", "a"}, false},
		{"nil vs empty map", nil, map[string]any{}, false},
		{"string vs number", "3", 3, false},
	}
	for _, c := range cases {
		require.Equalf(t, c.want, Same(c.a, c.b), "Same(%v, %v) [%s]", c.a, c.b, c.name)
	}
}

// An unmarshallable value is never equal to anything: the guard on top of
// Same must refuse rather than wave the write through.
func TestSameRefusesUnmarshallableValues(t *testing.T) {
	ch := make(chan int)
	require.False(t, Same(ch, ch))
	require.False(t, Same(map[string]any{"f": func() {}}, map[string]any{"f": func() {}}))
	require.False(t, Same("ok", ch))
}
