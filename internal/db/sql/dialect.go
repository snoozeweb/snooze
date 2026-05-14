// Package sql holds the shared Cond → parameterised SQL translation logic
// reused by the PostgreSQL and SQLite drivers. Per-dialect quirks (path
// extraction, regex syntax, placeholder shape) live behind the Dialect
// interface.
package sql

// Dialect abstracts the per-backend SQL fragments the Cond → SQL translator
// needs. Each driver wires a concrete Dialect into a shared Builder.
type Dialect interface {
	// PathText extracts a JSON path as text. For Postgres:
	//   data #>> '{a,b,c}'
	// For SQLite:
	//   json_extract(data, '$.a.b.c')
	PathText(path []string) string

	// PathJSON extracts a JSON path while preserving the JSON type, used for
	// regex / containment checks that operate on nested arrays.
	PathJSON(path []string) string

	// RegexMatch returns an expression that evaluates true if `left` matches
	// regex `pattern`. Postgres: `left ~* pattern`; SQLite: `left REGEXP pattern`
	// (registered helper).
	RegexMatch(left, pattern string) string

	// ArrayContains returns an expression that evaluates true if the JSON
	// array referenced by jsonExpr contains any of the values in the
	// parameter slot named valuesParam.
	ArrayContains(jsonExpr, valuesParam string) string

	// Placeholder returns the SQL parameter placeholder at index i (1-based).
	// Postgres: $1, $2; SQLite: ?
	Placeholder(i int) string

	// JSONTypeOf returns an expression that yields the JSON type of `expr`
	// as a lowercase string. Used to discriminate scalar vs array fields.
	JSONTypeOf(expr string) string
}
