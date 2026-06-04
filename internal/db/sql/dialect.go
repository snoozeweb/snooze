// Package sql holds the shared Cond → parameterised SQL translation logic
// reused by the PostgreSQL and SQLite drivers. The Builder owns the boolean
// tree walk (AlwaysTrue / AND / OR / NOT and their empty-set semantics, paren
// grouping, sub-condition recursion, and placeholder bookkeeping); every
// per-dialect quirk (JSON path extraction, type-aware casts, regex syntax,
// array/scalar two-branch unions, full-scan SEARCH) lives behind the Dialect.
package sql

import "github.com/snoozeweb/snooze/internal/condition"

// Binder accumulates bound parameters and hands back the placeholder text for
// each, in emission order. The walker creates one Binder per Convert call and
// threads it through every leaf renderer so the args slice ends up in the same
// order the placeholders appear in the SQL.
type Binder struct {
	dialect Dialect
	args    []any
}

// Bind records v and returns its placeholder (e.g. "$3" or "?").
func (b *Binder) Bind(v any) string {
	b.args = append(b.args, v)
	return b.dialect.Placeholder(len(b.args))
}

// Args returns the parameters bound so far, in order.
func (b *Binder) Args() []any { return b.args }

// SubRenderer renders a nested condition into a boolean SQL fragment using the
// same Binder, so an `IN`-with-sub-Cond can recurse back into the walker. It is
// passed to Dialect.In for backends (Postgres) that support the sub-Cond form.
type SubRenderer func(condition.Cond) (string, error)

// Dialect abstracts the per-backend SQL fragments the Cond → SQL translator
// needs. Each driver wires a concrete Dialect into a shared Builder. Every
// leaf renderer receives the shared Binder so bound parameters and their
// placeholders stay in lockstep.
//
// The Builder guarantees the renderers are never called with a boolean op
// (AND/OR/NOT) nor with AlwaysTrue — those are handled by the walker. The
// boolean-empty and always-true sentinels below let each dialect keep its own
// truthy/falsy literals ("TRUE"/"FALSE" for Postgres, "1"/"0" for SQLite).
type Dialect interface {
	// AlwaysTrue is the literal a zero-value (AlwaysTrue) Cond lowers to.
	AlwaysTrue() string
	// EmptyAnd is the literal an AND with no children lowers to.
	EmptyAnd() string
	// EmptyOr is the literal an OR with no children lowers to.
	EmptyOr() string

	// Placeholder returns the SQL parameter placeholder at index i (1-based).
	// Postgres: $1, $2; SQLite: ?
	Placeholder(i int) string

	// Eq / Neq render equality / inequality for field against value. value may
	// be nil (NULL / missing-key semantics).
	Eq(field string, value any, b *Binder) string
	Neq(field string, value any, b *Binder) string

	// Compare renders an ordered comparison (op is one of >, >=, <, <=).
	Compare(field, op string, value any, b *Binder) string

	// Matches renders a case-insensitive regex match against field.
	Matches(field string, value any, b *Binder) string

	// Exists renders a "field is present" predicate.
	Exists(field string, b *Binder) string

	// Contains renders the CONTAINS predicate: value (scalar or list) matched
	// as a case-insensitive regex against field, which may itself be a scalar
	// or a JSON array.
	Contains(field string, value any, b *Binder) string

	// In renders the IN predicate. When value is a condition.Cond, the dialect
	// may recurse via sub (Postgres) or degrade to literal membership (SQLite);
	// each backend keeps its current behaviour. It may return an error for
	// unsupported forms (e.g. Postgres' legacy list-form nested cond).
	In(field string, value any, b *Binder, sub SubRenderer) (string, error)

	// Search renders the SEARCH predicate. With searchFields it ORs a
	// per-field regex; with none it full-scans the serialised document.
	Search(value any, searchFields []string, b *Binder) string
}
