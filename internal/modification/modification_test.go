package modification

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Each test below mirrors a function in tests/utils/test_modification.py.

// TestValidate ports test_modification_validate at tests/utils/test_modification.py:13-21.
func TestValidate(t *testing.T) {
	rule := map[string]any{
		"name":      "My rule 1",
		"condition": []any{},
		"modifications": []any{
			[]any{"SET", "a", "1"},
		},
	}
	require.NoError(t, Validate(rule))
}

// TestValidateUnknownOp guards against accidentally accepting unsupported ops.
func TestValidateUnknownOp(t *testing.T) {
	rule := map[string]any{
		"modifications": []any{
			[]any{"NOPE", "a", "1"},
		},
	}
	require.ErrorIs(t, Validate(rule), ErrOperationNotSupported)
}

// TestSet ports test_modification_set at tests/utils/test_modification.py:23-28.
func TestSet(t *testing.T) {
	rec := map[string]any{"a": 1, "b": 2}
	m, err := Parse([]any{"SET", "c", 3})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, map[string]any{"a": 1, "b": 2, "c": 3}, rec)
}

// TestDelete ports test_modification_delete at tests/utils/test_modification.py:30-35.
func TestDelete(t *testing.T) {
	rec := map[string]any{"a": 1, "b": 2}
	m, err := Parse([]any{"DELETE", "b"})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, map[string]any{"a": 1}, rec)
}

// TestDeleteMissing covers the KeyError → False branch at modification.py:91.
func TestDeleteMissing(t *testing.T) {
	rec := map[string]any{"a": 1}
	m, err := Parse([]any{"DELETE", "missing"})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, map[string]any{"a": 1}, rec)
}

// TestArrayAppend ports test_modification_array_append at tests/utils/test_modification.py:37-42.
func TestArrayAppend(t *testing.T) {
	rec := map[string]any{"a": 1, "b": []any{"1", "2", "3"}}
	m, err := Parse([]any{"ARRAY_APPEND", "b", "4"})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, map[string]any{"a": 1, "b": []any{"1", "2", "3", "4"}}, rec)
}

// TestArrayAppendNonList exercises the `isinstance(array, list)` False branch.
func TestArrayAppendNonList(t *testing.T) {
	rec := map[string]any{"a": "scalar"}
	m, err := Parse([]any{"ARRAY_APPEND", "a", "x"})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, map[string]any{"a": "scalar"}, rec)
}

// TestArrayDelete ports test_modification_array_delete at tests/utils/test_modification.py:44-48.
func TestArrayDelete(t *testing.T) {
	rec := map[string]any{"a": 1, "b": []any{"1", "2", "3"}}
	m, err := Parse([]any{"ARRAY_DELETE", "b", "2"})
	require.NoError(t, err)
	_, err = Apply(rec, m)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"a": 1, "b": []any{"1", "3"}}, rec)
}

// TestArrayDeleteMissingValue covers the ValueError branch at modification.py:126.
func TestArrayDeleteMissingValue(t *testing.T) {
	rec := map[string]any{"b": []any{"1", "2"}}
	m, err := Parse([]any{"ARRAY_DELETE", "b", "99"})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, []any{"1", "2"}, rec["b"])
}

// TestTemplate ports test_modification_template at tests/utils/test_modification.py:50-56.
// Exercises Jinja2-style `{{ (a | int) + (b | int) }}` rendering.
func TestTemplate(t *testing.T) {
	rec := map[string]any{"a": "1", "b": "2"}
	m, err := Parse([]any{"SET", "c", "{{ (a | int) + (b | int) }}"})
	require.NoError(t, err)
	_, err = Apply(rec, m)
	require.NoError(t, err)
	require.Equal(t, "1", rec["a"])
	require.Equal(t, "2", rec["b"])
	require.Equal(t, "3", rec["c"])
}

// TestTemplateBareVar covers the simplest `{{ field }}` substitution.
func TestTemplateBareVar(t *testing.T) {
	rec := map[string]any{"host": "srv01"}
	m, err := Parse([]any{"SET", "owner", "team-{{ host }}"})
	require.NoError(t, err)
	_, err = Apply(rec, m)
	require.NoError(t, err)
	require.Equal(t, "team-srv01", rec["owner"])
}

// TestRegexParse ports test_modification_regex_parse at tests/utils/test_modification.py:58-63.
func TestRegexParse(t *testing.T) {
	rec := map[string]any{"message": "CRON[12345]: Error during cronjob"}
	m, err := Parse([]any{"REGEX_PARSE", "message", `(?P<appname>.*?)\[(?P<pid>\d+)\]: (?P<message>.*)`})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, map[string]any{
		"message": "Error during cronjob",
		"appname": "CRON",
		"pid":     "12345",
	}, rec)
}

// TestRegexParseBrokenRegex ports test_modification_regex_parse_broken_regex at
// tests/utils/test_modification.py:65-70.
func TestRegexParseBrokenRegex(t *testing.T) {
	rec := map[string]any{"message": "CRON[12345]: Error during cronjob"}
	m, err := Parse([]any{"REGEX_PARSE", "message", `(?P<appname.*?)\[(?P<pid>\d+)\]: (?P<message>.*)`})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, map[string]any{"message": "CRON[12345]: Error during cronjob"}, rec)
}

// TestRegexSub ports test_modification_regex_sub at tests/utils/test_modification.py:72-77.
func TestRegexSub(t *testing.T) {
	rec := map[string]any{"message": "Error in session 0x2134adf890bc89"}
	m, err := Parse([]any{"REGEX_SUB", "message", "message", `0x[a-fA-F0-9]+`, "0x###"})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, map[string]any{"message": "Error in session 0x###"}, rec)
}

// TestRegexSubMissingKey covers the KeyError → False branch at modification.py:172.
func TestRegexSubMissingKey(t *testing.T) {
	rec := map[string]any{"other": "noop"}
	m, err := Parse([]any{"REGEX_SUB", "message", "message", `\d+`, "###"})
	require.NoError(t, err)
	changed, err := Apply(rec, m)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, map[string]any{"other": "noop"}, rec)
}

// TestApplyAll wires multiple modifications in order.
func TestApplyAll(t *testing.T) {
	rec := map[string]any{"a": 1}
	ms := []Modification{
		mustParse(t, []any{"SET", "b", 2}),
		mustParse(t, []any{"DELETE", "a"}),
	}
	require.NoError(t, ApplyAll(rec, ms))
	require.Equal(t, map[string]any{"b": 2}, rec)
}

// TestParseUnknownOp surfaces ErrOperationNotSupported.
func TestParseUnknownOp(t *testing.T) {
	_, err := Parse([]any{"NOPE", "a"})
	require.ErrorIs(t, err, ErrOperationNotSupported)
}

// TestParseEmpty rejects empty arg lists.
func TestParseEmpty(t *testing.T) {
	_, err := Parse([]any{})
	require.ErrorIs(t, err, ErrInvalid)
}

// TestParsePadding mirrors Modification.__init__ at modification.py:49-51 —
// short arg lists are padded with empty strings.
func TestParsePadding(t *testing.T) {
	m, err := Parse([]any{"SET"})
	require.NoError(t, err)
	require.Equal(t, []any{"", ""}, m.Args)
}

func mustParse(t *testing.T, args []any) Modification {
	t.Helper()
	m, err := Parse(args)
	require.NoError(t, err)
	return m
}
