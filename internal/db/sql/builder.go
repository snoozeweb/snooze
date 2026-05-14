package sql

import (
	"fmt"
	"strings"

	"github.com/japannext/snooze/internal/condition"
)

// Builder walks a condition.Cond AST and emits parameterised SQL using the
// dialect for backend-specific quoting.
type Builder struct {
	Dialect Dialect
}

// Convert returns a WHERE-clause fragment and the bound parameters for cond.
// searchFields is the list of fields that SEARCH expands across.
func (b *Builder) Convert(cond condition.Cond, searchFields []string) (string, []any, error) {
	if b.Dialect == nil {
		return "", nil, fmt.Errorf("sql: nil dialect")
	}
	w := walker{dialect: b.Dialect, searchFields: searchFields}
	sqlStr, err := w.walk(cond)
	if err != nil {
		return "", nil, err
	}
	if sqlStr == "" {
		// AlwaysTrue: emit a trivially true predicate so callers can append it.
		return "1=1", nil, nil
	}
	return sqlStr, w.args, nil
}

type walker struct {
	dialect      Dialect
	args         []any
	searchFields []string
}

func (w *walker) bind(v any) string {
	w.args = append(w.args, v)
	return w.dialect.Placeholder(len(w.args))
}

func (w *walker) walk(c condition.Cond) (string, error) {
	switch c.Op {
	case condition.OpAlwaysTrue:
		return "", nil
	case condition.OpAnd:
		return w.combine(c.Children, "AND")
	case condition.OpOr:
		return w.combine(c.Children, "OR")
	case condition.OpNot:
		if len(c.Children) != 1 {
			return "", fmt.Errorf("sql: NOT expects one child, got %d", len(c.Children))
		}
		inner, err := w.walk(c.Children[0])
		if err != nil {
			return "", err
		}
		if inner == "" {
			return "FALSE", nil
		}
		return "NOT (" + inner + ")", nil
	case condition.OpEq:
		return w.binaryScalar(c, "=")
	case condition.OpNeq:
		return w.binaryScalar(c, "<>")
	case condition.OpGt:
		return w.binaryScalar(c, ">")
	case condition.OpGte:
		return w.binaryScalar(c, ">=")
	case condition.OpLt:
		return w.binaryScalar(c, "<")
	case condition.OpLte:
		return w.binaryScalar(c, "<=")
	case condition.OpMatches:
		path := splitPath(c.Field)
		left := w.dialect.PathText(path)
		pat := unsugarRegex(asString(c.Value))
		ph := w.bind(pat)
		return w.dialect.RegexMatch(left, ph), nil
	case condition.OpExists:
		path := splitPath(c.Field)
		return w.dialect.PathText(path) + " IS NOT NULL", nil
	case condition.OpContains:
		path := splitPath(c.Field)
		jsonExpr := w.dialect.PathJSON(path)
		ph := w.bind(c.Value)
		return w.dialect.ArrayContains(jsonExpr, ph), nil
	case condition.OpIn:
		path := splitPath(c.Field)
		jsonExpr := w.dialect.PathJSON(path)
		ph := w.bind(c.Value)
		return w.dialect.ArrayContains(jsonExpr, ph), nil
	case condition.OpSearch:
		return w.search(c.Value)
	}
	return "", fmt.Errorf("sql: unsupported op %q", c.Op)
}

func (w *walker) combine(kids []condition.Cond, glue string) (string, error) {
	parts := make([]string, 0, len(kids))
	for _, k := range kids {
		s, err := w.walk(k)
		if err != nil {
			return "", err
		}
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	switch len(parts) {
	case 0:
		return "", nil
	case 1:
		return parts[0], nil
	}
	return "(" + strings.Join(parts, " "+glue+" ") + ")", nil
}

func (w *walker) binaryScalar(c condition.Cond, op string) (string, error) {
	path := splitPath(c.Field)
	left := w.dialect.PathText(path)
	if c.Value == nil {
		if op == "=" {
			return left + " IS NULL", nil
		}
		if op == "<>" {
			return left + " IS NOT NULL", nil
		}
	}
	right := w.bind(asString(c.Value))
	return left + " " + op + " " + right, nil
}

func (w *walker) search(value any) (string, error) {
	if len(w.searchFields) == 0 {
		// SEARCH with no registered fields matches nothing: well-defined,
		// per the plan's "Drop Mongo $where SEARCH" rule.
		return "FALSE", nil
	}
	pat := "%" + asString(value) + "%"
	ph := w.bind(pat)
	parts := make([]string, 0, len(w.searchFields))
	for _, f := range w.searchFields {
		parts = append(parts, w.dialect.PathText(splitPath(f))+" ILIKE "+ph)
	}
	return "(" + strings.Join(parts, " OR ") + ")", nil
}

// splitPath converts "a.b.c" into ["a","b","c"]. Numeric components are
// preserved as text — the dialect handles type-driven indexing.
func splitPath(s string) []string {
	if s == "" {
		return nil
	}
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	}
	return fmt.Sprintf("%v", v)
}

func unsugarRegex(s string) string {
	if len(s) >= 2 && s[0] == '/' && s[len(s)-1] == '/' {
		return s[1 : len(s)-1]
	}
	return s
}
