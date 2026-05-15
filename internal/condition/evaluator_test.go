package condition

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func mustFromList(t *testing.T, v any) Cond {
	t.Helper()
	c, err := FromList(v)
	require.NoError(t, err)
	return c
}

// --- Equals ---

func TestEquals_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"=", "a", "0"})
	require.Equal(t, OpEq, c.Op)
}

func TestEquals_MatchSimple(t *testing.T) {
	rec := map[string]any{"a": "1", "b": "2"}
	require.True(t, Match(rec, mustFromList(t, []any{"=", "a", "1"})))
}

func TestEquals_MatchNestedDict(t *testing.T) {
	rec := map[string]any{"a": "1", "b": map[string]any{"c": "1"}}
	require.True(t, Match(rec, mustFromList(t, []any{"=", "b.c", "1"})))
}

func TestEquals_MissNestedDict(t *testing.T) {
	rec := map[string]any{"a": "1", "b": map[string]any{"c": int64(1)}}
	require.False(t, Match(rec, mustFromList(t, []any{"=", "a.c", "2"})))
}

func TestEquals_MatchNestedList(t *testing.T) {
	rec := map[string]any{"a": []any{"1", "2"}}
	require.True(t, Match(rec, mustFromList(t, []any{"=", "a.1", "2"})))
}

func TestEquals_EdgeNoField(t *testing.T) {
	_, err := FromList([]any{"=", nil, "1"})
	require.Error(t, err)
}

func TestEquals_EdgeNoValue(t *testing.T) {
	rec := map[string]any{"a": "1"}
	require.False(t, Match(rec, mustFromList(t, []any{"=", "a", nil})))
}

// --- NotEquals ---

func TestNotEquals_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"!=", "a", "0"})
	require.Equal(t, OpNeq, c.Op)
}

func TestNotEquals_Miss(t *testing.T) {
	rec := map[string]any{"a": "1", "b": "2"}
	require.False(t, Match(rec, mustFromList(t, []any{"!=", "a", "1"})))
}

// --- GreaterThan ---

func TestGreaterThan_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{">", "a", "0"})
	require.Equal(t, OpGt, c.Op)
}

func TestGreaterThan_MatchTwoFloat(t *testing.T) {
	rec := map[string]any{"a": 1.0, "b": 2.0}
	require.True(t, Match(rec, mustFromList(t, []any{">", "b", 1.0})))
}

func TestGreaterThan_MatchStringAndInteger(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(2)}
	// Python: 2 > "1" raises TypeError → False
	require.False(t, Match(rec, mustFromList(t, []any{">", "b", "1"})))
}

// --- LowerThan ---

func TestLowerThan_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"<", "a", "0"})
	require.Equal(t, OpLt, c.Op)
}

func TestLowerThan_MatchTwoString(t *testing.T) {
	rec := map[string]any{"var": "aa"}
	require.True(t, Match(rec, mustFromList(t, []any{"<", "var", "ab"})))
}

// --- And ---

func TestAnd_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"AND", []any{"=", "a", int64(1)}, []any{"=", "b", int64(2)}})
	require.Equal(t, OpAnd, c.Op)
}

func TestAnd_Matches(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(2)}
	require.True(t, Match(rec, mustFromList(t,
		[]any{"AND", []any{"=", "a", int64(1)}, []any{"=", "b", int64(2)}})))
}

func TestAnd_Misses(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(2)}
	require.False(t, Match(rec, mustFromList(t,
		[]any{"AND", []any{"=", "a", int64(1)}, []any{"=", "b", int64(3)}})))
}

func TestAnd_Multiple(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(2), "c": int64(3)}
	require.True(t, Match(rec, mustFromList(t,
		[]any{"AND", []any{"=", "a", int64(1)}, []any{"=", "b", int64(2)}, []any{"=", "c", int64(3)}})))
}

func TestAnd_Nested(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(2), "c": int64(3)}
	require.True(t, Match(rec, mustFromList(t,
		[]any{"AND", []any{"=", "a", int64(1)},
			[]any{"AND", []any{"=", "b", int64(2)}, []any{"=", "c", int64(3)}}})))
}

