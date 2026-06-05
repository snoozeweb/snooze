package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// tenantPredicate resolves the tenant scope for collection under ctx and returns
// a SQL fragment plus its single arg to AND into a WHERE clause, using the
// supplied placeholder index ($N). inject=false (platform scope or global
// collection) yields an empty clause; a naked context on a scoped collection
// fails closed with ErrNoTenant. Supply tableAlias to qualify the data column
// (e.g. "a") when the query joins the same table to itself.
func tenantPredicate(ctx context.Context, collection, alias string, placeholder int) (clause string, args []any, err error) {
	tenantID, inject, err := dbpkg.TenantScope(ctx, collection)
	if err != nil {
		return "", nil, err
	}
	if !inject {
		return "", nil, nil
	}
	col := "data"
	if alias != "" {
		col = alias + ".data"
	}
	return fmt.Sprintf(" AND %s->>'tenant_id' = $%d", col, placeholder), []any{tenantID}, nil
}

// CleanupTimeout drops every row whose (date_epoch + ttl) is in the past.
// Mirrors snooze.db.postgres.database.cleanup_timeout.
func (d *Driver) CleanupTimeout(ctx context.Context, collection string) (int, error) {
	// Fail-closed before the existence check: a scoped collection with a naked
	// context must error, never run cross-tenant. [H3]
	tenantClause, tenantArgs, err := tenantPredicate(ctx, collection, "", 1)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupTimeout %s: %w", collection, err)
	}
	table, err := d.tableIfExists(ctx, collection)
	if err != nil {
		return 0, err
	}
	if table == "" {
		return 0, nil
	}
	qt := quoteIdent(table)
	q := fmt.Sprintf(
		"DELETE FROM %s WHERE "+
			"(data->>'ttl')::numeric >= 0 AND "+
			"data ? 'date_epoch' AND "+
			"((data->>'date_epoch')::numeric + (data->>'ttl')::numeric) "+
			"<= extract(epoch from now())%s",
		qt, tenantClause,
	)
	tag, err := d.pool.Exec(ctx, q, tenantArgs...)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupTimeout %s: %w", collection, err)
	}
	deleted := int(tag.RowsAffected())
	if deleted > 0 {
		_ = notifyExec(ctx, d.pool, collection, "cleanup", nil)
	}
	return deleted, nil
}

