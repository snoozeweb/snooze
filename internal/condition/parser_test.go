package condition

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Each case asserts the Cond's legacy list form against the expected pyparsing
// output, so the corpus reads identically to the Python test_parser.py.
type parserCase struct {
	name    string
	input   string
	want    any // list-form expected output
	wantErr bool
}

func runParserCases(t *testing.T, cases []parserCase) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.input)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.want, got.ToList())
		})
	}
}

func TestParserLogic(t *testing.T) {
	runParserCases(t, []parserCase{
		{name: "word", input: "hello", want: []any{"SEARCH", "hello"}},
		{name: "key_value", input: "key = value", want: []any{"=", "key", "value"}},
		{name: "and", input: "key1=value1 AND key2=value2",
			want: []any{"AND", []any{"=", "key1", "value1"}, []any{"=", "key2", "value2"}}},
		{name: "and_symbol", input: "key1=value1&key2=value2",
			want: []any{"AND", []any{"=", "key1", "value1"}, []any{"=", "key2", "value2"}}},
		{name: "implicit_and", input: "key1=value1 key2=value2",
			want: []any{"AND", []any{"=", "key1", "value1"}, []any{"=", "key2", "value2"}}},
		{name: "or", input: "key1=value1 OR key2=value2",
			want: []any{"OR", []any{"=", "key1", "value1"}, []any{"=", "key2", "value2"}}},
		{name: "or_symbol", input: "key1=value1|key2=value2",
			want: []any{"OR", []any{"=", "key1", "value1"}, []any{"=", "key2", "value2"}}},
		{name: "not", input: "not key1=value1",
			want: []any{"NOT", []any{"=", "key1", "value1"}}},
		{name: "not_symbol", input: "!key1=value1",
			want: []any{"NOT", []any{"=", "key1", "value1"}}},
		{name: "parenthesis", input: "NOT (key1=value1 AND key2=value2)",
			want: []any{"NOT", []any{"AND", []any{"=", "key1", "value1"}, []any{"=", "key2", "value2"}}}},
		{name: "priority", input: "NOT key1=value1 AND key2=value2",
			want: []any{"AND", []any{"NOT", []any{"=", "key1", "value1"}}, []any{"=", "key2", "value2"}}},
		{name: "complex_query",
			input: `myapp and source=syslog and custom_field = "myapp01" or custom_field = "myapp02"`,
			want: []any{"AND",
				[]any{"SEARCH", "myapp"},
				[]any{"AND",
					[]any{"=", "source", "syslog"},
					[]any{"OR",
						[]any{"=", "custom_field", "myapp01"},
						[]any{"=", "custom_field", "myapp02"},
					},
				},
			}},
		{name: "complex_query_parenthesis",
			input: `myapp and source=syslog and (custom_field = "myapp01" or custom_field = "myapp02")`,
			want: []any{"AND",
				[]any{"SEARCH", "myapp"},
				[]any{"AND",
					[]any{"=", "source", "syslog"},
					[]any{"OR",
						[]any{"=", "custom_field", "myapp01"},
						[]any{"=", "custom_field", "myapp02"},
					},
				},
			}},
	})
}

func TestParserTypes(t *testing.T) {
	runParserCases(t, []parserCase{
		{name: "integer", input: "123", want: []any{"SEARCH", int64(123)}},
		{name: "negative_integer", input: "-42", want: []any{"SEARCH", int64(-42)}},
		{name: "float", input: "3.14", want: []any{"SEARCH", 3.14}},
		{name: "bool_true", input: "mybool=true", want: []any{"=", "mybool", true}},
		{name: "bool_false", input: "mybool=false", want: []any{"=", "mybool", false}},
		{name: "double_quoted_string", input: `key = "value"`, want: []any{"=", "key", "value"}},
		{name: "single_quoted_string", input: `key = 'value'`, want: []any{"=", "key", "value"}},
		{name: "double_quoted_escape", input: `key = "value\t\n\\"`,
			want: []any{"=", "key", "value\t\n\\"}},
		{name: "double_quoted_escape_quote", input: `key = "my \"test\""`,
			want: []any{"=", "key", `my "test"`}},
		{name: "quoted_field", input: `"myfield with space" = myapp01`,
			want: []any{"=", "myfield with space", "myapp01"}},
		{name: "single_quote_string", input: `'myfield' = 'myvalue'`,
			want: []any{"=", "myfield", "myvalue"}},
		{name: "array", input: "myfield = [1, 2, 3]",
			want: []any{"=", "myfield", []any{int64(1), int64(2), int64(3)}}},
		{name: "nested_array", input: "myfield = [[1], [2], [3]]",
			want: []any{"=", "myfield", []any{[]any{int64(1)}, []any{int64(2)}, []any{int64(3)}}}},
		{name: "dict", input: "myfield = {a: 1, b: 2}",
			want: []any{"=", "myfield", map[string]any{"a": int64(1), "b": int64(2)}}},
		{name: "nested_dict", input: `myfield = {a: {"mymessage": "x"}, b: 2}`,
			want: []any{"=", "myfield",
				map[string]any{"a": map[string]any{"mymessage": "x"}, "b": int64(2)}}},
		{name: "hash", input: "hash=3f75728488a0e6892905f0db6a473382",
			want: []any{"=", "hash", "3f75728488a0e6892905f0db6a473382"}},
	})
}

func TestParserOperations(t *testing.T) {
	runParserCases(t, []parserCase{
		{name: "nequal", input: "process != systemd",
			want: []any{"!=", "process", "systemd"}},
		{name: "matches", input: `message MATCHES "[aA]lert"`,
			want: []any{"MATCHES", "message", "[aA]lert"}},
		{name: "matches_symbol", input: `message ~ "[aA]lert"`,
			want: []any{"MATCHES", "message", "[aA]lert"}},
		{name: "exists", input: "custom_field EXISTS",
			want: []any{"EXISTS", "custom_field"}},
		{name: "exists_symbol", input: "custom_field?",
			want: []any{"EXISTS", "custom_field"}},
		{name: "exists_symbol_not", input: "!custom_field?",
			want: []any{"NOT", []any{"EXISTS", "custom_field"}}},
		{name: "gt", input: "mail_queue>100",
			want: []any{">", "mail_queue", int64(100)}},
		{name: "lt", input: "port < 1024",
			want: []any{"<", "port", int64(1024)}},
		{name: "contains", input: "rules contains myrule",
			want: []any{"CONTAINS", "rules", "myrule"}},
		{name: "contains_array", input: "myarray contains [1, 2, 3]",
			want: []any{"CONTAINS", "myarray", []any{int64(1), int64(2), int64(3)}}},
		{name: "in", input: "myrule in rules",
			want: []any{"IN", "myrule", "rules"}},
		{name: "in_array", input: "[1, 2, 3] in myarray",
			want: []any{"IN", []any{int64(1), int64(2), int64(3)}, "myarray"}},
	})
}