func TestAnd_NestedMiss(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(2), "c": int64(3)}
	require.False(t, Match(rec, mustFromList(t,
		[]any{"AND", []any{"=", "a", int64(1)},
			[]any{"AND", []any{"=", "b", int64(2)}, []any{"=", "c", int64(4)}}})))
}

// --- Or ---

func TestOr_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"OR", []any{"=", "a", int64(1)}, []any{"=", "b", int64(2)}})
	require.Equal(t, OpOr, c.Op)
}

func TestOr_Match(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(3)}
	require.True(t, Match(rec, mustFromList(t,
		[]any{"OR", []any{"=", "a", int64(1)}, []any{"=", "b", int64(2)}})))
}

func TestOr_Multiple(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(2), "c": int64(3)}
	require.True(t, Match(rec, mustFromList(t,
		[]any{"OR", []any{"=", "a", int64(6)}, []any{"=", "b", int64(4)}, []any{"=", "c", int64(3)}})))
}

// --- Not ---

func TestNot_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"NOT", []any{"=", "a", int64(1)}})
	require.Equal(t, OpNot, c.Op)
}

func TestNot_Match(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(3)}
	require.True(t, Match(rec, mustFromList(t,
		[]any{"NOT", []any{"=", "a", int64(2)}})))
}

func TestNot_Miss(t *testing.T) {
	rec := map[string]any{"a": int64(1), "b": int64(3)}
	require.False(t, Match(rec, mustFromList(t,
		[]any{"NOT", []any{"=", "a", int64(1)}})))
}

// --- Matches ---

func TestMatches_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"MATCHES", "a", "string"})
	require.Equal(t, OpMatches, c.Op)
}

func TestMatches_Match(t *testing.T) {
	rec := map[string]any{"a": "__pattern__"}
	require.True(t, Match(rec, mustFromList(t, []any{"MATCHES", "a", "pattern"})))
}

func TestMatches_MatchSugar(t *testing.T) {
	rec := map[string]any{"a": "__pattern__"}
	require.True(t, Match(rec, mustFromList(t, []any{"MATCHES", "a", "/pattern/"})))
}

// --- Exists ---

func TestExists_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"EXISTS", "a"})
	require.Equal(t, OpExists, c.Op)
}

func TestExists_Match(t *testing.T) {
	rec := map[string]any{"a": "1"}
	require.True(t, Match(rec, mustFromList(t, []any{"EXISTS", "a"})))
}

func TestExists_Miss(t *testing.T) {
	rec := map[string]any{"a": "1"}
	require.False(t, Match(rec, mustFromList(t, []any{"EXISTS", "b"})))
}

// --- Contains ---

func TestContains_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"CONTAINS", "a", "substring"})
	require.Equal(t, OpContains, c.Op)
}

func TestContains_MatchSearchInString(t *testing.T) {
	rec := map[string]any{"a": []any{"0", []any{"11", "2", int64(9)}, "3"}}
	require.True(t, Match(rec, mustFromList(t, []any{"CONTAINS", "a", "1"})))
	require.True(t, Match(rec, mustFromList(t, []any{"CONTAINS", "a", int64(9)})))
}

func TestContains_MatchIncompleteList(t *testing.T) {
	rec := map[string]any{"a": "11", "b": int64(9)}
	require.True(t, Match(rec, mustFromList(t, []any{"CONTAINS", "a", []any{"0", "1"}})))
	require.True(t, Match(rec, mustFromList(t, []any{"CONTAINS", "b", []any{"0", int64(9)}})))
}

// --- In ---

func TestIn_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"IN", []any{"1", "2", "3"}, "a"})
	require.Equal(t, OpIn, c.Op)
}

func TestIn_MatchList(t *testing.T) {
	rec := map[string]any{"a": "1", "b": int64(1)}
	require.True(t, Match(rec, mustFromList(t, []any{"IN", []any{"1", "5"}, "a"})))
	require.True(t, Match(rec, mustFromList(t, []any{"IN", []any{int64(1), int64(5)}, "b"})))
}

