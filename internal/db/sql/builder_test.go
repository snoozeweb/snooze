package sql

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
)

// mockDialect emits human-readable SQL fragments so the tests focus on the
// boolean-tree shape the Builder owns, not on exact backend leaf syntax. Leaf
// renderers are intentionally terse — the per-backend leaf SQL is covered by
// each driver's own convert tests and the shared driver suite.
type mockDialect struct{}

func (mockDialect) AlwaysTrue() string       { return "TRUE" }
func (mockDialect) EmptyAnd() string         { return "TRUE" }
func (mockDialect) EmptyOr() string          { return "FALSE" }
func (mockDialect) Placeholder(i int) string { return fmt.Sprintf("$%d", i) }

func (mockDialect) Eq(field string, value any, b *Binder) string {
	if value == nil {
		return field + " IS NULL"
	}
	return field + " = " + b.Bind(value)
}
func (mockDialect) Neq(field string, value any, b *Binder) string {
	if value == nil {
		return field + " IS NOT NULL"
	}
	return field + " <> " + b.Bind(value)
}
func (mockDialect) Compare(field, op string, value any, b *Binder) string {
	return field + " " + op + " " + b.Bind(value)
}
func (mockDialect) Matches(field string, value any, b *Binder) string {
	return field + " REGEX " + b.Bind(unsugarRegex(fmt.Sprint(value)))
}
func (mockDialect) Exists(field string, _ *Binder) string {
	return field + " IS NOT NULL"
}
func (mockDialect) Contains(field string, value any, b *Binder) string {
	return "CONTAINS(" + field + ", " + b.Bind(value) + ")"
}
func (mockDialect) In(field string, value any, b *Binder, _ SubRenderer) (string, error) {
	return "IN(" + field + ", " + b.Bind(value) + ")", nil
}
func (mockDialect) Search(value any, fields []string, b *Binder) string {
	if len(fields) == 0 {
		return "DOC REGEX " + b.Bind(fmt.Sprint(value))
	}
	ph := b.Bind("%" + fmt.Sprint(value) + "%")
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f+" ILIKE "+ph)
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func unsugarRegex(s string) string {
	if len(s) >= 2 && s[0] == '/' && s[len(s)-1] == '/' {
		return s[1 : len(s)-1]
	}
	return s
}

func TestBuilder_Equality(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpEq, Field: "host", Value: "foo"}, nil)
	require.NoError(t, err)
	require.Equal(t, "host = $1", sql)
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
	require.Equal(t, "(host = $1 AND (source = $2 OR source = $3))", sql)
	require.Equal(t, []any{"foo", "syslog", "snmptrap"}, args)
}

func TestBuilder_Not(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(condition.Cond{
		Op:       condition.OpNot,
		Children: []condition.Cond{{Op: condition.OpEq, Field: "x", Value: 1}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "(NOT x = $1)", sql)
}

func TestBuilder_Exists(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(condition.Cond{Op: condition.OpExists, Field: "tags"}, nil)
	require.NoError(t, err)
	require.Equal(t, "tags IS NOT NULL", sql)
}

func TestBuilder_Matches(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpMatches, Field: "msg", Value: "/foo/"}, nil)
	require.NoError(t, err)
	require.Equal(t, "msg REGEX $1", sql)
	require.Equal(t, []any{"foo"}, args)
}

func TestBuilder_Contains(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpContains, Field: "tags", Value: "p1"}, nil)
	require.NoError(t, err)
	require.Equal(t, "CONTAINS(tags, $1)", sql)
	require.Equal(t, []any{"p1"}, args)
}

func TestBuilder_In(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpIn, Field: "tags", Value: []any{"a", "b"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "IN(tags, $1)", sql)
	require.Equal(t, []any{[]any{"a", "b"}}, args)
}

func TestBuilder_NestedPath(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(condition.Cond{Op: condition.OpEq, Field: "a.b.c", Value: 1}, nil)
	require.NoError(t, err)
	require.Equal(t, "a.b.c = $1", sql)
}

func TestBuilder_AlwaysTrue(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{}, nil)
	require.NoError(t, err)
	require.Equal(t, "TRUE", sql)
	require.Empty(t, args)
}

func TestBuilder_SearchNoFields(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpSearch, Value: "abc"}, nil)
	require.NoError(t, err)
	// The live SQL backends full-scan a fieldless SEARCH (never "match
	// nothing"); the mock dialect mirrors that with a document-wide regex.
	require.Equal(t, "DOC REGEX $1", sql)
	require.Equal(t, []any{"abc"}, args)
}

func TestBuilder_SearchOverFields(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(
		condition.Cond{Op: condition.OpSearch, Value: "abc"},
		[]string{"host", "message"},
	)
	require.NoError(t, err)
	require.Equal(t, "(host ILIKE $1 OR message ILIKE $1)", sql)
	require.Equal(t, []any{"%abc%"}, args)
}

func TestBuilder_NilValueIsNull(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(condition.Cond{Op: condition.OpEq, Field: "x", Value: nil}, nil)
	require.NoError(t, err)
	require.Equal(t, "x IS NULL", sql)
	require.Empty(t, args)
}

func TestBuilder_EmptyAndOr(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(condition.Cond{Op: condition.OpAnd}, nil)
	require.NoError(t, err)
	require.Equal(t, "TRUE", sql)
	sql, _, err = b.Convert(condition.Cond{Op: condition.OpOr}, nil)
	require.NoError(t, err)
	require.Equal(t, "FALSE", sql)
}

func TestTableName(t *testing.T) {
	require.Equal(t, "snooze_record", TableName("record"))
	require.Equal(t, "snooze_audit__rule", TableName("audit.rule"))
}
