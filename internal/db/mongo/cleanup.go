package mongo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// CleanupTimeout deletes every document whose `date_epoch + ttl` is past now.
// Mirrors Python's cleanup_timeout aggregation pipeline.
func (d *Driver) CleanupTimeout(ctx context.Context, collection string) (int, error) {
	now := float64(time.Now().UTC().Unix())
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"ttl": bson.M{"$gte": 0}}}},
		bson.D{{Key: "$project", Value: bson.M{
			"date_epoch": 1,
			"ttl":        1,
			"timeout":    bson.M{"$add": []any{"$date_epoch", "$ttl"}},
		}}},
		bson.D{{Key: "$match", Value: bson.M{"timeout": bson.M{"$lte": now}}}},
	}
	return d.deleteByPipeline(ctx, collection, pipeline)
}

// CleanupComments removes audit comments whose parent record has been deleted.
func (d *Driver) CleanupComments(ctx context.Context) (int, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.M{"_id": "$record_uid"}}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from":         "record",
			"foreignField": "uid",
			"localField":   "_id",
			"as":           "matched",
		}}},
		bson.D{{Key: "$match", Value: bson.M{"matched": bson.M{"$eq": []any{}}}}},
	}
	coll := d.coll("comment")
	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, fmt.Errorf("mongo: cleanup_comments aggregate: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	var orphans []any
	for cur.Next(ctx) {
		var row bson.M
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
		if id, ok := row["_id"]; ok && id != nil {
			orphans = append(orphans, id)
		}
	}
	if err := cur.Err(); err != nil {
		return 0, err
	}
	if len(orphans) == 0 {
		return 0, nil
	}
	res, err := coll.DeleteMany(ctx, bson.M{"record_uid": bson.M{"$in": orphans}})
	if err != nil {
		return 0, fmt.Errorf("mongo: cleanup_comments delete: %w", err)
	}
	return int(res.DeletedCount), nil
}

// CleanupOrphans deletes documents whose declared parent uid is missing.
func (d *Driver) CleanupOrphans(ctx context.Context, collection string) (int, error) {
	coll := d.coll(collection)
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$addFields", Value: bson.M{"parent": bson.M{"$last": "$parents"}}}},
		bson.D{{Key: "$group", Value: bson.M{"_id": nil, "parents": bson.M{"$addToSet": "$parent"}}}},
	}
	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, fmt.Errorf("mongo: cleanup_orphans aggregate: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	var parents []any
	for cur.Next(ctx) {
		var row bson.M
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
		if arr, ok := row["parents"].(bson.A); ok {
			for _, p := range arr {
				if p == nil {
					continue
				}
				parents = append(parents, p)
			}
		}
	}
	if err := cur.Err(); err != nil {
		return 0, err
	}
	if len(parents) == 0 {
		return 0, nil
	}
	var toDelete []any
	for _, parent := range parents {
		err := coll.FindOne(ctx, bson.M{"uid": parent}).Err()
		if err == nil {
			continue
		}
		if err == mongo.ErrNoDocuments { //nolint:errorlint
			toDelete = append(toDelete, parent)
			continue
		}
		return 0, fmt.Errorf("mongo: cleanup_orphans probe: %w", err)
	}
	if len(toDelete) == 0 {
		return 0, nil
	}
	res, err := coll.DeleteMany(ctx, bson.M{"parents": bson.M{"$in": toDelete}})
	if err != nil {
		return 0, fmt.Errorf("mongo: cleanup_orphans delete: %w", err)
	}
	return int(res.DeletedCount), nil
}

// CleanupSnooze deletes snooze rows whose `time_constraints.datetime` array
// is non-empty AND every element's `until` is strictly in the past. Rows
// with no datetime constraint, or with any future/open-ended entry, are
// kept. See db.Driver.CleanupSnooze for the contract.
func (d *Driver) CleanupSnooze(ctx context.Context) (int, error) {
	return d.cleanupExpiredByDatetime(ctx, "snooze")
}

// CleanupNotification mirrors CleanupSnooze for the `notification`
// collection.
func (d *Driver) CleanupNotification(ctx context.Context) (int, error) {
	return d.cleanupExpiredByDatetime(ctx, "notification")
}

