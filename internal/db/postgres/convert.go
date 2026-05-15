package postgres

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/japannext/snooze/internal/condition"
	dbpkg "github.com/japannext/snooze/internal/db"
)

// convertResult is the rendered SQL fragment plus its bound parameters.
type convertResult struct {
	SQL    string
	Params []any
}

// fragment is a single rendered SQL chunk emitted by the translator, before
// numbered placeholders are stitched into the final string.
type fragment struct {
	sb   strings.Builder
	args []any
}

func (f *fragment) writeString(s string) { f.sb.WriteString(s) }

func (f *fragment) writeParam(v any) {
	// %d is materialised at the end via renumber; we emit a sentinel here.
	f.sb.WriteString(placeholderToken)
	f.args = append(f.args, v)
}

// placeholderToken is a string unlikely to appear inside SQL fragments
// emitted by this package. Replaced with $N positional markers at the end.
const placeholderToken = "\x01PG_PARAM\x01" //nolint:gosec

// build wires fragment text and arguments into a finalised query string with
// $1, $2, ... placeholders.
func (f *fragment) build() convertResult {
	text := f.sb.String()
	if len(f.args) == 0 {
		return convertResult{SQL: text}
	}
	var out strings.Builder
	out.Grow(len(text))
	idx := 0
	for {
		i := strings.Index(text, placeholderToken)
		if i < 0 {
			out.WriteString(text)
			break
		}
		out.WriteString(text[:i])
		idx++
		out.WriteByte('$')
		out.WriteString(strconv.Itoa(idx))
		text = text[i+len(placeholderToken):]
	}
	return convertResult{SQL: out.String(), Params: f.args}
}

// convert renders the boolean SQL expression that selects rows satisfying c.
// searchFields scopes the SEARCH operator to a set of dotted field paths.
//
// This is a port of src/snooze/db/postgres/convert.py — keep them in sync.
// (Phase 1A will lift most of this to internal/db/sql/Builder; for now it
// lives here so the Postgres driver compiles standalone.)
func convert(c condition.Cond, searchFields []string) (convertResult, error) {
	var f fragment
	if err := renderCond(&f, c, searchFields); err != nil {
		return convertResult{}, err
	}
	return f.build(), nil
}

func renderCond(f *fragment, c condition.Cond, searchFields []string) error {
	// AlwaysTrue (zero-value) matches everything.
	if c.IsZero() {
		f.writeString("TRUE")
		return nil
	}
	switch c.Op {
	case condition.OpAnd:
		return renderBoolean(f, c.Children, searchFields, "AND", "TRUE")
	case condition.OpOr:
		return renderBoolean(f, c.Children, searchFields, "OR", "FALSE")
	case condition.OpNot:
		if len(c.Children) == 0 {
			return fmt.Errorf("%w: NOT requires one child", dbpkg.ErrBadCondition)
		}
		f.writeString("(NOT ")
		if err := renderCond(f, c.Children[0], searchFields); err != nil {
			return err
		}
		f.writeString(")")
		return nil
	case condition.OpEq:
		return renderEq(f, c.Field, c.Value)
	case condition.OpNeq:
		return renderNeq(f, c.Field, c.Value)
	case condition.OpGt, condition.OpGte, condition.OpLt, condition.OpLte:
		return renderCompare(f, c.Field, string(c.Op), c.Value)
	case condition.OpMatches:
		return renderMatches(f, c.Field, c.Value)
	case condition.OpExists:
		return renderExists(f, c.Field)
	case condition.OpContains:
		return renderContains(f, c.Field, c.Value)
	case condition.OpIn:
		return renderIn(f, c.Field, c.Value, searchFields)
	case condition.OpSearch:
		return renderSearch(f, c.Value, searchFields)
	}
	return fmt.Errorf("%w: operator %q", dbpkg.ErrBadCondition, c.Op)
}

func renderBoolean(f *fragment, children []condition.Cond, searchFields []string, sep, empty string) error {
	if len(children) == 0 {
		f.writeString(empty)
		return nil
	}
	f.writeString("(")
	for i, child := range children {
		if i > 0 {
			f.writeString(" ")
			f.writeString(sep)
			f.writeString(" ")
		}
		if err := renderCond(f, child, searchFields); err != nil {
			return err
		}
	}
	f.writeString(")")
	return nil
}

// pathText emits a JSONB navigation expression terminating in ->> (text).
// Numeric-looking path segments become integer indices so array elements are
// reachable: "a.1" -> data->'a'->>1.
func pathText(field string) string {
	parts := strings.Split(field, ".")
	var b strings.Builder
	b.WriteString("data")
	for i, p := range parts {
		if i == len(parts)-1 {
			b.WriteString("->>")
		} else {
			b.WriteString("->")
		}
		b.WriteString(jsonPathLiteral(p))
	}
	return b.String()
}

