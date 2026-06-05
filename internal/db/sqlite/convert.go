// Condition DSL → SQL WHERE translator for the SQLite backend.
//
// The Cond → WHERE translation is routed through the shared
// internal/db/sql.Builder (one boolean-tree walker for both SQL backends);
// the SQLite-specific leaf SQL — JSON1 expressions (json_extract(data, '$.x')),
// positional ? placeholders, and the regexp UDF registered by driver.go that
// backs MATCHES/CONTAINS/SEARCH with case-insensitive Go regexp semantics — is
// provided by the dialect in dialect.go.

package sqlite

import (
	"context"
	"strconv"
	"strings"

	"github.com/snoozeweb/snooze/internal/condition"
	sqlbuilder "github.com/snoozeweb/snooze/internal/db/sql"
)

// builder is the shared Cond → WHERE translator wired with the SQLite dialect.
var builder = sqlbuilder.Builder{Dialect: dialect{}}

// compile lowers cond to a parenthesised SQL boolean expression plus the
// arguments to bind. An empty (zero) condition lowers to "1" (always true).
// ctx and collection are required for tenant injection (resolved inside
// builder.Convert via db.TenantScope).
func compile(ctx context.Context, collection string, c condition.Cond, searchFields []string) (string, []any, error) {
	return builder.Convert(ctx, collection, c, searchFields)
}

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
