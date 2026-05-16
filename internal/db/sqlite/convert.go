// Condition DSL → SQL WHERE translator for the SQLite backend.
//
// Structurally identical to the Postgres ``convert.py`` but emits JSON1
// expressions (``json_extract(data, '$.x')``) and positional ``?``
// placeholders. The regexp UDF registered by driver.go backs the MATCHES
// and CONTAINS operators with case-insensitive Go regexp semantics so the
// behaviour matches the Python ``re.search(pat, val, flags=re.IGNORECASE)``.

package sqlite

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/snoozeweb/snooze/internal/condition"
)

// compile lowers cond to a parenthesised SQL boolean expression plus the
// arguments to bind. An empty (zero) condition lowers to "1" (always true).
func compile(c condition.Cond, searchFields []string) (string, []any, error) {
	if c.IsZero() {
		return "1", nil, nil
	}
	b := &builder{}
	if err := b.emit(c, searchFields); err != nil {
		return "", nil, err
	}
	return b.sb.String(), b.args, nil
}

type builder struct {
	sb   strings.Builder
	args []any
}

func (b *builder) bind(v any) {
	b.args = append(b.args, v)
	b.sb.WriteString("?")
}

func (b *builder) write(s string) { b.sb.WriteString(s) }

// pathExpr produces a json_extract() reference to a dotted field. “terminal“
// selects the text-cast form used in comparisons; non-terminal returns the
// raw json (string/number/object/array as-is) for IS NULL checks and
// json_type() probes.
func pathExpr(field string, terminal bool) string {
	path := "$." + escapeJSONPath(field)
	_ = terminal // SQLite's json_extract returns the appropriate scalar/JSON either way.
	return "json_extract(data, '" + path + "')"
}

// jsonPathExpr always returns the JSON-typed extraction (no text coercion).
// Used when the SQL needs to feed the result into json_type, json_each, etc.
func jsonPathExpr(field string) string { return pathExpr(field, false) }

// escapeJSONPath converts a dotted path like "a.1.b" into a JSON-path tail
// suitable for splicing after "$.". Numeric segments become bracketed array
// indices: "a.1.b" -> "a[1].b".
//
// Single quotes inside identifier segments are doubled to keep the surrounding
// literal valid (we splice the path into '...' below; we don't bind it).
func escapeJSONPath(field string) string {
	parts := strings.Split(field, ".")
	var b strings.Builder
	for i, p := range parts {
		if isInt(p) && i > 0 {
			b.WriteString("[")
			b.WriteString(p)
			b.WriteString("]")
			continue
		}
		if i > 0 {
			b.WriteString(".")
		}
		b.WriteString(strings.ReplaceAll(p, "'", "''"))
	}
	return b.String()
}