// cleanupExpiredByDatetime is the body shared by CleanupSnooze and
// CleanupNotification. We scan every candidate row and evaluate the "every
// element's until is past" predicate in Go for parity with the
// SQLite/Postgres implementations.
func (d *Driver) cleanupExpiredByDatetime(ctx context.Context, collection string) (int, error) {
	coll := d.coll(collection)
	now := time.Now().UTC()
	cur, err := coll.Find(ctx, bson.M{
		"time_constraints.datetime.0": bson.M{"$exists": true},
	})
	if err != nil {
		return 0, fmt.Errorf("mongo: cleanupExpired %s: %w", collection, err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	var toDelete []any
	for cur.Next(ctx) {
		var row bson.M
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
		entries := extractDatetime(row)
		if datetimeAllExpired(entries, now) {
			if uid, ok := row["uid"]; ok && uid != nil {
				toDelete = append(toDelete, uid)
			}
		}
	}
	if err := cur.Err(); err != nil {
		return 0, err
	}
	if len(toDelete) == 0 {
		return 0, nil
	}
	res, err := coll.DeleteMany(ctx, bson.M{"uid": bson.M{"$in": toDelete}})
	if err != nil {
		return 0, fmt.Errorf("mongo: cleanupExpired %s: delete: %w", collection, err)
	}
	return int(res.DeletedCount), nil
}

// extractDatetime navigates the `time_constraints.datetime` array out of a
// raw decoded row. Returns nil when the path is absent or shaped unexpectedly.
//
// Decoding a document into bson.M yields nested documents as bson.D (ordered),
// not bson.M, so every level must tolerate bson.D / bson.M / map[string]any —
// otherwise the datetime entries are silently dropped and expiry cleanup
// never deletes anything.
func extractDatetime(row bson.M) []map[string]any {
	tc, ok := asBSONMap(row["time_constraints"])
	if !ok {
		return nil
	}
	var arr bson.A
	switch a := tc["datetime"].(type) {
	case bson.A:
		arr = a
	case []any:
		arr = bson.A(a)
	default:
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := asBSONMap(e)
		if !ok {
			// A non-document element means the list is malformed. The SQL
			// backends fail to unmarshal such a list and keep the row; match
			// that by bailing out (nil entries -> datetimeAllExpired == false).
			return nil
		}
		out = append(out, m)
	}
	return out
}

// asBSONMap normalises a value decoded by the bson library into a
// map[string]any, accepting bson.M, bson.D (ordered docs — the default for
// nested documents when decoding into bson.M) and plain map[string]any.
func asBSONMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case bson.M:
		return map[string]any(m), true
	case map[string]any:
		return m, true
	case bson.D:
		out := make(map[string]any, len(m))
		for _, e := range m {
			out[e.Key] = e.Value
		}
		return out, true
	}
	return nil, false
}

