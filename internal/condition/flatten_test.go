package condition

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlattenNested(t *testing.T) {
	a := []any{1, []any{2, []any{3, []any{4, []any{5}}}}}
	out := Flatten(a)
	require.Equal(t, []any{1, 2, 3, 4, 5}, out)
}

func TestFlattenStringIsLeaf(t *testing.T) {
	a := []any{"abc", []any{"de"}}
	out := Flatten(a)
	require.Equal(t, []any{"abc", "de"}, out)
}

func TestFlattenScalar(t *testing.T) {
	out := Flatten(42)
	require.Equal(t, []any{42}, out)
}

func TestFlattenEmpty(t *testing.T) {
	out := Flatten([]any{})
	require.Empty(t, out)
}

func TestFlattenMixed(t *testing.T) {
	a := []any{[]any{"a", "b"}, "c", []any{[]any{"d"}}}
	out := Flatten(a)
	require.Equal(t, []any{"a", "b", "c", "d"}, out)
}
