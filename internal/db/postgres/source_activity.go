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
	// numericExpr, not a bare (data->>'date_epoch')::numeric: record.Validate
	// accepts any JSON value for date_epoch, and one alert carrying a
	// non-numeric one made the whole inputs page fail with "invalid input
	// syntax for type numeric" until that row was deleted. COALESCE(..., 0) is
	// kept, so such a row now counts under its source with last_epoch 0 —
	// exactly like a record with no date_epoch.
	//
	// Error tolerance is the ONLY benefit here. The COALESCE wrapper makes
	// this predicate un-indexable: Postgres matches an expression index by
	// comparing expression trees, "COALESCE(<expr>, 0)" is a different node
	// than "<expr>", and COALESCE is not strict so the planner cannot prove
	// the partial index's "<expr> IS NOT NULL" predicate either. This query
	// stays a full scan of `record` (it is an aggregate over every row anyway,
	// so it always was). Do not "align" it with the index — dropping the
	// COALESCE would change the aggregate's result for garbage rows.
	epoch := "COALESCE(" + numericExpr("date_epoch") + ", 0)"
	q := fmt.Sprintf(
		"SELECT COALESCE(NULLIF(data->>'source',''), 'unknown') AS source, "+
			"MAX(%s)::bigint AS last_epoch, "+
			"COUNT(*) AS n FROM %s "+
			"WHERE %s >= $1%s GROUP BY source",
		epoch, qt, epoch, tenantClause,
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
