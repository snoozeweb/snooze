package postgres

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
	sqlbuilder "github.com/snoozeweb/snooze/internal/db/sql"
)

// dialect implements sqlbuilder.Dialect for the PostgreSQL/JSONB backend. It
// holds the leaf-rendering logic — type-aware ::numeric casts, NULL-safe
// IS DISTINCT FROM, COALESCE two-branch array/scalar unions, the
// jsonb_array_elements recursion for IN-with-sub-Cond — that the shared Builder
// invokes per operator. The boolean tree (AND/OR/NOT, grouping) lives in the
// Builder; this file is a straight port of the leaf emitters that used to live
// in convert.go.
type dialect struct{}

var _ sqlbuilder.Dialect = dialect{}

func (dialect) AlwaysTrue() string { return "TRUE" }
func (dialect) EmptyAnd() string   { return "TRUE" }
func (dialect) EmptyOr() string    { return "FALSE" }

// Placeholder returns the positional $N marker. The Binder hands placeholders
// out in emission order, which is also bind order, so the numbering matches the
// args slice 1:1.
func (dialect) Placeholder(i int) string { return "$" + strconv.Itoa(i) }

func (dialect) Eq(field string, value any, b *sqlbuilder.Binder) string {
	if value == nil {
		// NULL match: either the navigated leaf is JSON null or the top-level
		// key is missing entirely.
		return "(" + pathText(field) + " IS NULL OR NOT (data ? " + b.Bind(firstSegment(field)) + "))"
	}
	return typedCompare(field, "=", value, b)
}

func (dialect) Neq(field string, value any, b *sqlbuilder.Binder) string {
	if value == nil {
		return "(" + pathText(field) + " IS NOT NULL)"
	}
	// NULL-safe inequality using IS DISTINCT FROM.
	//
	// The numeric branch goes through numericExpr for the same reason
	// typedCompare does: an unguarded (data->>'f')::numeric aborts the query
	// with "invalid input syntax for type numeric" the moment one row holds
	// non-numeric text there. Guarded, such a row yields NULL, and
	// "NULL IS DISTINCT FROM 5" is TRUE — which is exactly the answer NEQ
	// wants.
	//
	// Behaviour is unchanged for JSON null, for missing keys, and for text
	// matching numericExpr's guard, which is exactly '^-?[0-9]+(\.[0-9]+)?$'
	// (optional minus, digits, optional .digits — see numericExpr). A JSON
	// STRING holding a numeric form OUTSIDE that grammar — " 5", "+5", "1e5",
	// ".5", "NaN" — used to be accepted by the bare ::numeric cast and now
	// compares as NULL instead, so NEQ reports TRUE for it (and EQ/range
	// report no match) rather than casting. Everything that used to raise the
	// syntax error also becomes NULL, which is the fix. JSON numbers are
	// untouched: ->> renders them in the canonical form the guard accepts.
	// (IS DISTINCT FROM is not btree-indexable either way, so this is a
	// correctness change, not an index one.)
	switch {
	case isNumeric(value):
		return "(" + numericExpr(field) + " IS DISTINCT FROM " + b.Bind(value) + ")"
	case isBool(value):
		return "(" + pathText(field) + " IS DISTINCT FROM " + b.Bind(boolText(value)) + ")"
	default:
		return "(" + pathText(field) + " IS DISTINCT FROM " + b.Bind(fmt.Sprint(value)) + ")"
	}
}

func (dialect) Compare(field, op string, value any, b *sqlbuilder.Binder) string {
	return typedCompare(field, op, value, b)
}

// typedCompare emits "(<expr> OP $N)" with the right cast for the operand type.
// The outer parens around the cast matter: data->>'k'::numeric parses as
// data->>('k'::numeric). Mirrors src/snooze/db/postgres/convert.py.
//
// The numeric branch emits numericExpr(field), i.e. the CASE-guarded cast, for
// two reasons. (1) Indexability: Driver.CreateIndex builds an expression index
// on exactly that string (partial, on "expr IS NOT NULL" — which a strict
// comparison operator implies), and Postgres only matches an expression index
// when the predicate's expression is the same one — the previous bare
// "(data->>'f')::numeric" matched nothing we index, so every range filter on
// date_epoch was a sequential scan. (2) It fixes a latent runtime error: with
// the bare cast, a single row holding non-numeric text in a numerically
// compared field makes the whole query fail with "invalid input syntax for
// type numeric"; the guarded form yields NULL for that row, which simply does
// not match.
//
// The guard accepts exactly '^-?[0-9]+(\.[0-9]+)?$', which is narrower than
// ::numeric: a JSON string like " 5", "+5" or "1e5" now yields NULL and
// matches nothing where it previously cast. See numericExpr for the full
// grammar and rationale.
func typedCompare(field, op string, value any, b *sqlbuilder.Binder) string {
	switch {
	case isBool(value):
		return "(" + pathText(field) + " " + op + " " + b.Bind(boolText(value)) + ")"
	case isNumeric(value):
		return "(" + numericExpr(field) + " " + op + " " + b.Bind(value) + ")"
	default:
		return "(" + pathText(field) + " " + op + " " + b.Bind(fmt.Sprint(value)) + ")"
	}
}

