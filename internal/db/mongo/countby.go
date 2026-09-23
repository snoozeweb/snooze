package mongo

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/snoozeweb/snooze/internal/condition"
)

// CountBy groups the documents matching cond by field and counts each group.
// $ifNull maps both a missing key and a null to "", so they share the ""
// bucket with an explicit empty string.
func (d *Driver) CountBy(ctx context.Context, collection string, cond condition.Cond, field string) (map[string]int, error) {
	out := map[string]int{}
	if field == "" {
		return out, errors.New("mongo: count by: empty field")
	}
	// Convert carries the tenant predicate and fails closed on a naked
	// context, exactly as it does for Search.
	filter, err := Convert(ctx, collection, cond, d.searchFieldsFor(collection))
	if err != nil {
		return out, err
	}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: filter}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id": bson.M{"$ifNull": bson.A{"$" + field, ""}},
			"n":   bson.M{"$sum": 1},
		}}},
	}
	cur, err := d.coll(collection).Aggregate(ctx, pipeline)
	if err != nil {
		return out, fmt.Errorf("mongo: count by aggregate: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	for cur.Next(ctx) {
		var row struct {
			ID any `bson:"_id"`
			N  int `bson:"n"`
		}
		if err := cur.Decode(&row); err != nil {
			return out, fmt.Errorf("mongo: count by decode: %w", err)
		}
		key, ok := row.ID.(string)
		if !ok {
			key = fmt.Sprint(row.ID)
		}
		out[key] += row.N
	}
	return out, cur.Err()
}
