package postgres

import (
	"context"
	"fmt"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

var _ dbpkg.SourceActivityAggregator = (*Driver)(nil)

// SourceActivity aggregates the `record` collection by source: the max
// date_epoch and count per source at or after `since`. Returns an empty slice
// (never nil) when the collection is absent.
func (d *Driver) SourceActivity(ctx context.Context, since int64) ([]dbpkg.SourceActivity, error) {
	out := []dbpkg.SourceActivity{}
	// Resolve tenant scope FIRST so a naked context fails closed [H3] even when
	// the `record` table does not exist yet. tenant predicate binds as $2
	// (since is $1).
	tenantClause, tenantArgs, err := tenantPredicate(ctx, "record", "", 2)
	if err != nil {
		return out, fmt.Errorf("postgres: source activity: %w", err)
	}
	table, err := d.tableIfExists(ctx, "record")
	if err != nil {
		return out, err
	}
	if table == "" {
		return out, nil
	}
	qt := quoteIdent(table)
	q := fmt.Sprintf(
		"SELECT COALESCE(NULLIF(data->>'source',''), 'unknown') AS source, "+
			"MAX(COALESCE((data->>'date_epoch')::numeric, 0))::bigint AS last_epoch, "+
			"COUNT(*) AS n FROM %s "+
			"WHERE COALESCE((data->>'date_epoch')::numeric, 0) >= $1%s GROUP BY source",
		qt, tenantClause,
	)
	rows, err := d.pool.Query(ctx, q, append([]any{since}, tenantArgs...)...)
	if err != nil {
		return out, fmt.Errorf("postgres: source activity: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dbpkg.SourceActivity
		if err := rows.Scan(&row.Source, &row.LastEpoch, &row.Count); err != nil {
			return out, fmt.Errorf("postgres: scan source activity: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
