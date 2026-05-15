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

	dbpkg "github.com/japannext/snooze/internal/db"
)

// CleanupTimeout drops every row whose (date_epoch + ttl) is in the past.
// Mirrors snooze.db.postgres.database.cleanup_timeout.
func (d *Driver) CleanupTimeout(ctx context.Context, collection string) (int, error) {
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
			"(COALESCE((data->>'date_epoch')::numeric, 0) + "+
			" COALESCE((data->>'ttl')::numeric, 0)) <= extract(epoch from now())",
		qt,
	)
	tag, err := d.pool.Exec(ctx, q)
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
			"(SELECT data->>'uid' FROM %s WHERE data ? 'uid')",
		quoteIdent(ct), quoteIdent(rt),
	)
	tag, err := d.pool.Exec(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupComments: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// CleanupOrphans drops every row whose `parents` array references a uid
// that no longer exists in the same collection. Mirrors the Python CTE.
func (d *Driver) CleanupOrphans(ctx context.Context, collection string) (int, error) {
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
			" AND jsonb_array_length(data->'parents') > 0"+
			"), missing AS ("+
			" SELECT parent FROM parents WHERE parent IS NOT NULL AND parent NOT IN ("+
			" SELECT data->>'uid' FROM %s WHERE data ? 'uid'"+
			" ))"+
			" DELETE FROM %s WHERE EXISTS ("+
			" SELECT 1 FROM jsonb_array_elements_text(data->'parents') p, missing m"+
			" WHERE p = m.parent)",
		qt, qt, qt,
	)
	tag, err := d.pool.Exec(ctx, q)
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
			"AND jsonb_array_length(data->'time_constraints'->'datetime') > 0",
		qt,
	)
	rows, err := d.pool.Query(ctx, q)
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
	q := fmt.Sprintf(
		"WITH latest AS ("+
			"  SELECT DISTINCT ON (data->>'object_id') "+
			"    data->>'object_id' AS oid, "+
			"    data->>'action' AS action, "+
			"    COALESCE((data->>'date_epoch')::numeric, 0) AS de "+
			"  FROM %s "+
			"  ORDER BY data->>'object_id', (data->>'timestamp')::timestamptz DESC NULLS LAST"+
			") "+
			"DELETE FROM %s WHERE data ? 'object_id' AND "+
			"data->>'object_id' IN ("+
			"  SELECT oid FROM latest WHERE action = 'deleted' AND de < $1"+
			")",
		qt, qt,
	)
	tag, err := d.pool.Exec(ctx, q, threshold)
	if err != nil {
		return 0, fmt.Errorf("postgres: cleanupAuditLogs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// RenumberField re-packs an integer-valued field so its values form a
// contiguous 0-based sequence ordered by the field's current numeric value.
func (d *Driver) RenumberField(ctx context.Context, collection, field string) error {
	table, err := d.tableIfExists(ctx, collection)
	if err != nil {
		return err
	}
	if table == "" {
		return nil
	}
	qt := quoteIdent(table)
	// field is a single key, not a dotted path — same as the Python helper.
	if field == "" {
		return fmt.Errorf("%w: renumberField needs a non-empty field", dbpkg.ErrValidation)
	}
	q := fmt.Sprintf(
		"WITH ranked AS ("+
			" SELECT uid, row_number() OVER ("+
			"  ORDER BY (data->>$1)::numeric ASC NULLS LAST, uid ASC"+
			" ) - 1 AS new_pos FROM %s"+
			") "+
			"UPDATE %s t SET "+
			"data = jsonb_set(t.data, ARRAY[$1], to_jsonb(r.new_pos), true), "+
			"updated_at = clock_timestamp() FROM ranked r WHERE r.uid = t.uid",
		qt, qt,
	)
	if _, err := d.pool.Exec(ctx, q, field); err != nil {
		return fmt.Errorf("postgres: renumberField: %w", err)
	}
	return nil
}

// ComputeStats aggregates the per-bucket totals for stat-shaped records
// (date/key/value). Output buckets are formatted using the same
// "YYYY-MM-DDTHH:MM:OF" template as the Python backend.
func (d *Driver) ComputeStats(ctx context.Context, collection string, from, to time.Time, groupBy string) ([]dbpkg.StatsBucket, error) {
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
			" FROM %s WHERE (data->>'date')::timestamptz BETWEEN $1 AND $2"+
			") "+
			"SELECT to_char(date_trunc($3, d), 'YYYY-MM-DD\"T\"HH24:MI:OF') AS bucket, "+
			"k AS key, SUM(v) AS value FROM src GROUP BY bucket, k ORDER BY bucket",
		qt,
	)
	rows, err := d.pool.Query(ctx, q, from, to, trunc)
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("postgres: backup mkdir: %w", err)
	}
	target := filepath.Join(dir, collection+".json")
	tmp := target + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("postgres: backup create: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(docs); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("postgres: backup encode: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("postgres: backup close: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("postgres: backup rename: %w", err)
	}
	return nil
}
