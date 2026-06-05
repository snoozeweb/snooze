package sql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// gcol is a global (NOT tenant-scoped) collection name; using it for the
// boolean-tree tests means TenantScope never injects a tenant_id predicate, so
// the rendered SQL stays purely a function of the input condition.
const gcol = "tenant"

// gctx returns a plain context for the boolean-tree tests. Paired with gcol
// (a global collection) it never fails closed and never injects a tenant.
func gctx() context.Context { return context.Background() }

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
	sql, args, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpEq, Field: "host", Value: "foo"}, nil)
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
	sql, args, err := b.Convert(gctx(), gcol, cond, nil)
	require.NoError(t, err)
	require.Equal(t, "(host = $1 AND (source = $2 OR source = $3))", sql)
	require.Equal(t, []any{"foo", "syslog", "snmptrap"}, args)
}

func TestBuilder_Not(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(gctx(), gcol, condition.Cond{
		Op:       condition.OpNot,
		Children: []condition.Cond{{Op: condition.OpEq, Field: "x", Value: 1}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "(NOT x = $1)", sql)
}

func TestBuilder_Exists(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpExists, Field: "tags"}, nil)
	require.NoError(t, err)
	require.Equal(t, "tags IS NOT NULL", sql)
}

func TestBuilder_Matches(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpMatches, Field: "msg", Value: "/foo/"}, nil)
	require.NoError(t, err)
	require.Equal(t, "msg REGEX $1", sql)
	require.Equal(t, []any{"foo"}, args)
}

func TestBuilder_Contains(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpContains, Field: "tags", Value: "p1"}, nil)
	require.NoError(t, err)
	require.Equal(t, "CONTAINS(tags, $1)", sql)
	require.Equal(t, []any{"p1"}, args)
}

func TestBuilder_In(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpIn, Field: "tags", Value: []any{"a", "b"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "IN(tags, $1)", sql)
	require.Equal(t, []any{[]any{"a", "b"}}, args)
}

func TestBuilder_NestedPath(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpEq, Field: "a.b.c", Value: 1}, nil)
	require.NoError(t, err)
	require.Equal(t, "a.b.c = $1", sql)
}

func TestBuilder_AlwaysTrue(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(gctx(), gcol, condition.Cond{}, nil)
	require.NoError(t, err)
	require.Equal(t, "TRUE", sql)
	require.Empty(t, args)
}

// A malformed condition with an empty Op but a populated field/value/children
// must be rejected, NOT lowered to "match everything" — otherwise a stored bad
// condition fed to a discarding snooze Delete would wipe the whole collection.
func TestBuilder_EmptyOpWithFieldRejected(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	for _, c := range []condition.Cond{
		{Field: "x"},
		{Value: "y"},
		{Children: []condition.Cond{{Op: condition.OpEq, Field: "a", Value: 1}}},
	} {
		_, _, err := b.Convert(gctx(), gcol, c, nil)
		require.Error(t, err, "empty-op cond %+v must be rejected", c)
	}
}

// NOT must carry exactly one child; >1 (reachable via object-form JSON) was
// silently truncated before and is now rejected on both SQL backends.
func TestBuilder_NotRequiresExactlyOneChild(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	two := condition.Cond{Op: condition.OpNot, Children: []condition.Cond{
		{Op: condition.OpEq, Field: "a", Value: 1},
		{Op: condition.OpEq, Field: "b", Value: 2},
	}}
	_, _, err := b.Convert(gctx(), gcol, two, nil)
	require.Error(t, err)
	_, _, err = b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpNot}, nil)
	require.Error(t, err)
}

func TestBuilder_SearchNoFields(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpSearch, Value: "abc"}, nil)
	require.NoError(t, err)
	// The live SQL backends full-scan a fieldless SEARCH (never "match
	// nothing"); the mock dialect mirrors that with a document-wide regex.
	require.Equal(t, "DOC REGEX $1", sql)
	require.Equal(t, []any{"abc"}, args)
}

func TestBuilder_SearchOverFields(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(
		gctx(), gcol,
		condition.Cond{Op: condition.OpSearch, Value: "abc"},
		[]string{"host", "message"},
	)
	require.NoError(t, err)
	require.Equal(t, "(host ILIKE $1 OR message ILIKE $1)", sql)
	require.Equal(t, []any{"%abc%"}, args)
}

func TestBuilder_NilValueIsNull(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, args, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpEq, Field: "x", Value: nil}, nil)
	require.NoError(t, err)
	require.Equal(t, "x IS NULL", sql)
	require.Empty(t, args)
}

func TestBuilder_EmptyAndOr(t *testing.T) {
	b := &Builder{Dialect: mockDialect{}}
	sql, _, err := b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpAnd}, nil)
	require.NoError(t, err)
	require.Equal(t, "TRUE", sql)
	sql, _, err = b.Convert(gctx(), gcol, condition.Cond{Op: condition.OpOr}, nil)
	require.NoError(t, err)
	require.Equal(t, "FALSE", sql)
}

func TestTableName(t *testing.T) {
	require.Equal(t, "snooze_record", TableName("record"))
	require.Equal(t, "snooze_audit__rule", TableName("audit.rule"))
}

// containsArg reports whether want appears among the bound parameters. Tenant
// injection binds the slug as a positional placeholder, so the test asserts on
// the arg list rather than parsing the placeholder syntax.
func containsArg(args []any, want any) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// A tenant-scoped collection under a tenant context must fold a tenant_id
// predicate into the WHERE clause, binding the slug as a parameter alongside
// the user's own predicate.
func TestBuilderConvert_tenantInjected(t *testing.T) {
	b := Builder{Dialect: mockDialect{}}
	ctx := snoozetypes.WithTenant(context.Background(), "acme")
	sql, args, err := b.Convert(ctx, "record", condition.Equals("host", "srv1"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "tenant_id") {
		t.Fatalf("sql missing tenant_id: %s", sql)
	}
	if !containsArg(args, "acme") {
		t.Fatalf("args missing tenant value %q: %v", "acme", args)
	}
	// host=srv1 predicate must also be present
	if !containsArg(args, "srv1") {
		t.Fatalf("args missing host value: %v", args)
	}
}

// A global collection never receives a tenant_id predicate, even under a
// tenant context.
func TestBuilderConvert_globalCollectionNoInjection(t *testing.T) {
	b := Builder{Dialect: mockDialect{}}
	ctx := snoozetypes.WithTenant(context.Background(), "acme")
	sql, _, err := b.Convert(ctx, "tenant", condition.Cond{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(sql, "tenant_id") {
		t.Fatalf("global collection must not inject tenant_id; got: %s", sql)
	}
}

// A scoped collection with neither a tenant nor platform scope in context must
// fail closed with ErrNoTenant before emitting any SQL.
func TestBuilderConvert_failClosed(t *testing.T) {
	b := Builder{Dialect: mockDialect{}}
	_, _, err := b.Convert(context.Background(), "record", condition.Cond{}, nil)
	if err == nil {
		t.Fatal("naked context on scoped collection: expected error")
	}
	if !errors.Is(err, snoozetypes.ErrNoTenant) {
		t.Fatalf("error must wrap ErrNoTenant, got: %v", err)
	}
}
