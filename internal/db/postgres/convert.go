package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/snoozeweb/snooze/internal/condition"
	sqlbuilder "github.com/snoozeweb/snooze/internal/db/sql"
)

// convertResult is the rendered SQL fragment plus its bound parameters.
type convertResult struct {
	SQL    string
	Params []any
}

// builder is the shared Cond → WHERE translator wired with the Postgres
// dialect. The boolean tree walk lives in internal/db/sql; the JSONB-specific
// leaf fragments live in dialect.go.
var builder = sqlbuilder.Builder{Dialect: dialect{}}

// convert renders the boolean SQL expression that selects rows satisfying c.
// searchFields scopes the SEARCH operator to a set of dotted field paths.
//
// This is a port of src/snooze/db/postgres/convert.py — keep them in sync. The
// Cond → WHERE translation is routed through the shared internal/db/sql.Builder
// (one walker for both SQL backends); Postgres-specific leaf SQL is provided by
// the dialect in dialect.go. ORDER BY, LIMIT/OFFSET and query assembly stay in
// this package (renderOrderBy / renderPagination and the driver).
// ctx and collection are required for tenant injection (resolved inside
// builder.Convert via db.TenantScope).
func convert(ctx context.Context, collection string, c condition.Cond, searchFields []string) (convertResult, error) {
	sql, params, err := builder.Convert(ctx, collection, c, searchFields)
	if err != nil {
		return convertResult{}, err
	}
	return convertResult{SQL: sql, Params: params}, nil
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
