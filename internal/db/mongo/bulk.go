package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	dbpkg "github.com/japannext/snooze/internal/db"
)

// BulkIncrement applies a batch of search→delta operations in one BulkWrite.
// When upsert is true, missing documents are created with the search keys.
func (d *Driver) BulkIncrement(ctx context.Context, collection string, ops []dbpkg.IncrementOp, upsert bool) error {
	if len(ops) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(ops))
	for _, op := range ops {
		deltas := bson.M{}
		for f, v := range op.Deltas {
			deltas[f] = v
		}
		update := bson.M{"$inc": deltas}
		if upsert {
			update["$setOnInsert"] = bson.M(op.Search)
		}
		m := mongo.NewUpdateOneModel().
			SetFilter(bson.M(op.Search)).
			SetUpdate(update).
			SetUpsert(upsert)
		models = append(models, m)
	}
	_, err := d.coll(collection).BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		return fmt.Errorf("mongo: bulk_write: %w", err)
	}
	return nil
}