// pathJSON emits a JSONB navigation expression terminating in -> (jsonb).
func pathJSON(field string) string {
	parts := strings.Split(field, ".")
	var b strings.Builder
	b.WriteString("data")
	for _, p := range parts {
		b.WriteString("->")
		b.WriteString(jsonPathLiteral(p))
	}
	return b.String()
}

// jsonPathLiteral renders a single path component as either an integer index
// or a single-quoted SQL string literal. Integer detection mirrors the
// Python “lstrip('-').isdigit()“ rule.
func jsonPathLiteral(part string) string {
	if isIntLiteral(part) {
		return part
	}
	return sqlString(part)
}

func isIntLiteral(s string) bool {
	if s == "" {
		return false
	}
	rest := s
	if rest[0] == '-' {
		rest = rest[1:]
	}
	if rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sqlString quotes s as a Postgres string literal. Used only for static
// JSON path keys we control via the AST — never for user-supplied values
// (those go through placeholders).
func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// isNumeric reports whether v is a non-bool numeric type. JSON unmarshal
// gives us float64 by default; ints flow through too.
func isNumeric(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	}
	return false
}

func isBool(v any) bool {
	_, ok := v.(bool)
	return ok
}

// boolText returns Postgres' boolean text representation.
func boolText(v any) string {
	if v.(bool) {
		return "true"
	}
	return "false"
}

// firstSegment returns the top-level JSON key of a dotted path.
func firstSegment(field string) string {
	if i := strings.IndexByte(field, '.'); i >= 0 {
		return field[:i]
	}
	return field
}

func renderEq(f *fragment, field string, value any) error {
	if value == nil {
		// NULL match: either the navigated leaf is JSON null or the
		// top-level key is missing entirely.
		f.writeString("(")
		f.writeString(pathText(field))
		f.writeString(" IS NULL OR NOT (data ? ")
		f.writeParam(firstSegment(field))
		f.writeString("))")
		return nil
	}
	return renderTypedCompare(f, field, "=", value)
}

func renderNeq(f *fragment, field string, value any) error {
	if value == nil {
		f.writeString("(")
		f.writeString(pathText(field))
		f.writeString(" IS NOT NULL)")
		return nil
	}
	// NULL-safe inequality using IS DISTINCT FROM.
	f.writeString("(")
	switch {
	case isNumeric(value):
		f.writeString("(")
		f.writeString(pathText(field))
		f.writeString(")::numeric IS DISTINCT FROM ")
		f.writeParam(value)
	case isBool(value):
		f.writeString(pathText(field))
		f.writeString(" IS DISTINCT FROM ")
		f.writeParam(boolText(value))
	default:
		f.writeString(pathText(field))
		f.writeString(" IS DISTINCT FROM ")
		f.writeParam(fmt.Sprint(value))
	}
	f.writeString(")")
	return nil
}

func renderCompare(f *fragment, field, op string, value any) error {
	return renderTypedCompare(f, field, op, value)
}

// renderTypedCompare emits "(<expr> OP $N)" with the right cast for the
// operand type. The outer parens around the cast matter: data->>'k'::numeric
// parses as data->>('k'::numeric). Mirrors src/snooze/db/postgres/convert.py.
func renderTypedCompare(f *fragment, field, op string, value any) error {
	switch {
	case isBool(value):
		f.writeString("(")
		f.writeString(pathText(field))
		f.writeString(" ")
		f.writeString(op)
		f.writeString(" ")
		f.writeParam(boolText(value))
		f.writeString(")")
	case isNumeric(value):
		f.writeString("((")
		f.writeString(pathText(field))
		f.writeString(")::numeric ")
		f.writeString(op)
		f.writeString(" ")
		f.writeParam(value)
		f.writeString(")")
	default:
		f.writeString("(")
		f.writeString(pathText(field))
		f.writeString(" ")
		f.writeString(op)
		f.writeString(" ")
		f.writeParam(fmt.Sprint(value))
		f.writeString(")")
	}
	return nil
}

func renderMatches(f *fragment, field string, value any) error {
	f.writeString("(")
	f.writeString(pathText(field))
	f.writeString(" ~* ")
	f.writeParam(unsugarRegex(toString(value)))
	f.writeString(")")
	return nil
}

func renderExists(f *fragment, field string) error {
	if !strings.Contains(field, ".") {
		f.writeString("(data ? ")
		f.writeParam(field)
		f.writeString(")")
		return nil
	}
	f.writeString("(")
	f.writeString(pathJSON(field))
	f.writeString(" IS NOT NULL)")
	return nil
}

