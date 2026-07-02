package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

var _ dbpkg.SourceActivityAggregator = (*Driver)(nil)

// SourceActivity aggregates the `record` collection by source: the max
// date_epoch and count per source at or after `since`. A missing/empty
// `source` field groups under "unknown".
func (d *Driver) SourceActivity(ctx context.Context, since int64) ([]dbpkg.SourceActivity, error) {
	out := []dbpkg.SourceActivity{}
	tf, _, err := tenantFilter(ctx, "record")
	if err != nil {
		return out, fmt.Errorf("mongo: source activity: %w", err)
	}
	pipeline := prependMatch(tf, mongo.Pipeline{
		bson.D{{Key: "$set", Value: bson.M{
			"_src": bson.M{"$cond": bson.A{
				bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$source", ""}}, ""}},
				"unknown",
				"$source",
			}},
			"_epoch": bson.M{"$ifNull": bson.A{"$date_epoch", 0}},
		}}},
		bson.D{{Key: "$match", Value: bson.M{"$expr": bson.M{"$gte": bson.A{"$_epoch", since}}}}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id":       "$_src",
			"lastEpoch": bson.M{"$max": "$_epoch"},
			"count":     bson.M{"$sum": 1},
		}}},
	})
	cur, err := d.coll("record").Aggregate(ctx, pipeline)
	if err != nil {
		return out, fmt.Errorf("mongo: source activity aggregate: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	for cur.Next(ctx) {
		var row struct {
			ID        string  `bson:"_id"`
			LastEpoch float64 `bson:"lastEpoch"`
			Count     int64   `bson:"count"`
		}
		if err := cur.Decode(&row); err != nil {
			return out, err
		}
		src := row.ID
		if src == "" {
			src = "unknown"
		}
		// int64(row.LastEpoch) is lossless: Unix-second epochs are far below
		// float64's 2^53 exact-integer limit (SQLite/Postgres stay integer end-to-end).
		out = append(out, dbpkg.SourceActivity{Source: src, LastEpoch: int64(row.LastEpoch), Count: row.Count})
	}
	return out, cur.Err()
}
