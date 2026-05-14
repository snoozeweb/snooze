package condition

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDig(t *testing.T) {
	rec := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "found",
			},
		},
	}
	v, ok := Dig(rec, "a", "b", "c")
	require.True(t, ok)
	require.Equal(t, "found", v)
}

func TestDigMiss(t *testing.T) {
	rec := map[string]any{"a": "1"}
	v, ok := Dig(rec, "a", "b", "c")
	require.False(t, ok)
	require.Nil(t, v)
}

func TestDigList(t *testing.T) {
	rec := map[string]any{"a": []any{"1", "2", "3"}}
	v, ok := Dig(rec, "a", "1")
	require.True(t, ok)
	require.Equal(t, "2", v)
}

func TestDigListOutOfBounds(t *testing.T) {
	rec := map[string]any{"a": []any{"1"}}
	_, ok := Dig(rec, "a", "9")
	require.False(t, ok)
}

func TestDigNestedListAndMap(t *testing.T) {
	rec := map[string]any{
		"a": []any{
			map[string]any{"b": "value"},
		},
	}
	v, ok := Dig(rec, "a", "0", "b")
	require.True(t, ok)
	require.Equal(t, "value", v)
}

func TestDigEmptyPath(t *testing.T) {
	rec := map[string]any{"a": "1"}
	v, ok := Dig(rec)
	require.True(t, ok)
	require.Equal(t, rec, v)
}