func renderContains(f *fragment, field string, value any) error {
	values := flattenList(value)
	if len(values) == 0 {
		f.writeString("FALSE")
		return nil
	}
	patterns := make([]string, 0, len(values))
	for _, v := range values {
		patterns = append(patterns, unsugarRegex(toString(v)))
	}
	// Two-branch union: either the scalar form of the field matches one of
	// the patterns, or the field is an array and some element matches.
	f.writeString("COALESCE((")
	f.writeString(pathText(field))
	f.writeString(" ~* ANY(")
	f.writeParam(patterns)
	f.writeString("::text[])) OR (jsonb_typeof(")
	f.writeString(pathJSON(field))
	f.writeString(") = 'array' AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(")
	f.writeString(pathJSON(field))
	f.writeString(") v WHERE v ~* ANY(")
	f.writeParam(patterns)
	f.writeString("::text[]))), false)")
	return nil
}

// dslOperators is the set of operators that can appear as the first element
// of a list literal to mark it as a nested condition (legacy list form).
var dslOperators = map[condition.Op]struct{}{
	condition.OpAnd: {}, condition.OpOr: {}, condition.OpNot: {},
	condition.OpEq: {}, condition.OpNeq: {}, condition.OpGt: {}, condition.OpGte: {},
	condition.OpLt: {}, condition.OpLte: {}, condition.OpMatches: {}, condition.OpExists: {},
	condition.OpContains: {}, condition.OpIn: {}, condition.OpSearch: {},
}

func renderIn(f *fragment, field string, value any, searchFields []string) error {
	// If value is a nested Cond, evaluate it against each array element.
	if sub, ok := value.(condition.Cond); ok {
		f.writeString("COALESCE((jsonb_typeof(")
		f.writeString(pathJSON(field))
		f.writeString(") = 'array' AND EXISTS (SELECT 1 FROM jsonb_array_elements(")
		f.writeString(pathJSON(field))
		f.writeString(") AS arr(data) WHERE ")
		if err := renderCond(f, sub, searchFields); err != nil {
			return err
		}
		f.writeString(")), false)")
		return nil
	}
	// Legacy: a list whose first element is a DSL operator is a sub-Cond.
	if l, ok := value.([]any); ok && len(l) > 0 {
		if s, ok := l[0].(string); ok {
			if _, isOp := dslOperators[condition.Op(s)]; isOp {
				// Unsupported in the new Cond AST — surface a clear error so
				// upstream callers know they need to wrap nested conds via
				// condition.Cond, not legacy lists.
				return fmt.Errorf("%w: IN with nested list-form condition is not supported in the Go driver", dbpkg.ErrBadCondition)
			}
		}
	}
	// Literal membership over either a scalar field or an array of scalars.
	items := flattenList(value)
	strs := make([]string, 0, len(items))
	for _, it := range items {
		strs = append(strs, toString(it))
	}
	f.writeString("COALESCE(")
	f.writeString(pathText(field))
	f.writeString(" = ANY(")
	f.writeParam(strs)
	f.writeString("::text[]) OR (jsonb_typeof(")
	f.writeString(pathJSON(field))
	f.writeString(") = 'array' AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(")
	f.writeString(pathJSON(field))
	f.writeString(") v WHERE v = ANY(")
	f.writeParam(strs)
	f.writeString("::text[]))), false)")
	return nil
}

func renderSearch(f *fragment, value any, searchFields []string) error {
	needle := toString(value)
	if len(searchFields) > 0 {
		f.writeString("(")
		for i, fld := range searchFields {
			if i > 0 {
				f.writeString(" OR ")
			}
			f.writeString("(")
			f.writeString(pathText(fld))
			f.writeString(" ~* ")
			f.writeParam(needle)
			f.writeString(")")
		}
		f.writeString(")")
		return nil
	}
	f.writeString("(data::text ~* ")
	f.writeParam(needle)
	f.writeString(")")
	return nil
}

// flattenList returns value as a slice of any. Singletons are wrapped.
func flattenList(value any) []any {
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		return v
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out
	default:
		return []any{v}
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// unsugarRegex mirrors snooze.utils.condition.unsugar_regex. The Python
// implementation strips Perl-style anchors of the form /pattern/ but
// otherwise passes the pattern through. The Go driver only emits this for
// MATCHES and CONTAINS so we keep the rewrite minimal and document parity.
func unsugarRegex(s string) string {
	if len(s) >= 2 && s[0] == '/' && s[len(s)-1] == '/' {
		return s[1 : len(s)-1]
	}
	return s
}

// renderOrderBy renders the ORDER BY clause for a dotted field path. The
// expression sorts numeric-looking values numerically first and falls back to
// lexicographic order, matching the Python backend.
func renderOrderBy(field string, asc bool) string {
	dir := "ASC"
	if !asc {
		dir = "DESC"
	}
	expr := pathText(field)
	return fmt.Sprintf(
		"ORDER BY CASE WHEN %s ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN (%s)::numeric END %s NULLS LAST, %s %s NULLS LAST",
		expr, expr, dir, expr, dir,
	)
}

// renderPagination renders LIMIT/OFFSET; page is 1-indexed.
func renderPagination(perPage, page int) string {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 1
	}
	return fmt.Sprintf("LIMIT %d OFFSET %d", perPage, (page-1)*perPage)
}
