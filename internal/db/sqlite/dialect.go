package sqlite

import (
	"fmt"
	"strings"

	sqlbuilder "github.com/snoozeweb/snooze/internal/db/sql"
)

// dialect implements sqlbuilder.Dialect for the SQLite/JSON1 backend. It holds
// the leaf-rendering logic — json_extract() navigation, the registered regexp()
// UDF for MATCHES/CONTAINS/SEARCH, NULL-safe IS/IS NOT equality, and the
// json_each() array branches — that the shared Builder invokes per operator.
// The boolean tree (AND/OR/NOT, grouping, empty-set literals) lives in the
// Builder; this file is a straight port of the leaf emitters that used to live
// in convert.go.
type dialect struct{}

var _ sqlbuilder.Dialect = dialect{}

func (dialect) AlwaysTrue() string { return "1" }
func (dialect) EmptyAnd() string   { return "1" }
func (dialect) EmptyOr() string    { return "0" }

// Placeholder returns SQLite's positional "?" marker for every slot.
func (dialect) Placeholder(int) string { return "?" }

func (dialect) Eq(field string, value any, b *sqlbuilder.Binder) string {
	return emitEq(field, value, false, b)
}

func (dialect) Neq(field string, value any, b *sqlbuilder.Binder) string {
	return emitEq(field, value, true, b)
}

func emitEq(field string, value any, negate bool, b *sqlbuilder.Binder) string {
	expr := pathExpr(field, true)
	if value == nil {
		// MATCHES Mongo's `=, null` semantics: the field is absent or null.
		if negate {
			return "(" + expr + " IS NOT NULL)"
		}
		return "(" + expr + " IS NULL)"
	}
	// SQLite's IS / IS NOT semantics are NULL-safe, matching the Python
	// `Query == value` predicate that returns False (not None) when the field
	// is missing. We emit IS / IS NOT for every scalar type below.
	switch v := value.(type) {
	case bool:
		intVal := int64(0)
		if v {
			intVal = 1
		}
		if negate {
			return "(" + expr + " IS NOT " + b.Bind(intVal) + ")"
		}
		return "(" + expr + " = " + b.Bind(intVal) + ")"
	case int, int8, int16, int32, int64, float32, float64:
		if negate {
			return "((" + expr + ") IS NOT " + b.Bind(numericValue(v)) + ")"
		}
		return "((" + expr + ") = " + b.Bind(numericValue(v)) + ")"
	case string:
		if negate {
			return "(" + expr + " IS NOT " + b.Bind(v) + ")"
		}
		return "(" + expr + " = " + b.Bind(v) + ")"
	}
	// Fallback: stringify any other Value type.
	if negate {
		return "(" + expr + " IS NOT " + b.Bind(fmt.Sprint(value)) + ")"
	}
	return "(" + expr + " = " + b.Bind(fmt.Sprint(value)) + ")"
}

func (dialect) Compare(field, op string, value any, b *sqlbuilder.Binder) string {
	expr := pathExpr(field, true)
	var bound string
	switch v := value.(type) {
	case int, int8, int16, int32, int64, float32, float64:
		bound = b.Bind(numericValue(v))
	case bool:
		i := int64(0)
		if v {
			i = 1
		}
		bound = b.Bind(i)
	default:
		bound = b.Bind(fmt.Sprint(v))
	}
	return "((" + expr + ") " + op + " " + bound + ")"
}

func (dialect) Matches(field string, value any, b *sqlbuilder.Binder) string {
	// regexp(pattern, value) returns 1 on match. The UDF compiles
	// case-insensitive so we don't need to wrap the pattern here.
	pattern := fmt.Sprint(value)
	expr := pathExpr(field, true)
	return "(regexp(" + b.Bind(pattern) + ", " + expr + ") = 1)"
}

func (dialect) Exists(field string, _ *sqlbuilder.Binder) string {
	// json_type() returns NULL when the path doesn't resolve, and 'null' when
	// the JSON value is explicit null. EXISTS maps to "field is present", which
	// we treat as "json_type is not NULL" (covers explicit null too).
	return "(json_type(data, '$." + escapeJSONPath(field) + "') IS NOT NULL)"
}

