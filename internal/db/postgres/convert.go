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
func pathText(field string) string { return pathTextOn("", field) }

// pathTextOn is pathText with an optional table alias on the data column, for
// the self-joining maintenance queries in cleanup.go ("a.data->>'x'"). alias
// must be a literal identifier written in this package, never user input.
func pathTextOn(alias, field string) string {
	parts := strings.Split(field, ".")
	var b strings.Builder
	if alias != "" {
		b.WriteString(alias)
		b.WriteString(".")
	}
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

// numericExpr emits the guarded numeric projection of a JSONB text leaf:
// the ::numeric cast when the stored text matches the guard, SQL NULL
// otherwise.
//
// This one helper is the single source of truth for "field as a number" in
// this package and MUST stay byte-identical across its consumers —
// dialect.typedCompare (range/equality predicates), dialect.Neq, renderOrderBy
// (numeric sort key), Driver.CreateIndex (the backing expression index and its
// partial predicate) and the maintenance queries in cleanup.go /
// source_activity.go / bulk.go. Postgres only matches an expression index when
// the query's expression is textually (post-parse) the same, so any divergence
// here silently downgrades every numeric filter and ORDER BY to a sequential
// scan.
//
// The CASE guard is not only about indexability: an unguarded
// (data->>'f')::numeric aborts the whole query with "invalid input syntax for
// type numeric" as soon as a single row holds non-numeric text in a
// numerically-compared field, and it makes CREATE INDEX fail outright for the
// same reason. Guarded, such a row yields NULL — it simply does not match.
//
// The accepted grammar is EXACTLY '^-?[0-9]+(\.[0-9]+)?$': an optional leading
// minus, one or more digits, and an optional fractional part of one or more
// digits. That is DELIBERATELY narrower than what a bare ::numeric cast
// accepts, and the difference is observable. All of these used to cast (and
// now compare as NULL, i.e. never match an ordinary comparison and sort last):
// leading/trailing whitespace (" 5", "5\n"), an explicit plus ("+5"),
// exponent notation ("1e5", "1E-5"), a bare-dot form (".5", "5."), the special
// values "NaN"/"Infinity"/"inf", and underscore digit separators ("1_000",
// Postgres 16+). Numbers stored as JSON numbers rather than JSON strings are
// unaffected — ->> renders them in the canonical form the guard accepts — so
// this only bites documents that stashed a hand-formatted numeric STRING.
// Widening the guard is not free: it must stay a single expression shared with
// the index, and any character class added here invalidates every existing
// expression index on disk.
func numericExpr(field string) string { return numericExprOn("", field) }

// numericExprOn is numericExpr with an optional table alias; see pathTextOn.
func numericExprOn(alias, field string) string {
	expr := pathTextOn(alias, field)
	return fmt.Sprintf("CASE WHEN %s ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN (%s)::numeric END", expr, expr)
}

// rfc3339Guard is the POSIX regex a JSONB text leaf must match before
// timestamptzExpr will cast it. It accepts exactly the shape Go's
// encoding/json produces for a time.Time (RFC 3339, optional fractional
// seconds, "Z" or a ±HH:MM offset) plus the offset-less "YYYY-MM-DDTHH:MM:SS"
// form and a space instead of the "T", which is what a hand-written or
// migrated row tends to carry. Calendar fields are range-checked, not merely
// counted: month 00/13, hour 25 and minute 60 are all rejected.
const rfc3339Guard = `^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])[T ]` +
	`([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]+)?` +
	`(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])?$`

// timestamptzExpr emits the guarded timestamptz projection of a JSONB text
// leaf: the ::timestamptz cast when the stored text matches rfc3339Guard, SQL
// NULL otherwise. It is the timestamp counterpart of numericExpr and exists
// for the same reason — a single row holding non-timestamp text in a
// timestamp-cast field used to abort the WHOLE statement with "invalid input
// syntax for type timestamp with time zone", and since the callers re-run the
// identical statement on a schedule, that abort repeats forever.
//
// The guard is a REGEX, so it is syntactic, and that leaves one documented
// residual gap: a syntactically well-formed but non-existent instant —
// "2026-02-31T00:00:00Z", "2025-02-29T00:00:00Z" — matches the guard and still
// raises "date/time field value out of range" on the cast. Closing that would
// need either a PL/pgSQL function with an exception block (server-side DDL
// this driver deliberately does not install) or to_timestamp() with an
// explicit format mask, which silently ROLLS such a value over (Feb 31 becomes
// Mar 3) and would therefore invent data. A regex that rejects every string
// timestamptz rejects does not exist, because leap years are not a regular
// language. What the guard does cover is the failure that actually happens:
// arbitrary text ("yesterday", "", "n/a"), numbers, and truncated or
// otherwise malformed timestamps.
func timestamptzExpr(field string) string {
	expr := pathText(field)
	return fmt.Sprintf("CASE WHEN %s ~ '%s' THEN (%s)::timestamptz END",
		expr, rfc3339Guard, expr)
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
	// The numeric sort key comes from numericExpr so it is byte-identical to
	// the predicate form and to the numeric expression index (see N2 in
	// docs/superpowers/plans/2026-09-08-notification-delivery-history-followups.md).
	return fmt.Sprintf(
		"ORDER BY %s %s NULLS LAST, %s %s NULLS LAST",
		numericExpr(field), dir, expr, dir,
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
