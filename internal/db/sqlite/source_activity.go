package sqlite

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
	// the `record` table does not exist yet (mirrors CleanupTimeout/CleanupOrphans).
	tenantID, injectTenant, tenantErr := dbpkg.TenantScope(ctx, "record")
	if tenantErr != nil {
		return out, fmt.Errorf("sqlite: SourceActivity: %w", tenantErr)
	}
	exists, err := d.collectionExists(ctx, "record")
	if err != nil {
		return out, err
	}
	if !exists {
		return out, nil
	}
	tbl, err := tableName("record")
	if err != nil {
		return out, err
	}
	tenantClause := ""
	var tenantArgs []any
	if injectTenant {
		tenantClause = " AND json_extract(data, '$.tenant_id') = ?"
		tenantArgs = []any{tenantID}
	}
	//nolint:gosec // table name is validated by tableName/quoteIdent.
	stmt := fmt.Sprintf(`
		SELECT
		  COALESCE(NULLIF(json_extract(data, '$.source'), ''), 'unknown') AS source,
		  MAX(CAST(COALESCE(json_extract(data, '$.date_epoch'), 0) AS INTEGER)) AS last_epoch,
		  COUNT(*) AS n
		FROM %s
		WHERE CAST(COALESCE(json_extract(data, '$.date_epoch'), 0) AS INTEGER) >= ?%s
		GROUP BY source
	`, quoteIdent(tbl), tenantClause)
	rows, err := d.db.QueryContext(ctx, stmt, append([]any{since}, tenantArgs...)...)
	if err != nil {
		return out, fmt.Errorf("sqlite: source activity: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var row dbpkg.SourceActivity
		if err := rows.Scan(&row.Source, &row.LastEpoch, &row.Count); err != nil {
			return out, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