func (dialect) Contains(field string, value any, b *sqlbuilder.Binder) string {
	// CONTAINS: any of the (possibly list) values matches as a regex
	// (case-insensitive) against the field. Field may itself be a scalar or a
	// JSON array — we handle both via UNION of two branches.
	values := toList(value)
	if len(values) == 0 {
		return "0"
	}
	textExpr := pathExpr(field, true)
	jsonExpr := jsonPathExpr(field)

	var sb strings.Builder
	sb.WriteString("(")
	for i, v := range values {
		if i > 0 {
			sb.WriteString(" OR ")
		}
		patStr := fmt.Sprint(v)
		// Scalar branch.
		sb.WriteString("(regexp(")
		sb.WriteString(b.Bind(patStr))
		sb.WriteString(", ")
		sb.WriteString(textExpr)
		sb.WriteString(") = 1)")
		// Array branch (only consulted when the field is a JSON array).
		sb.WriteString(" OR (json_type(")
		sb.WriteString(jsonExpr)
		sb.WriteString(") = 'array' AND EXISTS (SELECT 1 FROM json_each(")
		sb.WriteString(jsonExpr)
		sb.WriteString(") je WHERE regexp(")
		sb.WriteString(b.Bind(patStr))
		sb.WriteString(", je.value) = 1))")
	}
	sb.WriteString(")")
	return sb.String()
}

func (dialect) In(field string, value any, b *sqlbuilder.Binder, _ sqlbuilder.SubRenderer) (string, error) {
	// Field membership: the field's value matches any element of the list
	// literal `value`, OR (if the field is an array) any element of the field
	// array matches.
	//
	// The Python backend supports a recursive sub-condition form
	// (`['IN', ['=', 'foo', 'bar'], 'arrayfield']`) that evaluates the nested
	// condition against each array element. SQLite has no LATERAL rename to bind
	// the iteration variable as `data`, so for now we degrade to literal-list
	// membership against the array's text values when the caller passes a nested
	// condition. The Mongo/Postgres backends keep the full semantics; this is a
	// known divergence flagged for the dbtest matrix.
	if sub, ok := value.([]any); ok {
		return emitInLiteralList(field, sub, b), nil
	}
	if list, ok := value.([]string); ok {
		out := make([]any, len(list))
		for i, s := range list {
			out[i] = s
		}
		return emitInLiteralList(field, out, b), nil
	}
	return emitInLiteralList(field, []any{value}, b), nil
}

func emitInLiteralList(field string, values []any, b *sqlbuilder.Binder) string {
	if len(values) == 0 {
		return "0"
	}
	textExpr := pathExpr(field, true)
	jsonExpr := jsonPathExpr(field)
	var sb strings.Builder
	sb.WriteString("(")
	// Scalar branch: the field's text matches any literal.
	sb.WriteString(textExpr)
	sb.WriteString(" IN (")
	for i, v := range values {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(b.Bind(fmt.Sprint(v)))
	}
	sb.WriteString(")")
	// Array branch: at least one of the field's array elements matches.
	sb.WriteString(" OR (json_type(")
	sb.WriteString(jsonExpr)
	sb.WriteString(") = 'array' AND EXISTS (SELECT 1 FROM json_each(")
	sb.WriteString(jsonExpr)
	sb.WriteString(") je WHERE je.value IN (")
	for i, v := range values {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(b.Bind(fmt.Sprint(v)))
	}
	sb.WriteString(")))")
	sb.WriteString(")")
	return sb.String()
}

func (dialect) Search(value any, searchFields []string, b *sqlbuilder.Binder) string {
	needle := fmt.Sprint(value)
	if len(searchFields) == 0 {
		// Search the entire serialised document.
		return "(regexp(" + b.Bind(needle) + ", data) = 1)"
	}
	var sb strings.Builder
	sb.WriteString("(")
	for i, f := range searchFields {
		if i > 0 {
			sb.WriteString(" OR ")
		}
		sb.WriteString("regexp(")
		sb.WriteString(b.Bind(needle))
		sb.WriteString(", ")
		sb.WriteString(pathExpr(f, true))
		sb.WriteString(") = 1")
	}
	sb.WriteString(")")
	return sb.String()
}