// datetimeAllExpired returns true when entries is non-empty and every
// element's `until` parses to a timestamp strictly before now. Missing or
// unparseable `until`, or any future/equal value, returns false.
func datetimeAllExpired(entries []map[string]any, now time.Time) bool {
	if len(entries) == 0 {
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

// CleanupAuditLogs deletes audit entries belonging to objects whose most
// recent event is a "delete" older than olderThan. ("delete" is the verb the
// audit emitter writes — see internal/plugins/crud.go; the UI relabels it
// "deleted".)
func (d *Driver) CleanupAuditLogs(ctx context.Context, olderThan time.Duration) (int, error) {
	now := float64(time.Now().UTC().Unix())
	threshold := now - olderThan.Seconds()
	// Prune every object whose max date_epoch is below the threshold AND has a
	// "delete" event at that max epoch. Using "a delete exists at the max epoch"
	// (rather than picking one arbitrary latest row) is deterministic and
	// matches the SQL backends on same-epoch create+delete ties. date_epoch is
	// the field audit writers populate.
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.M{
			"_id":      "$object_id",
			"maxEpoch": bson.M{"$max": "$date_epoch"},
			"events":   bson.M{"$push": bson.M{"action": "$action", "de": "$date_epoch"}},
		}}},
		bson.D{{Key: "$match", Value: bson.M{"maxEpoch": bson.M{"$lt": threshold}}}},
		bson.D{{Key: "$match", Value: bson.M{"$expr": bson.M{"$gt": bson.A{
			bson.M{"$size": bson.M{"$filter": bson.M{
				"input": "$events",
				"as":    "e",
				"cond": bson.M{"$and": bson.A{
					bson.M{"$eq": bson.A{"$$e.action", "delete"}},
					bson.M{"$eq": bson.A{"$$e.de", "$maxEpoch"}},
				}},
			}}},
			0,
		}}}}},
	}
	cur, err := d.coll("audit").Aggregate(ctx, pipeline)
	if err != nil {
		return 0, fmt.Errorf("mongo: cleanup_audit_logs aggregate: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	var ids []any
	for cur.Next(ctx) {
		var row bson.M
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
		if id, ok := row["_id"]; ok && id != nil {
			ids = append(ids, id)
		}
	}
	if err := cur.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := d.coll("audit").DeleteMany(ctx, bson.M{"object_id": bson.M{"$in": ids}})
	if err != nil {
		return 0, fmt.Errorf("mongo: cleanup_audit_logs delete: %w", err)
	}
	return int(res.DeletedCount), nil
}

// RenumberField rewrites a numeric field in ascending order starting at 0.
func (d *Driver) RenumberField(ctx context.Context, collection, field string) error {
	coll := d.coll(collection)
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$sort", Value: bson.M{field: 1}}},
		bson.D{{Key: "$group", Value: bson.M{"_id": 1, "tmp_items": bson.M{"$push": "$$ROOT"}}}},
		bson.D{{Key: "$unwind", Value: bson.M{"path": "$tmp_items", "includeArrayIndex": field}}},
		bson.D{{Key: "$replaceWith", Value: bson.M{"$mergeObjects": []any{"$tmp_items", bson.M{field: "$" + field}}}}},
		bson.D{{Key: "$merge", Value: bson.M{"into": collection, "on": "_id", "whenMatched": "replace"}}},
	}
	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("mongo: renumber_field: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	// Drain to ensure the $merge stage completes.
	for cur.Next(ctx) { //nolint:revive
	}
	return cur.Err()
}

// ComputeStats aggregates counter buckets by hour/day/month/year/week/weekday.
func (d *Driver) ComputeStats(ctx context.Context, collection string, from, to time.Time, groupBy string) ([]dbpkg.StatsBucket, error) {
	dateFormat := groupByFormat(groupBy)
	zone := from.Format("-0700")
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"$and": []bson.M{
			{"date": bson.M{"$gte": from}},
			{"date": bson.M{"$lte": to}},
		}}}},
		bson.D{{Key: "$addFields", Value: bson.M{"date_range": bson.M{
			"$dateToString": bson.M{"format": dateFormat, "timezone": zone, "date": "$date"},
		}}}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id":   bson.M{"id": "$date_range", "key": "$key"},
			"value": bson.M{"$sum": "$value"},
		}}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id":  "$_id.id",
			"data": bson.M{"$push": bson.M{"key": "$_id.key", "value": "$value"}},
		}}},
		bson.D{{Key: "$sort", Value: bson.M{"_id": 1}}},
	}
	cur, err := d.coll(collection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("mongo: compute_stats aggregate: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	var out []dbpkg.StatsBucket
	for cur.Next(ctx) {
		var row bson.M
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		bucket := dbpkg.StatsBucket{Bucket: fmt.Sprintf("%v", row["_id"])}
		if arr, ok := row["data"].(bson.A); ok {
			for _, e := range arr {
				if m, ok := e.(bson.M); ok {
					k, _ := m["key"].(string)
					v, _ := numeric(m["value"])
					bucket.Series = append(bucket.Series, dbpkg.KV{Key: k, Value: v})
				}
			}
		}
		out = append(out, bucket)
	}
	return out, cur.Err()
}

func groupByFormat(groupBy string) string {
	switch groupBy {
	case "hour":
		return "%Y-%m-%dT%H:00%z"
	case "day":
		return "%Y-%m-%dT00:00%z"
	case "month":
		return "%Y-%m-01T00:00%z"
	case "year":
		return "%Y-01-01T00:00%z"
	case "week":
		return "%Y-%VT00:00%z"
	case "weekday":
		return "%u"
	default:
		return "%Y-%m-%dT%H:00%z"
	}
}

// numeric coerces a few common BSON numeric representations to float64.
func numeric(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

// deleteByPipeline runs an aggregation pipeline and deletes every _id it
// returns. Used by CleanupTimeout to mimic the run_pipeline helper.
func (d *Driver) deleteByPipeline(ctx context.Context, collection string, pipeline mongo.Pipeline) (int, error) {
	coll := d.coll(collection)
	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, fmt.Errorf("mongo: aggregate: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	var ids []any
	for cur.Next(ctx) {
		var row bson.M
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
		if id, ok := row["_id"]; ok && id != nil {
			ids = append(ids, id)
		}
	}
	if err := cur.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return 0, fmt.Errorf("mongo: delete_many by pipeline: %w", err)
	}
	return int(res.DeletedCount), nil
}

// writeBackupFile is the OS-level helper for Backup. Centralised so tests can
// override it via a build-time hook if needed.
func writeBackupFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}
