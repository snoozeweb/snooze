package sql

import (
	"fmt"

	"github.com/snoozeweb/snooze/internal/condition"
)

// Builder walks a condition.Cond AST and emits parameterised SQL. It owns the
// boolean tree (AlwaysTrue / AND / OR / NOT, paren grouping, empty-set literals)
// and the placeholder/arg bookkeeping; every per-dialect leaf fragment lives
// behind the Dialect.
type Builder struct {
	Dialect Dialect
}

// Convert returns a WHERE-clause boolean fragment and the bound parameters for
// cond. searchFields is the list of fields the SEARCH operator expands across;
// an empty list makes SEARCH full-scan the serialised document (the live SQL
// backends never match "nothing" on a bare SEARCH).
func (b *Builder) Convert(cond condition.Cond, searchFields []string) (string, []any, error) {
	if b.Dialect == nil {
		return "", nil, fmt.Errorf("sql: nil dialect")
	}
	w := walker{dialect: b.Dialect, searchFields: searchFields}
	w.binder.dialect = b.Dialect
	sqlStr, err := w.walk(cond)
	if err != nil {
		return "", nil, err
	}
	return sqlStr, w.binder.args, nil
}

type walker struct {
	dialect      Dialect
	binder       Binder
	searchFields []string
}

func (w *walker) walk(c condition.Cond) (string, error) {
	// AlwaysTrue (zero value) matches everything.
	if c.IsZero() {
		return w.dialect.AlwaysTrue(), nil
	}
	switch c.Op {
	case condition.OpAlwaysTrue:
		return w.dialect.AlwaysTrue(), nil
	case condition.OpAnd:
		return w.combine(c.Children, "AND", w.dialect.EmptyAnd())
	case condition.OpOr:
		return w.combine(c.Children, "OR", w.dialect.EmptyOr())
	case condition.OpNot:
		if len(c.Children) == 0 {
			return "", fmt.Errorf("sql: NOT requires one child")
		}
		inner, err := w.walk(c.Children[0])
		if err != nil {
			return "", err
		}
		return "(NOT " + inner + ")", nil
	case condition.OpEq:
		return w.dialect.Eq(c.Field, c.Value, &w.binder), nil
	case condition.OpNeq:
		return w.dialect.Neq(c.Field, c.Value, &w.binder), nil
	case condition.OpGt, condition.OpGte, condition.OpLt, condition.OpLte:
		return w.dialect.Compare(c.Field, string(c.Op), c.Value, &w.binder), nil
	case condition.OpMatches:
		return w.dialect.Matches(c.Field, c.Value, &w.binder), nil
	case condition.OpExists:
		return w.dialect.Exists(c.Field, &w.binder), nil
	case condition.OpContains:
		return w.dialect.Contains(c.Field, c.Value, &w.binder), nil
	case condition.OpIn:
		return w.dialect.In(c.Field, c.Value, &w.binder, w.walk)
	case condition.OpSearch:
		return w.dialect.Search(c.Value, w.searchFields, &w.binder), nil
	}
	return "", fmt.Errorf("sql: unsupported op %q", c.Op)
}

// combine renders an AND/OR group. The group is always parenthesised — even a
// single child — to match the live per-backend translators byte-for-byte. An
// empty group lowers to the dialect's truthy (AND) / falsy (OR) literal.
func (w *walker) combine(kids []condition.Cond, glue, empty string) (string, error) {
	if len(kids) == 0 {
		return empty, nil
	}
	out := "("
	for i, k := range kids {
		if i > 0 {
			out += " " + glue + " "
		}
		s, err := w.walk(k)
		if err != nil {
			return "", err
		}
		out += s
	}
	return out + ")", nil
}