// CleanupComments drops every comment row whose record_uid no longer
// resolves to a record.
func (d *Driver) CleanupComments(ctx context.Context) (int, error) {
	// Fail-closed: comment/record are tenant-scoped. Both the comments being
	// pruned and the records consulted for liveness stay within the calling
	// tenant. The record sub-query predicate appears first in the SQL text ($1),
	// the outer comment predicate second ($2). [H3]
	recordClause, recordArgs, err := tenantPredicate(ctx, "record", "", 1)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupComments: %w", err)
	}
	commentClause, commentArgs, err := tenantPredicate(ctx, "comment", "", len(recordArgs)+1)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupComments: %w", err)
	}
	cols, err := d.ListCollections(ctx)
	if err != nil {
		return 0, err
	}
	if !slices.Contains(cols, "comment") || !slices.Contains(cols, "record") {
		return 0, nil
	}
	ct, err := sanitizeCollection("comment")
	if err != nil {
		return 0, err
	}
	rt, err := sanitizeCollection("record")
	if err != nil {
		return 0, err
	}
	q := fmt.Sprintf(
		"DELETE FROM %s WHERE data->>'record_uid' NOT IN "+
			"(SELECT data->>'uid' FROM %s WHERE data ? 'uid'%s)%s",
		quoteIdent(ct), quoteIdent(rt), recordClause, commentClause,
	)
	args := append(append([]any{}, recordArgs...), commentArgs...)
	tag, err := d.pool.Exec(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupComments: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// CleanupOrphans drops every row whose `parents` array references a uid
// that no longer exists in the same collection. Mirrors the Python CTE.
func (d *Driver) CleanupOrphans(ctx context.Context, collection string) (int, error) {
	// Fail-closed; every reference to the table is tenant-scoped so only the
	// calling tenant's rows define both the parent set and the deletion set. The
	// three predicates bind in SQL-text order: parents CTE ($1), the missing-CTE
	// uid sub-query ($2), the outer DELETE ($3). [H3]
	parentsClause, parentsArgs, err := tenantPredicate(ctx, collection, "", 1)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupOrphans %s: %w", collection, err)
	}
	uidClause, uidArgs, err := tenantPredicate(ctx, collection, "", len(parentsArgs)+1)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupOrphans %s: %w", collection, err)
	}
	delClause, delArgs, err := tenantPredicate(ctx, collection, "", len(parentsArgs)+len(uidArgs)+1)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupOrphans %s: %w", collection, err)
	}
	table, err := d.tableIfExists(ctx, collection)
	if err != nil {
		return 0, err
	}
	if table == "" {
		return 0, nil
	}
	qt := quoteIdent(table)
	q := fmt.Sprintf(
		"WITH parents AS ("+
			" SELECT DISTINCT (data->'parents'->-1) #>> '{}' AS parent FROM %s"+
			" WHERE jsonb_typeof(data->'parents') = 'array'"+
			" AND jsonb_array_length(data->'parents') > 0%s"+
			"), missing AS ("+
			" SELECT parent FROM parents WHERE parent IS NOT NULL AND parent NOT IN ("+
			" SELECT data->>'uid' FROM %s WHERE data ? 'uid'%s"+
			" ))"+
			" DELETE FROM %s WHERE EXISTS ("+
			" SELECT 1 FROM jsonb_array_elements_text(data->'parents') p, missing m"+
			" WHERE p = m.parent)%s",
		qt, parentsClause, qt, uidClause, qt, delClause,
	)
	args := append(append(append([]any{}, parentsArgs...), uidArgs...), delArgs...)
	tag, err := d.pool.Exec(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupOrphans %s: %w", collection, err)
	}
	return int(tag.RowsAffected()), nil
}

// CleanupSnooze deletes snooze rows whose `time_constraints.datetime` JSON
// array is non-empty AND every element's `until` parses to a timestamp
// strictly before now. See db.Driver.CleanupSnooze for the contract.
func (d *Driver) CleanupSnooze(ctx context.Context) (int, error) {
	return d.cleanupExpiredByDatetime(ctx, "snooze")
}

// CleanupNotification mirrors CleanupSnooze for the `notification`
// collection.
func (d *Driver) CleanupNotification(ctx context.Context) (int, error) {
	return d.cleanupExpiredByDatetime(ctx, "notification")
}

// cleanupExpiredByDatetime is the body shared by CleanupSnooze and
// CleanupNotification. We fetch (uid, datetime array) for every candidate
// row and evaluate the "every element's until is past" predicate in Go;
// expressing it in pure SQL across jsonb_array_elements would be possible
// but harder to keep in sync with the SQLite/Mongo backends.
func (d *Driver) cleanupExpiredByDatetime(ctx context.Context, collection string) (int, error) {
	// Fail-closed; the candidate scan is tenant-scoped, so only the calling
	// tenant's rows can be deleted (the DELETE keys on the collected uids). [H3]
	tenantClause, tenantArgs, err := tenantPredicate(ctx, collection, "", 1)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupExpired %s: %w", collection, err)
	}
	table, err := d.tableIfExists(ctx, collection)
	if err != nil {
		return 0, err
	}
	if table == "" {
		return 0, nil
	}
	qt := quoteIdent(table)
	q := fmt.Sprintf(
		"SELECT uid, data->'time_constraints'->'datetime' FROM %s "+
			"WHERE jsonb_typeof(data->'time_constraints'->'datetime') = 'array' "+
			"AND jsonb_array_length(data->'time_constraints'->'datetime') > 0%s",
		qt, tenantClause,
	)
	rows, err := d.pool.Query(ctx, q, tenantArgs...)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupExpired %s: %w", collection, err)
	}
	defer rows.Close()
	now := time.Now().UTC()
	var toDelete []string
	for rows.Next() {
		var uid string
		var raw []byte
		if err := rows.Scan(&uid, &raw); err != nil {
			return 0, err
		}
		if datetimeAllExpired(raw, now) {
			toDelete = append(toDelete, uid)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(toDelete) == 0 {
		return 0, nil
	}
	tag, err := d.pool.Exec(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE uid = ANY($1)", qt),
		toDelete,
	)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupExpired %s: delete: %w", collection, err)
	}
	return int(tag.RowsAffected()), nil
}

