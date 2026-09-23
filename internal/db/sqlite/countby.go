package sqlite

import (
	"context"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// CountBy groups the rows matching cond by field and counts each group. The
// value is read with json_extract and cast to TEXT, so a missing key and a JSON
// null both come back as SQL NULL and fold, with "", into the "" bucket.
func (d *Driver) CountBy(ctx context.Context, collection string, cond condition.Cond, field string) (map[string]int, error) {
	out := map[string]int{}
	if field == "" {
		return out, errors.New("sqlite: count by: empty field")
	}
	// Fail-closed before the table-exist check, exactly like Search.
	if _, _, err := dbpkg.TenantScope(ctx, collection); err != nil {
		return out, fmt.Errorf("sqlite: count by: %w", err)
	}
	exists, err := d.collectionExists(ctx, collection)
	if err != nil {
		return out, err
	}
	if !exists {
		return out, nil
	}
	tbl, err := tableName(collection)
	if err != nil {
		return out, err
	}
	where, args, err := d.compileWith(ctx, collection, cond)
	if err != nil {
		return out, err
	}
	//nolint:gosec // table name is validated by tableName/quoteIdent; the path is escaped by pathExpr.
	q := fmt.Sprintf("SELECT COALESCE(CAST(%s AS TEXT), '') AS k, count(*) FROM %s WHERE %s GROUP BY k",
		pathExpr(field, true), quoteIdent(tbl), where)
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out, fmt.Errorf("sqlite: count by: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var (
			key string
			n   int
		)
		if err := rows.Scan(&key, &n); err != nil {
			return out, err
		}
		out[key] += n
	}
	return out, rows.Err()
}
