package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// CountBy groups the rows matching cond by field and counts each group. The
// ->> projection yields SQL NULL for both a missing key and a JSON null, so
// those fold, with "", into the "" bucket.
func (d *Driver) CountBy(ctx context.Context, collection string, cond condition.Cond, field string) (map[string]int, error) {
	out := map[string]int{}
	if field == "" {
		return out, errors.New("postgres: count by: empty field")
	}
	// Fail-closed before the table-exist check, exactly like Search.
	if _, _, err := dbpkg.TenantScope(ctx, collection); err != nil {
		return out, fmt.Errorf("postgres: count by: %w", err)
	}
	table, err := d.tableIfExists(ctx, collection)
	if err != nil {
		return out, err
	}
	if table == "" {
		return out, nil
	}
	res, err := convert(ctx, collection, cond, d.getSearchFields(collection))
	if err != nil {
		return out, err
	}
	// pathText renders every path segment as an escaped literal, so the field
	// never reaches the statement as raw SQL.
	q := fmt.Sprintf("SELECT COALESCE(%s, '') AS k, count(*) FROM %s WHERE %s GROUP BY 1",
		pathText(field), quoteIdent(table), res.SQL)
	rows, err := d.pool.Query(ctx, q, res.Params...)
	if err != nil {
		return out, fmt.Errorf("postgres: count by %s: %w", collection, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			key string
			n   int
		)
		if err := rows.Scan(&key, &n); err != nil {
			return out, fmt.Errorf("postgres: scan count by: %w", err)
		}
		out[key] += n
	}
	return out, rows.Err()
}
