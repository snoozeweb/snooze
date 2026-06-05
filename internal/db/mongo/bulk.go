package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// BulkIncrement applies a batch of search→delta operations in one BulkWrite.
// When upsert is true, missing documents are created with the search keys.
func (d *Driver) BulkIncrement(ctx context.Context, collection string, ops []dbpkg.IncrementOp, upsert bool) error {
	if len(ops) == 0 {
		return nil
	}
	// BulkIncrement filters are built directly from op.Search (not via Convert),
	// so tenant isolation must be injected by hand into both the match filter and
	// the $setOnInsert payload.
	tenantID, injectTenant, tenantErr := dbpkg.TenantScope(ctx, collection)
	if tenantErr != nil {
		return fmt.Errorf("mongo: BulkIncrement: %w", tenantErr)
	}
	models := make([]mongo.WriteModel, 0, len(ops))
	for _, op := range ops {
		filter := bson.M(cloneDoc(op.Search))
		if injectTenant {
			filter["tenant_id"] = tenantID
		}
		deltas := bson.M{}
		for f, v := range op.Deltas {
			deltas[f] = v
		}
		update := bson.M{"$inc": deltas}
		if upsert {
			setOnInsert := bson.M(cloneDoc(op.Search))
			if injectTenant {
				setOnInsert["tenant_id"] = tenantID
			}
			update["$setOnInsert"] = setOnInsert
		}
		m := mongo.NewUpdateOneModel().
			SetFilter(filter).
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