// datetimeAllExpired matches the predicate used by CleanupSnooze /
// CleanupNotification: every element of the JSON array must have a
// parseable `until` strictly before `now`. Returns false for empty arrays,
// missing/unparseable `until`s, or any future/equal `until`.
func datetimeAllExpired(raw []byte, now time.Time) bool {
	if len(raw) == 0 {
		return false
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		untilRaw, ok := e["until"]
		if !ok {
			return false
		}
		untilStr, ok := untilRaw.(string)
		if !ok || untilStr == "" {
			return false
		}
		t, err := time.Parse(time.RFC3339, untilStr)
		if err != nil {
			if t2, err2 := time.Parse("2006-01-02T15:04", untilStr); err2 == nil {
				t = t2.UTC()
			} else {
				return false
			}
		}
		if !t.Before(now) {
			return false
		}
	}
	return true
}

// CleanupAuditLogs drops audit rows for object_ids last marked deleted
// before now - olderThan.
func (d *Driver) CleanupAuditLogs(ctx context.Context, olderThan time.Duration) (int, error) {
	// Fail-closed. audit is tenant-scoped and object_id is namespaced per tenant,
	// so the outer DELETE, the candidate sub-query (alias a) and the max-epoch
	// correlated sub-query (alias b) all carry the tenant predicate. Placeholders:
	// $1 threshold, $2 inner-b, $3 candidate-a, $4 outer DELETE. [H3]
	bClause, bArgs, err := tenantPredicate(ctx, "audit", "b", 2)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupAuditLogs: %w", err)
	}
	aClause, aArgs, err := tenantPredicate(ctx, "audit", "a", 2+len(bArgs))
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupAuditLogs: %w", err)
	}
	outerClause, outerArgs, err := tenantPredicate(ctx, "audit", "", 2+len(bArgs)+len(aArgs))
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupAuditLogs: %w", err)
	}
	cols, err := d.ListCollections(ctx)
	if err != nil {
		return 0, err
	}
	if !slices.Contains(cols, "audit") {
		return 0, nil
	}
	at, err := sanitizeCollection("audit")
	if err != nil {
		return 0, err
	}
	qt := quoteIdent(at)
	threshold := float64(time.Now().Add(-olderThan).Unix())
	// Prune every object whose max date_epoch is below the threshold AND has a
	// 'delete' event at that max epoch. The "delete-at-max exists" form (rather
	// than picking one arbitrary latest row) is deterministic and matches the
	// SQLite/Mongo backends on same-epoch create+delete ties. 'delete' is the
	// verb the audit emitter writes (internal/plugins/crud.go).
	q := fmt.Sprintf(
		"DELETE FROM %s WHERE data->>'object_id' IN ("+
			"  SELECT a.data->>'object_id' FROM %s a"+
			"  WHERE a.data->>'action' = 'delete'"+
			"    AND COALESCE((a.data->>'date_epoch')::numeric, 0) < $1"+
			"    AND COALESCE((a.data->>'date_epoch')::numeric, 0) = ("+
			"      SELECT MAX(COALESCE((b.data->>'date_epoch')::numeric, 0))"+
			"      FROM %s b WHERE b.data->>'object_id' = a.data->>'object_id'%s"+
			"    )%s"+
			")%s",
		qt, qt, qt, bClause, aClause, outerClause,
	)
	args := []any{threshold}
	args = append(args, bArgs...)
	args = append(args, aArgs...)
	args = append(args, outerArgs...)
	tag, err := d.pool.Exec(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupAuditLogs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ComputeStats aggregates the per-bucket totals for stat-shaped records
// (date/key/value). Output buckets are formatted using the same
// "YYYY-MM-DDTHH:MM:OF" template as the Python backend.
func (d *Driver) ComputeStats(ctx context.Context, collection string, from, to time.Time, groupBy string) ([]dbpkg.StatsBucket, error) {
	// Fail-closed; aggregate only the calling tenant's stats rows. The predicate
	// lands inside the src CTE WHERE clause as $4 (after from/to/trunc). [H4]
	tenantClause, tenantArgs, err := tenantPredicate(ctx, collection, "", 4)
	if err != nil {
		return nil, fmt.Errorf("postgres: computeStats: %w", err)
	}
	table, err := d.tableIfExists(ctx, collection)
	if err != nil {
		return nil, err
	}
	if table == "" {
		return nil, nil
	}
	from = from.Truncate(time.Hour)
	trunc := groupByToTruncUnit(groupBy)
	qt := quoteIdent(table)
	q := fmt.Sprintf(
		"WITH src AS ("+
			" SELECT (data->>'date')::timestamptz AS d, "+
			"        data->>'key' AS k, "+
			"        COALESCE((data->>'value')::numeric, 0) AS v "+
			" FROM %s WHERE (data->>'date')::timestamptz BETWEEN $1 AND $2%s"+
			") "+
			"SELECT to_char(date_trunc($3, d), 'YYYY-MM-DD\"T\"HH24:MI:OF') AS bucket, "+
			"k AS key, SUM(v) AS value FROM src GROUP BY bucket, k ORDER BY bucket",
		qt, tenantClause,
	)
	rows, err := d.pool.Query(ctx, q, append([]any{from, to, trunc}, tenantArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("postgres: computeStats: %w", err)
	}
	defer rows.Close()
	grouped := map[string][]dbpkg.KV{}
	order := []string{}
	for rows.Next() {
		var bucket, key string
		var value float64
		if err := rows.Scan(&bucket, &key, &value); err != nil {
			return nil, fmt.Errorf("postgres: scan stats: %w", err)
		}
		if _, seen := grouped[bucket]; !seen {
			order = append(order, bucket)
		}
		grouped[bucket] = append(grouped[bucket], dbpkg.KV{Key: key, Value: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: stats rows: %w", err)
	}
	out := make([]dbpkg.StatsBucket, 0, len(order))
	for _, b := range order {
		series := grouped[b]
		sort.SliceStable(series, func(i, j int) bool { return series[i].Key < series[j].Key })
		out = append(out, dbpkg.StatsBucket{Bucket: b, Series: series})
	}
	return out, nil
}

func groupByToTruncUnit(g string) string {
	switch g {
	case "day":
		return "day"
	case "month":
		return "month"
	case "year":
		return "year"
	case "week":
		return "week"
	case "weekday":
		return "dow"
	case "":
		return "hour"
	default:
		return g // postgres accepts hour/minute/second too
	}
}

// backupSingleCollection dumps the named collection's rows to a JSON file at
// dir/<collection>.json. Pure data; no metadata.
func (d *Driver) backupSingleCollection(ctx context.Context, dir, collection string) error {
	table, err := sanitizeCollection(collection)
	if err != nil {
		return err
	}
	qt := quoteIdent(table)
	rows, err := d.pool.Query(ctx, fmt.Sprintf("SELECT data FROM %s", qt))
	if err != nil {
		return fmt.Errorf("postgres: backup query: %w", err)
	}
	defer rows.Close()
	docs := []dbpkg.Document{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return fmt.Errorf("postgres: scan backup: %w", err)
		}
		doc := dbpkg.Document{}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("postgres: decode backup: %w", err)
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres: backup rows: %w", err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("postgres: backup mkdir: %w", err)
	}
	target := filepath.Join(dir, collection+".json")
	tmp := target + ".tmp"
	f, err := os.Create(tmp) //nolint:gosec
	if err != nil {
		return fmt.Errorf("postgres: backup create: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(docs); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("postgres: backup encode: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("postgres: backup close: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("postgres: backup rename: %w", err)
	}
	return nil
}