func isInt(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' {
		s = s[1:]
		if s == "" {
			return false
		}
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// emit recursively writes the SQL fragment for c.
func (b *builder) emit(c condition.Cond, searchFields []string) error {
	switch c.Op {
	case condition.OpAlwaysTrue:
		b.write("1")
		return nil
	case condition.OpAnd:
		if len(c.Children) == 0 {
			b.write("1")
			return nil
		}
		b.write("(")
		for i, ch := range c.Children {
			if i > 0 {
				b.write(" AND ")
			}
			if err := b.emit(ch, searchFields); err != nil {
				return err
			}
		}
		b.write(")")
		return nil
	case condition.OpOr:
		if len(c.Children) == 0 {
			b.write("0")
			return nil
		}
		b.write("(")
		for i, ch := range c.Children {
			if i > 0 {
				b.write(" OR ")
			}
			if err := b.emit(ch, searchFields); err != nil {
				return err
			}
		}
		b.write(")")
		return nil
	case condition.OpNot:
		if len(c.Children) != 1 {
			return fmt.Errorf("sqlite: NOT expects 1 child, got %d", len(c.Children))
		}
		b.write("(NOT ")
		if err := b.emit(c.Children[0], searchFields); err != nil {
			return err
		}
		b.write(")")
		return nil
	case condition.OpEq:
		return b.emitEq(c.Field, c.Value, false)
	case condition.OpNeq:
		return b.emitEq(c.Field, c.Value, true)
	case condition.OpGt:
		return b.emitCmp(c.Field, ">", c.Value)
	case condition.OpGte:
		return b.emitCmp(c.Field, ">=", c.Value)
	case condition.OpLt:
		return b.emitCmp(c.Field, "<", c.Value)
	case condition.OpLte:
		return b.emitCmp(c.Field, "<=", c.Value)
	case condition.OpMatches:
		return b.emitMatches(c.Field, c.Value)
	case condition.OpExists:
		return b.emitExists(c.Field)
	case condition.OpContains:
		return b.emitContains(c.Field, c.Value)
	case condition.OpIn:
		return b.emitIn(c.Field, c.Value, searchFields)
	case condition.OpSearch:
		return b.emitSearch(c.Value, searchFields)
	}
	return fmt.Errorf("sqlite: unsupported op %q", c.Op)
}

func (b *builder) emitEq(field string, value any, negate bool) error {
	expr := pathExpr(field, true)
	if value == nil {
		// MATCHES Mongo's `=, null` semantics: the field is absent or null.
		if negate {
			b.write("(")
			b.write(expr)
			b.write(" IS NOT NULL)")
		} else {
			b.write("(")
			b.write(expr)
			b.write(" IS NULL)")
		}
		return nil
	}
	// SQLite's ``IS`` / ``IS NOT`` semantics are NULL-safe, matching the
	// Python ``Query == value`` predicate that returns False (not None)
	// when the field is missing. We emit IS / IS NOT for every scalar
	// type below.
	switch v := value.(type) {
	case bool:
		// JSON booleans round-trip as integers via json_extract(data->>'k')
		// only when the underlying JSON type is an integer; for a real
		// JSON boolean the extraction returns the int 0/1 already, so a
		// simple ``= 1``/``= 0`` works in either case.
		intVal := 0
		if v {
			intVal = 1
		}
		if negate {
			b.write("(")
			b.write(expr)
			b.write(" IS NOT ")
			b.bind(int64(intVal))
			b.write(")")
		} else {
			b.write("(")
			b.write(expr)
			b.write(" = ")
			b.bind(int64(intVal))
			b.write(")")
		}
		return nil
	case int, int8, int16, int32, int64, float32, float64:
		if negate {
			b.write("((")
			b.write(expr)
			b.write(") IS NOT ")
			b.bind(numericValue(v))
			b.write(")")
		} else {
			b.write("((")
			b.write(expr)
			b.write(") = ")
			b.bind(numericValue(v))
			b.write(")")
		}
		return nil
	case string:
		if negate {
			b.write("(")
			b.write(expr)
			b.write(" IS NOT ")
			b.bind(v)
			b.write(")")
		} else {
			b.write("(")
			b.write(expr)
			b.write(" = ")
			b.bind(v)
			b.write(")")
		}
		return nil
	}
	// Fallback: stringify any other Value type.
	if negate {
		b.write("(")
		b.write(expr)
		b.write(" IS NOT ")
		b.bind(fmt.Sprint(value))
		b.write(")")
	} else {
		b.write("(")
		b.write(expr)
		b.write(" = ")
		b.bind(fmt.Sprint(value))
		b.write(")")
	}
	return nil
}

func (b *builder) emitCmp(field, op string, value any) error {
	expr := pathExpr(field, true)
	b.write("((")
	b.write(expr)
	b.write(") ")
	b.write(op)
	b.write(" ")
	switch v := value.(type) {
	case int, int8, int16, int32, int64, float32, float64:
		b.bind(numericValue(v))
	case bool:
		i := int64(0)
		if v {
			i = 1
		}
		b.bind(i)
	default:
		b.bind(fmt.Sprint(v))
	}
	b.write(")")
	return nil
}

func (b *builder) emitMatches(field string, value any) error {
	// regexp(pattern, value) returns 1 on match. The UDF compiles
	// case-insensitive so we don't need to wrap the pattern here.
	pattern := fmt.Sprint(value)
	expr := pathExpr(field, true)
	b.write("(regexp(")
	b.bind(pattern)
	b.write(", ")
	b.write(expr)
	b.write(") = 1)")
	return nil
}

func (b *builder) emitExists(field string) error {
	// Use json_type(): returns NULL when the path doesn't resolve, and
	// 'null' when the JSON value is explicit null. EXISTS in the Python
	// code maps to "field is present", which we treat as "json_type is
	// not NULL" (covers explicit null too).
	b.write("(json_type(data, '$.")
	b.write(escapeJSONPath(field))
	b.write("') IS NOT NULL)")
	return nil
}

func (b *builder) emitContains(field string, value any) error {
	// CONTAINS: any of the (possibly list) values matches as a regex
	// (case-insensitive) against the field. Field may itself be a
	// scalar or a JSON array — we handle both via UNION of two
	// branches.
	values := toList(value)
	if len(values) == 0 {
		b.write("0")
		return nil
	}
	textExpr := pathExpr(field, true)
	jsonExpr := jsonPathExpr(field)

	b.write("(")
	for i, v := range values {
		if i > 0 {
			b.write(" OR ")
		}
		patStr := fmt.Sprint(v)
		// Scalar branch.
		b.write("(regexp(")
		b.bind(patStr)
		b.write(", ")
		b.write(textExpr)
		b.write(") = 1)")
		// Array branch (only consulted when the field is a JSON array).
		b.write(" OR (json_type(")
		b.write(jsonExpr)
		b.write(") = 'array' AND EXISTS (SELECT 1 FROM json_each(")
		b.write(jsonExpr)
		b.write(") je WHERE regexp(")
		b.bind(patStr)
		b.write(", je.value) = 1))")
	}
	b.write(")")
	return nil
}

func (b *builder) emitIn(field string, value any, _ []string) error {
	// Field membership: the field's value matches any element of the list
	// literal ``value``, OR (if the field is an array) any element of the
	// field array matches.
	//
	// The Python backend supports a recursive sub-condition form
	// (``['IN', ['=', 'foo', 'bar'], 'arrayfield']``) that evaluates the
	// nested condition against each array element. SQLite has no LATERAL
	// rename to bind the iteration variable as ``data``, so for now we
	// degrade to literal-list membership against the array's text values
	// when the caller passes a nested condition. The Mongo/Postgres
	// backends keep the full semantics; this is a known divergence
	// flagged for the dbtest matrix.
	if sub, ok := value.([]any); ok {
		return b.emitInLiteralList(field, sub)
	}
	if list, ok := value.([]string); ok {
		out := make([]any, len(list))
		for i, s := range list {
			out[i] = s
		}
		return b.emitInLiteralList(field, out)
	}
	return b.emitInLiteralList(field, []any{value})
}

func (b *builder) emitInLiteralList(field string, values []any) error {
	if len(values) == 0 {
		b.write("0")
		return nil
	}
	textExpr := pathExpr(field, true)
	jsonExpr := jsonPathExpr(field)
	b.write("(")
	// Scalar branch: the field's text matches any literal.
	b.write(textExpr)
	b.write(" IN (")
	for i, v := range values {
		if i > 0 {
			b.write(", ")
		}
		b.bind(fmt.Sprint(v))
	}
	b.write(")")
	// Array branch: at least one of the field's array elements matches.
	b.write(" OR (json_type(")
	b.write(jsonExpr)
	b.write(") = 'array' AND EXISTS (SELECT 1 FROM json_each(")
	b.write(jsonExpr)
	b.write(") je WHERE je.value IN (")
	for i, v := range values {
		if i > 0 {
			b.write(", ")
		}
		b.bind(fmt.Sprint(v))
	}
	b.write(")))")
	b.write(")")
	return nil
}

func (b *builder) emitSearch(value any, searchFields []string) error {
	needle := fmt.Sprint(value)
	if len(searchFields) == 0 {
		// Search the entire serialised document.
		b.write("(regexp(")
		b.bind(needle)
		b.write(", data) = 1)")
		return nil
	}
	b.write("(")
	for i, f := range searchFields {
		if i > 0 {
			b.write(" OR ")
		}
		b.write("regexp(")
		b.bind(needle)
		b.write(", ")
		b.write(pathExpr(f, true))
		b.write(") = 1")
	}
	b.write(")")
	return nil
}

// toList normalises a Value to []any. Strings, numbers, and bools become
// 1-element lists; nil becomes an empty list.
func toList(v any) []any {
	if v == nil {
		return nil
	}
	if l, ok := v.([]any); ok {
		return l
	}
	if l, ok := v.([]string); ok {
		out := make([]any, len(l))
		for i, s := range l {
			out[i] = s
		}
		return out
	}
	return []any{v}
}

// numericValue normalises numeric Go types to driver-friendly bind values.
func numericValue(v any) any {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case float32:
		return float64(x)
	case float64:
		return x
	case string:
		if n, err := strconv.ParseFloat(x, 64); err == nil {
			return n
		}
		return x
	}
	return v
}
