// Maintenance / cleanup helpers for the SQLite backend.
//
// Direct ports of the Postgres versions, swapping ``->>`` for
// ``json_extract(data, '$.<field>')`` and ``->`` for the same expression
// without a text cast. All single-statement where possible.

package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// CleanupTimeout deletes records whose “date_epoch + ttl“ has elapsed.
// Records without a “ttl“ field, without a “date_epoch“, or with a
// negative ttl are kept (matching the Mongo/legacy-Python contract).
func (d *Driver) CleanupTimeout(ctx context.Context, collection string) (int, error) {
	// Fail-closed before the existence check: a scoped collection with a naked
	// context must error, never silently run cross-tenant. [H3]
	tenantClause, tenantArgs, err := tenantPredicate(ctx, collection)
	if err != nil {
		return 0, fmt.Errorf("sqlite: cleanupTimeout: %w", err)
	}
	exists, err := d.collectionExists(ctx, collection)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	tbl, err := tableName(collection)
	if err != nil {
		return 0, err
	}
	stmt := fmt.Sprintf( //nolint:gosec
		`DELETE FROM %s WHERE
			json_extract(data, '$.ttl') IS NOT NULL
			AND CAST(json_extract(data, '$.ttl') AS REAL) >= 0
			AND json_extract(data, '$.date_epoch') IS NOT NULL
			AND (CAST(json_extract(data, '$.date_epoch') AS REAL)
				+ CAST(json_extract(data, '$.ttl') AS REAL))
			<= CAST(strftime('%%s','now') AS REAL)%s`,
		quoteIdent(tbl), tenantClause,
	)
	res, err := d.db.ExecContext(ctx, stmt, tenantArgs...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// tenantPredicate resolves the tenant scope for collection under ctx and returns
// a SQL fragment plus its args to AND into a WHERE clause. inject=false (platform
// scope or global collection) yields an empty clause; a naked context on a scoped
// collection fails closed with ErrNoTenant. The fragment selects the current
// table's tenant_id; supply tableAlias to qualify it (e.g. "a") when the query
// joins the same table to itself.
func tenantPredicate(ctx context.Context, collection string) (clause string, args []any, err error) {
	return tenantPredicateAlias(ctx, collection, "")
}

func tenantPredicateAlias(ctx context.Context, collection, alias string) (clause string, args []any, err error) {
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
	return fmt.Sprintf(" AND json_extract(%s, '$.tenant_id') = ?", col), []any{tenantID}, nil
}

// CleanupComments drops comment rows whose record_uid no longer resolves to
// an existing record. A no-op if either collection is missing.
func (d *Driver) CleanupComments(ctx context.Context) (int, error) {
	// Fail-closed: comment/record are tenant-scoped. Both the comments being
	// pruned and the records consulted for liveness must stay within the calling
	// tenant. [H3]
	commentClause, commentArgs, err := tenantPredicate(ctx, "comment")
	if err != nil {
		return 0, fmt.Errorf("sqlite: cleanupComments: %w", err)
	}
	recordClause, recordArgs, err := tenantPredicate(ctx, "record")
	if err != nil {
		return 0, fmt.Errorf("sqlite: cleanupComments: %w", err)
	}
	cExists, err := d.collectionExists(ctx, "comment")
	if err != nil {
		return 0, err
	}
	rExists, err := d.collectionExists(ctx, "record")
	if err != nil {
		return 0, err
	}
	if !cExists || !rExists {
		return 0, nil
	}
	ct, err := tableName("comment")
	if err != nil {
		return 0, err
	}
	rt, err := tableName("record")
	if err != nil {
		return 0, err
	}
	stmt := fmt.Sprintf( //nolint:gosec
		`DELETE FROM %s WHERE json_extract(data, '$.record_uid') NOT IN
			(SELECT json_extract(data, '$.uid') FROM %s
			 WHERE json_extract(data, '$.uid') IS NOT NULL%s)%s`,
		quoteIdent(ct), quoteIdent(rt), recordClause, commentClause,
	)
	// Args bind in statement order: the record sub-query predicate first, then
	// the outer comment predicate.
	args := append(append([]any{}, recordArgs...), commentArgs...)
	res, err := d.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CleanupOrphans drops rows whose “parents“ array references a non-existent
// ancestor in the same collection.
func (d *Driver) CleanupOrphans(ctx context.Context, collection string) (int, error) {
	// Fail-closed before the existence check. The candidate scan below is
	// tenant-scoped so only the calling tenant's rows are considered; the
	// parent-existence probe (d.GetOne) is itself tenant-scoped via Search. [H3]
	tenantClause, tenantArgs, err := tenantPredicate(ctx, collection)
	if err != nil {
		return 0, fmt.Errorf("sqlite: cleanupOrphans: %w", err)
	}
	exists, err := d.collectionExists(ctx, collection)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	tbl, err := tableName(collection)
	if err != nil {
		return 0, err
	}
	// Load all rows; collect distinct parents; check existence; delete
	// orphan rows. Done in Go because SQLite's lack of LATERAL joins makes
	// the single-statement form messy.
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT uid, json_extract(data, '$.parents') FROM %s "+
			"WHERE json_type(data, '$.parents') = 'array'%s",
		quoteIdent(tbl), tenantClause,
	), tenantArgs...)
	if err != nil {
		return 0, err
	}
	defer rows.Close() //nolint:errcheck
	type rowRef struct {
		uid     string
		parents []string
	}
	var refs []rowRef
	parents := map[string]struct{}{}
	for rows.Next() {
		var uid string
		var parentsJSON []byte
		if err := rows.Scan(&uid, &parentsJSON); err != nil {
			return 0, err
		}
		var list []any
		if err := json.Unmarshal(parentsJSON, &list); err != nil {
			continue
		}
		var ps []string
		for _, p := range list {
			s, ok := p.(string)
			if !ok {
				s = fmt.Sprint(p)
			}
			ps = append(ps, s)
			parents[s] = struct{}{}
		}
		refs = append(refs, rowRef{uid: uid, parents: ps})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(parents) == 0 {
		return 0, nil
	}

	// Check which parents exist.
	missing := map[string]struct{}{}
	for p := range parents {
		// Empty parent UID can't resolve to anything.
		if p == "" {
			missing[p] = struct{}{}
			continue
		}
		_, err := d.GetOne(ctx, collection, dbpkg.Document{"uid": p})
		if errors.Is(err, dbpkg.ErrNotFound) {
			missing[p] = struct{}{}
			continue
		}
		if err != nil {
			return 0, err
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}

	// Collect uids that reference a missing parent.
	var toDelete []string
	for _, r := range refs {
		for _, p := range r.parents {
			if _, ok := missing[p]; ok {
				toDelete = append(toDelete, r.uid)
				break
			}
		}
	}
	if len(toDelete) == 0 {
		return 0, nil
	}

	// Single DELETE … IN (...) statement, batched if the list is large.
	const batch = 500
	deleted := 0
	for i := 0; i < len(toDelete); i += batch {
		j := i + batch
		if j > len(toDelete) {
			j = len(toDelete)
		}
		chunk := toDelete[i:j]
		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = strings.TrimSuffix(placeholders, ",")
		stmt := fmt.Sprintf("DELETE FROM %s WHERE uid IN (%s)", quoteIdent(tbl), placeholders) //nolint:gosec
		args := make([]any, len(chunk))
		for i, s := range chunk {
			args[i] = s
		}
		res, err := d.db.ExecContext(ctx, stmt, args...)
		if err != nil {
			return deleted, err
		}
		n, _ := res.RowsAffected()
		deleted += int(n)
	}
	return deleted, nil
}

// CleanupSnooze deletes snooze rows whose `time_constraints.datetime` list
// has at least one entry AND every entry's `until` is in the past. Rows
// without any datetime constraint, or with at least one entry whose `until`
// is in the future / absent, are kept. See db.Driver.CleanupSnooze.
func (d *Driver) CleanupSnooze(ctx context.Context) (int, error) {
	return d.cleanupExpiredByDatetime(ctx, "snooze")
}

// CleanupNotification mirrors CleanupSnooze for the `notification`
// collection.
func (d *Driver) CleanupNotification(ctx context.Context) (int, error) {
	return d.cleanupExpiredByDatetime(ctx, "notification")
}

// cleanupExpiredByDatetime implements the shared CleanupSnooze /
// CleanupNotification body. We pull rows that declare a non-empty
// `time_constraints.datetime` array, evaluate each entry's `until` in Go
// (the JSON path expressions for "every element is in the past" aren't
// portable across the three backends), and DELETE by uid in batches.
func (d *Driver) cleanupExpiredByDatetime(ctx context.Context, collection string) (int, error) {
	// Fail-closed before the existence check; the candidate scan is tenant-scoped
	// so only the calling tenant's rows are considered. [H3]
	tenantClause, tenantArgs, err := tenantPredicate(ctx, collection)
	if err != nil {
		return 0, fmt.Errorf("sqlite: cleanupExpired %s: %w", collection, err)
	}
	exists, err := d.collectionExists(ctx, collection)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	tbl, err := tableName(collection)
	if err != nil {
		return 0, err
	}
	q := fmt.Sprintf( //nolint:gosec
		`SELECT uid, json_extract(data, '$.time_constraints.datetime') FROM %s
		 WHERE json_type(data, '$.time_constraints.datetime') = 'array'
		   AND json_array_length(json_extract(data, '$.time_constraints.datetime')) > 0%s`,
		quoteIdent(tbl), tenantClause,
	)
	rows, err := d.db.QueryContext(ctx, q, tenantArgs...)
	if err != nil {
		return 0, err
	}
	defer rows.Close() //nolint:errcheck
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
	const batch = 500
	deleted := 0
	for i := 0; i < len(toDelete); i += batch {
		j := i + batch
		if j > len(toDelete) {
			j = len(toDelete)
		}
		chunk := toDelete[i:j]
		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = strings.TrimSuffix(placeholders, ",")
		stmt := fmt.Sprintf("DELETE FROM %s WHERE uid IN (%s)", quoteIdent(tbl), placeholders) //nolint:gosec
		args := make([]any, len(chunk))
		for k, s := range chunk {
			args[k] = s
		}
		res, err := d.db.ExecContext(ctx, stmt, args...)
		if err != nil {
			return deleted, err
		}
		n, _ := res.RowsAffected()
		deleted += int(n)
	}
	return deleted, nil
}

// datetimeAllExpired returns true when `raw` is a JSON array of {until: ...}
// objects and every element has a valid `until` strictly before `now`. An
// empty array, a missing `until`, an unparseable `until`, or any
// future/equal `until` keeps the row.
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
			// Accept the short "YYYY-MM-DDTHH:MM" Python form too.
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

// CleanupAuditLogs prunes audit entries for objects whose latest event is
// a "delete" action older than the threshold. ("delete" is the verb the audit
// emitter writes — see internal/plugins/crud.go; the UI relabels it "deleted".)
func (d *Driver) CleanupAuditLogs(ctx context.Context, olderThan time.Duration) (int, error) {
	// Fail-closed. audit is tenant-scoped: an object_id is namespaced per tenant,
	// so the outer DELETE, the candidate sub-query (alias a) and the
	// max-epoch correlated sub-query (alias b) all carry the tenant predicate. [H3]
	outerClause, outerArgs, err := tenantPredicate(ctx, "audit")
	if err != nil {
		return 0, fmt.Errorf("sqlite: cleanupAuditLogs: %w", err)
	}
	aClause, aArgs, err := tenantPredicateAlias(ctx, "audit", "a")
	if err != nil {
		return 0, fmt.Errorf("sqlite: cleanupAuditLogs: %w", err)
	}
	bClause, bArgs, err := tenantPredicateAlias(ctx, "audit", "b")
	if err != nil {
		return 0, fmt.Errorf("sqlite: cleanupAuditLogs: %w", err)
	}
	exists, err := d.collectionExists(ctx, "audit")
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	tbl, err := tableName("audit")
	if err != nil {
		return 0, err
	}
	threshold := float64(time.Now().Add(-olderThan).Unix())
	// SQLite has no DISTINCT ON; emulate via a sub-query: for every
	// object_id with a 'delete' action older than threshold whose
	// date_epoch is the maximum for that object_id, delete every audit
	// row for that object_id.
	//nolint:gosec
	stmt := fmt.Sprintf(`
		DELETE FROM %s
		WHERE json_extract(data, '$.object_id') IN (
			SELECT json_extract(a.data, '$.object_id') FROM %s a
			WHERE json_extract(a.data, '$.action') = 'delete'
			  AND COALESCE(CAST(json_extract(a.data, '$.date_epoch') AS REAL), 0) < ?
			  AND COALESCE(CAST(json_extract(a.data, '$.date_epoch') AS REAL), 0) = (
				SELECT MAX(COALESCE(CAST(json_extract(b.data, '$.date_epoch') AS REAL), 0))
				FROM %s b
				WHERE json_extract(b.data, '$.object_id')
				  = json_extract(a.data, '$.object_id')%s
			  )%s
		)%s
	`, quoteIdent(tbl), quoteIdent(tbl), quoteIdent(tbl), bClause, aClause, outerClause)
	// Args bind in statement order: threshold (?), then inner b-predicate, then
	// candidate a-predicate, then the outer DELETE predicate.
	args := []any{threshold}
	args = append(args, bArgs...)
	args = append(args, aArgs...)
	args = append(args, outerArgs...)
	res, err := d.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ComputeStats aggregates a stats collection into time buckets. Result shape
// matches Mongo/Postgres so the API surface doesn't branch on driver.
func (d *Driver) ComputeStats(ctx context.Context, collection string, from, to time.Time, groupBy string) ([]dbpkg.StatsBucket, error) {
	// Fail-closed; aggregate only the calling tenant's stats rows. [H4]
	tenantClause, tenantArgs, err := tenantPredicate(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("sqlite: computeStats: %w", err)
	}
	exists, err := d.collectionExists(ctx, collection)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	tbl, err := tableName(collection)
	if err != nil {
		return nil, err
	}
	from = from.Truncate(time.Hour)

	// SQLite has no date_trunc; emulate via strftime patterns.
	format, ok := groupByFormats[groupBy]
	if !ok {
		format = groupByFormats["hour"]
	}
	//nolint:gosec
	stmt := fmt.Sprintf(`
		SELECT strftime(?, json_extract(data, '$.date')) AS bucket,
		       json_extract(data, '$.key') AS k,
		       SUM(COALESCE(CAST(json_extract(data, '$.value') AS REAL), 0)) AS v
		FROM %s
		WHERE json_extract(data, '$.date') BETWEEN ? AND ?%s
		GROUP BY bucket, k
		ORDER BY bucket
	`, quoteIdent(tbl), tenantClause)
	fromS := from.UTC().Format(time.RFC3339)
	toS := to.UTC().Format(time.RFC3339)

	rows, err := d.db.QueryContext(ctx, stmt, append([]any{format, fromS, toS}, tenantArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	grouped := make(map[string][]dbpkg.KV)
	order := []string{}
	for rows.Next() {
		var bucket, k string
		var v float64
		if err := rows.Scan(&bucket, &k, &v); err != nil {
			return nil, err
		}
		if _, seen := grouped[bucket]; !seen {
			order = append(order, bucket)
		}
		grouped[bucket] = append(grouped[bucket], dbpkg.KV{Key: k, Value: v})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(order)
	out := make([]dbpkg.StatsBucket, 0, len(order))
	for _, b := range order {
		out = append(out, dbpkg.StatsBucket{Bucket: b, Series: grouped[b]})
	}
	return out, nil
}

// RecordStats aggregates the `record` collection in SQL — one query for the
// time-series (bucketed by `bucketSec`, grouped by source) and one for the
// per-window totals (severity + environment). The two-query split keeps the
// SQL simple enough to read at a glance; total volume is the same.
//
// Returns empty maps (never nil) when the collection is absent.
func (d *Driver) RecordStats(ctx context.Context, from, to time.Time, bucketSec int64) (dbpkg.RecordStatsBuckets, error) {
	out := dbpkg.RecordStatsBuckets{
		Series:        map[int64]map[string]int64{},
		BySeverity:    map[string]int64{},
		ByEnvironment: map[string]int64{},
		ByState:       map[string]int64{},
	}
	if bucketSec <= 0 {
		return out, fmt.Errorf("bucketSec must be > 0")
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

	// Resolve tenant scope for the "record" collection.
	tenantID, injectTenant, tenantErr := dbpkg.TenantScope(ctx, "record")
	if tenantErr != nil {
		return out, fmt.Errorf("sqlite: RecordStats: %w", tenantErr)
	}

	fromEpoch, toEpoch := from.Unix(), to.Unix()

	// Build optional tenant predicate, appended to both WHERE clauses.
	tenantClause := ""
	var tenantArgs []any
	if injectTenant {
		tenantClause = " AND json_extract(data, '$.tenant_id') = ?"
		tenantArgs = []any{tenantID}
	}

	// Series: bucket-start (= epoch / stride * stride) and source → count.
	//nolint:gosec
	seriesStmt := fmt.Sprintf(`
		SELECT
		  CAST(COALESCE(json_extract(data, '$.date_epoch'), 0) AS INTEGER) / ? * ? AS slot,
		  COALESCE(json_extract(data, '$.source'), 'unknown') AS source,
		  COUNT(*) AS n
		FROM %s
		WHERE COALESCE(json_extract(data, '$.date_epoch'), 0) BETWEEN ? AND ?%s
		GROUP BY slot, source
	`, quoteIdent(tbl), tenantClause)
	rows, err := d.db.QueryContext(ctx, seriesStmt, append([]any{bucketSec, bucketSec, fromEpoch, toEpoch}, tenantArgs...)...)
	if err != nil {
		return out, fmt.Errorf("record stats: series: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var slot int64
		var source string
		var n int64
		if err := rows.Scan(&slot, &source, &n); err != nil {
			return out, err
		}
		bucket := out.Series[slot]
		if bucket == nil {
			bucket = map[string]int64{}
			out.Series[slot] = bucket
		}
		bucket[source] = n
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	// Totals: one row per (severity, environment, state); we reduce in Go.
	//nolint:gosec
	totalsStmt := fmt.Sprintf(`
		SELECT
		  COALESCE(NULLIF(json_extract(data, '$.severity'), ''), 'info')      AS sev,
		  COALESCE(NULLIF(json_extract(data, '$.environment'), ''), '(none)')  AS env,
		  COALESCE(NULLIF(json_extract(data, '$.state'), ''), 'open')          AS st,
		  COUNT(*) AS n
		FROM %s
		WHERE COALESCE(json_extract(data, '$.date_epoch'), 0) BETWEEN ? AND ?%s
		GROUP BY sev, env, st
	`, quoteIdent(tbl), tenantClause)
	rows2, err := d.db.QueryContext(ctx, totalsStmt, append([]any{fromEpoch, toEpoch}, tenantArgs...)...)
	if err != nil {
		return out, fmt.Errorf("record stats: totals: %w", err)
	}
	defer rows2.Close() //nolint:errcheck
	for rows2.Next() {
		var sev, env, st string
		var n int64
		if err := rows2.Scan(&sev, &env, &st, &n); err != nil {
			return out, err
		}
		out.BySeverity[sev] += n
		out.ByEnvironment[env] += n
		out.ByState[st] += n
	}
	return out, rows2.Err()
}

// groupByFormats maps the high-level bucket name to a strftime format string
// suitable for splicing into SQLite.
var groupByFormats = map[string]string{
	"hour":    "%Y-%m-%dT%H:00",
	"day":     "%Y-%m-%dT00:00",
	"month":   "%Y-%m-01T00:00",
	"year":    "%Y-01-01T00:00",
	"week":    "%Y-W%W",
	"weekday": "%w",
}