func (dialect) Matches(field string, value any, b *sqlbuilder.Binder) string {
	return "(" + pathText(field) + " ~* " + b.Bind(unsugarRegex(toString(value))) + ")"
}

func (dialect) Exists(field string, b *sqlbuilder.Binder) string {
	if !strings.Contains(field, ".") {
		return "(data ? " + b.Bind(field) + ")"
	}
	return "(" + pathJSON(field) + " IS NOT NULL)"
}

func (dialect) Contains(field string, value any, b *sqlbuilder.Binder) string {
	values := flattenList(value)
	if len(values) == 0 {
		return "FALSE"
	}
	patterns := make([]string, 0, len(values))
	for _, v := range values {
		patterns = append(patterns, unsugarRegex(toString(v)))
	}
	// Two-branch union: either the scalar form of the field matches one of the
	// patterns, or the field is an array and some element matches.
	var sb strings.Builder
	sb.WriteString("COALESCE((")
	sb.WriteString(pathText(field))
	sb.WriteString(" ~* ANY(")
	sb.WriteString(b.Bind(patterns))
	sb.WriteString("::text[])) OR (jsonb_typeof(")
	sb.WriteString(pathJSON(field))
	sb.WriteString(") = 'array' AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(")
	sb.WriteString(pathJSON(field))
	sb.WriteString(") v WHERE v ~* ANY(")
	sb.WriteString(b.Bind(patterns))
	sb.WriteString("::text[]))), false)")
	return sb.String()
}

// dslOperators is the set of operators that can appear as the first element of
// a list literal to mark it as a nested condition (legacy list form).
var dslOperators = map[condition.Op]struct{}{
	condition.OpAnd: {}, condition.OpOr: {}, condition.OpNot: {},
	condition.OpEq: {}, condition.OpNeq: {}, condition.OpGt: {}, condition.OpGte: {},
	condition.OpLt: {}, condition.OpLte: {}, condition.OpMatches: {}, condition.OpExists: {},
	condition.OpContains: {}, condition.OpIn: {}, condition.OpSearch: {},
}

func (dialect) In(field string, value any, b *sqlbuilder.Binder, sub sqlbuilder.SubRenderer) (string, error) {
	// If value is a nested Cond, evaluate it against each array element.
	if cond, ok := value.(condition.Cond); ok {
		var sb strings.Builder
		sb.WriteString("COALESCE((jsonb_typeof(")
		sb.WriteString(pathJSON(field))
		sb.WriteString(") = 'array' AND EXISTS (SELECT 1 FROM jsonb_array_elements(")
		sb.WriteString(pathJSON(field))
		sb.WriteString(") AS arr(data) WHERE ")
		inner, err := sub(cond)
		if err != nil {
			return "", err
		}
		sb.WriteString(inner)
		sb.WriteString(")), false)")
		return sb.String(), nil
	}
	// Legacy: a list whose first element is a DSL operator is a sub-Cond.
	if l, ok := value.([]any); ok && len(l) > 0 {
		if s, ok := l[0].(string); ok {
			if _, isOp := dslOperators[condition.Op(s)]; isOp {
				// Unsupported in the new Cond AST — surface a clear error so
				// upstream callers know they need to wrap nested conds via
				// condition.Cond, not legacy lists.
				return "", fmt.Errorf("%w: IN with nested list-form condition is not supported in the Go driver", dbpkg.ErrBadCondition)
			}
		}
	}
	// Literal membership over either a scalar field or an array of scalars.
	items := flattenList(value)
	strs := make([]string, 0, len(items))
	for _, it := range items {
		strs = append(strs, toString(it))
	}
	var sb strings.Builder
	sb.WriteString("COALESCE(")
	sb.WriteString(pathText(field))
	sb.WriteString(" = ANY(")
	sb.WriteString(b.Bind(strs))
	sb.WriteString("::text[]) OR (jsonb_typeof(")
	sb.WriteString(pathJSON(field))
	sb.WriteString(") = 'array' AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(")
	sb.WriteString(pathJSON(field))
	sb.WriteString(") v WHERE v = ANY(")
	sb.WriteString(b.Bind(strs))
	sb.WriteString("::text[]))), false)")
	return sb.String(), nil
}

func (dialect) Search(value any, searchFields []string, b *sqlbuilder.Binder) string {
	needle := toString(value)
	if len(searchFields) > 0 {
		var sb strings.Builder
		sb.WriteString("(")
		for i, fld := range searchFields {
			if i > 0 {
				sb.WriteString(" OR ")
			}
			sb.WriteString("(")
			sb.WriteString(pathText(fld))
			sb.WriteString(" ~* ")
			sb.WriteString(b.Bind(needle))
			sb.WriteString(")")
		}
		sb.WriteString(")")
		return sb.String()
	}
	return "(data::text ~* " + b.Bind(needle) + ")"
}