func TestIn_MissList(t *testing.T) {
	rec := map[string]any{"a": []any{"0", []any{"11", "2"}, "3"}}
	require.False(t, Match(rec, mustFromList(t, []any{"IN", []any{"1", "5"}, "a"})))
}

func TestIn_MatchCondition(t *testing.T) {
	rec := map[string]any{"a": []any{
		map[string]any{"b": "0"},
		map[string]any{"c": "0"},
	}}
	require.True(t, Match(rec, mustFromList(t, []any{"IN", []any{"=", "c", "0"}, "a"})))
}

func TestIn_MissCondition(t *testing.T) {
	rec := map[string]any{"a": []any{
		map[string]any{"b": "0"},
		map[string]any{"c": "0"},
	}}
	require.False(t, Match(rec, mustFromList(t, []any{"IN", []any{"=", "d", "0"}, "a"})))
}

func TestIn_MatchInteger(t *testing.T) {
	rec := map[string]any{"a": []any{
		map[string]any{"b": int64(0)},
		map[string]any{"c": "0"},
	}}
	require.True(t, Match(rec, mustFromList(t, []any{"IN", []any{"=", "b", int64(0)}, "a"})))
}

// --- Search ---

func TestSearch_GetCondition(t *testing.T) {
	c := mustFromList(t, []any{"SEARCH", "string"})
	require.Equal(t, OpSearch, c.Op)
}

func TestSearch_MatchIncompleteField(t *testing.T) {
	rec := map[string]any{"myfield": []any{
		map[string]any{"b": "mystring"},
		map[string]any{"mysearch": "0"},
	}}
	require.True(t, Match(rec, mustFromList(t, []any{"SEARCH", "field"})))
}

func TestSearch_MatchNestedValue(t *testing.T) {
	rec := map[string]any{"myfield": []any{
		map[string]any{"b": "mystring"},
		map[string]any{"mysearch": "0"},
	}}
	require.True(t, Match(rec, mustFromList(t, []any{"SEARCH", "string"})))
}

func TestSearch_MatchIncompleteNestedField(t *testing.T) {
	rec := map[string]any{"myfield": []any{
		map[string]any{"b": "mystring"},
		map[string]any{"mysearch": "0"},
	}}
	require.True(t, Match(rec, mustFromList(t, []any{"SEARCH", "search"})))
}

func TestSearch_Miss(t *testing.T) {
	rec := map[string]any{"myfield": []any{
		map[string]any{"b": "mystring"},
		map[string]any{"mysearch": "0"},
	}}
	require.False(t, Match(rec, mustFromList(t, []any{"SEARCH", "value"})))
}

// --- AlwaysTrue ---

func TestAlwaysTrue_GetCondition(t *testing.T) {
	cases := []any{
		[]any{},
		[]any{""},
		[]any{nil},
		nil,
	}
	for _, src := range cases {
		c, err := FromList(src)
		require.NoError(t, err)
		require.Equal(t, OpAlwaysTrue, c.Op)
	}
}

func TestAlwaysTrue_Match(t *testing.T) {
	records := []map[string]any{
		{"a": int64(1)},
		{"b": "2"},
		{},
	}
	c := Cond{}
	for _, rec := range records {
		require.True(t, Match(rec, c))
	}
}

// --- Deeply nested mixed trees ---
//
// The two tests above (TestAnd_Nested, TestAnd_NestedMiss) pin 2-level
// nesting. These pin 3+ levels and mixed AND/OR/NOT, which is the shape
// the ConditionEditor builds when users click "Add group" inside an
// existing group.

