package sql

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/condition"
)

// mockDialect emits human-readable SQL fragments so the tests focus on
// shape, not exact backend syntax.
type mockDialect struct{}

func (mockDialect) PathText(path []string) string {
	return "TEXT(" + strings.Join(path, ".") + ")"
}
func (mockDialect) PathJSON(path []string) string {
	return "JSON(" + strings.Join(path, ".") + ")"
}
func (mockDialect) RegexMatch(left, pattern string) string {
	return left + " REGEX " + pattern
}
func (mockDialect) ArrayContains(jsonExpr, valuesParam string) string {
	return "CONTAINS(" + jsonExpr + ", " + valuesParam + ")"
}
func (mockDialect) Placeholder(i int) string { return fmt.Sprintf("$%d", i) }
func (mockDialect) JSONTypeOf(expr string) string {
	return "TYPEOF(" + expr + ")"
}

func TestBuilder_Equality(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpEq, Field: "host", Value: "foo"}, nil)
	require.NoError(t, err)
	require.Equal(t, "TEXT(host) = $1", sql)
	require.Equal(t, []any{"foo"}, args)
}

func TestBuilder_NestedAndOr(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	cond := condition.Cond{Op: condition.OpAnd, Children: []condition.Cond{
		{Op: condition.OpEq, Field: "host", Value: "foo"},
		{Op: condition.OpOr, Children: []condition.Cond{
			{Op: condition.OpEq, Field: "source", Value: "syslog"},
			{Op: condition.OpEq, Field: "source", Value: "snmptrap"},
		}},
	}}
	sql, args, err := b.Convert(cond, nil)
	require.NoError(t, err)
	require.Equal(t, "(TEXT(host) = $1 AND (TEXT(source) = $2 OR TEXT(source) = $3))", sql)
	require.Equal(t, []any{"foo", "syslog", "snmptrap"}, args)
}

func TestBuilder_Not(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(condition.Cond{
		Op:       condition.OpNot,
		Children: []condition.Cond{{Op: condition.OpEq, Field: "x", Value: 1}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "NOT (TEXT(x) = $1)", sql)
}

func TestBuilder_Exists(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(condition.Cond{Op: condition.OpExists, Field: "tags"}, nil)
	require.NoError(t, err)
	require.Equal(t, "TEXT(tags) IS NOT NULL", sql)
}

func TestBuilder_Matches(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpMatches, Field: "msg", Value: "/foo/"}, nil)
	require.NoError(t, err)
	require.Equal(t, "TEXT(msg) REGEX $1", sql)
	require.Equal(t, []any{"foo"}, args)
}

func TestBuilder_Contains(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpContains, Field: "tags", Value: "p1"}, nil)
	require.NoError(t, err)
	require.Equal(t, "CONTAINS(JSON(tags), $1)", sql)
	require.Equal(t, []any{"p1"}, args)
}

func TestBuilder_In(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpIn, Field: "tags", Value: []any{"a", "b"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "CONTAINS(JSON(tags), $1)", sql)
	require.Equal(t, []any{[]any{"a", "b"}}, args)
}

func TestBuilder_NestedPath(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(condition.Cond{Op: condition.OpEq, Field: "a.b.c", Value: 1}, nil)
	require.NoError(t, err)
	require.Equal(t, "TEXT(a.b.c) = $1", sql)
}

func TestBuilder_AlwaysTrue(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{}, nil)
	require.NoError(t, err)
	require.Equal(t, "1=1", sql)
	require.Empty(t, args)
}

func TestBuilder_SearchNoFields(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(condition.Cond{Op: condition.OpSearch, Value: "abc"}, nil)
	require.NoError(t, err)
	require.Equal(t, "FALSE", sql)
}

func TestBuilder_SearchOverFields(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(
		condition.Cond{Op: condition.OpSearch, Value: "abc"},
		[]string{"host", "message"},
	)
	require.NoError(t, err)
	require.Equal(t, "(TEXT(host) ILIKE $1 OR TEXT(message) ILIKE $1)", sql)
	require.Equal(t, []any{"%abc%"}, args)
}

func TestBuilder_NilValueIsNull(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpEq, Field: "x", Value: nil}, nil)
	require.NoError(t, err)
	require.Equal(t, "TEXT(x) IS NULL", sql)
	require.Empty(t, args)
}

func TestTableName(t *testing.T) {
	require.Equal(t, "snooze_record", TableName("record"))
	require.Equal(t, "snooze_audit__rule", TableName("audit.rule"))
}