func TestEval_DeepMixedTree_Match(t *testing.T) {
	// AND(
	//   OR(
	//     =(host, srv-prod-1),
	//     NOT(EXISTS(maintenance)),
	//   ),
	//   AND(
	//     MATCHES(message, "CPU"),
	//     OR(=(severity, "critical"), =(severity, "error")),
	//   ),
	// )
	c := mustFromList(t, []any{
		"AND",
		[]any{"OR",
			[]any{"=", "host", "srv-prod-1"},
			[]any{"NOT", []any{"EXISTS", "maintenance"}},
		},
		[]any{"AND",
			[]any{"MATCHES", "message", "CPU"},
			[]any{"OR",
				[]any{"=", "severity", "critical"},
				[]any{"=", "severity", "error"},
			},
		},
	})
	require.True(t, Match(map[string]any{
		"host":     "srv-prod-1",
		"message":  "CPU usage above 95%",
		"severity": "critical",
	}, c))
}

func TestEval_DeepMixedTree_RightArmFails(t *testing.T) {
	c := mustFromList(t, []any{
		"AND",
		[]any{"OR",
			[]any{"=", "host", "srv-prod-1"},
			[]any{"NOT", []any{"EXISTS", "maintenance"}},
		},
		[]any{"AND",
			[]any{"MATCHES", "message", "CPU"},
			[]any{"OR",
				[]any{"=", "severity", "critical"},
				[]any{"=", "severity", "error"},
			},
		},
	})
	// Right-arm fails: severity is "info" (not critical/error).
	require.False(t, Match(map[string]any{
		"host":     "srv-prod-1",
		"message":  "CPU usage above 95%",
		"severity": "info",
	}, c))
}

func TestEval_DeepMixedTree_LeftArmFailsViaMaintenance(t *testing.T) {
	c := mustFromList(t, []any{
		"AND",
		[]any{"OR",
			[]any{"=", "host", "srv-prod-1"},
			[]any{"NOT", []any{"EXISTS", "maintenance"}},
		},
		[]any{"AND",
			[]any{"MATCHES", "message", "CPU"},
			[]any{"OR",
				[]any{"=", "severity", "critical"},
				[]any{"=", "severity", "error"},
			},
		},
	})
	// Different host + maintenance is set → left OR fails on both arms,
	// AND fails despite right being satisfied.
	require.False(t, Match(map[string]any{
		"host":        "srv-other",
		"maintenance": true,
		"message":     "CPU usage above 95%",
		"severity":    "critical",
	}, c))
}

func TestEval_DoubleNot(t *testing.T) {
	// NOT(NOT(=(a, 1))) is equivalent to =(a, 1).
	c := mustFromList(t, []any{"NOT", []any{"NOT", []any{"=", "a", int64(1)}}})
	require.True(t, Match(map[string]any{"a": int64(1)}, c))
	require.False(t, Match(map[string]any{"a": int64(2)}, c))
}

func TestEval_NotAroundOrGroup(t *testing.T) {
	// NOT(OR(=(a, 1), =(b, 2))) = AND(!=(a, 1), !=(b, 2))
	c := mustFromList(t, []any{"NOT", []any{"OR",
		[]any{"=", "a", int64(1)},
		[]any{"=", "b", int64(2)},
	}})
	require.False(t, Match(map[string]any{"a": int64(1), "b": int64(99)}, c))
	require.False(t, Match(map[string]any{"a": int64(99), "b": int64(2)}, c))
	require.True(t, Match(map[string]any{"a": int64(99), "b": int64(99)}, c))
}

func TestEval_DeeplyNestedShortCircuit(t *testing.T) {
	// 5-level AND chain matching at every level. Pins that the evaluator
	// walks all the way to the leaves and doesn't truncate at some depth.
	c := mustFromList(t, []any{"AND",
		[]any{"=", "a", int64(1)},
		[]any{"AND",
			[]any{"=", "b", int64(2)},
			[]any{"AND",
				[]any{"=", "c", int64(3)},
				[]any{"AND",
					[]any{"=", "d", int64(4)},
					[]any{"=", "e", int64(5)},
				},
			},
		},
	})
	require.True(t, Match(map[string]any{
		"a": int64(1), "b": int64(2), "c": int64(3), "d": int64(4), "e": int64(5),
	}, c))
	// Innermost leaf fails → whole tree fails (recursion reaches it).
	require.False(t, Match(map[string]any{
		"a": int64(1), "b": int64(2), "c": int64(3), "d": int64(4), "e": int64(99),
	}, c))
}
