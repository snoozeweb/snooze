// Package mongo implements the db.Driver interface against MongoDB using the
// official go.mongodb.org/mongo-driver/v2 driver.
//
// One semantic departure from the Python implementation: the SEARCH operator
// no longer runs a JavaScript $where deep-iterate function (which is slow,
// blocks the agg server, and is forbidden on most managed MongoDBs). Instead,
// the driver translates SEARCH to an indexable $or over the per-collection
// list of search fields registered via CreateIndex. When no search fields are
// registered, SEARCH matches nothing.
//
// Change streams (used by Watcher) require the connected MongoDB to be a
// replica-set or sharded cluster; they fail on a standalone mongod.
package mongo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/google/uuid"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
)

// Config holds the connection parameters of the MongoDB Driver.
type Config struct {
	// URI is a MongoDB connection string (e.g. "mongodb://localhost:27017").
	URI string
	// Database is the logical database name; defaults to "snooze" when empty.
	Database string
	// ServerSelectionTimeout caps the time the driver waits for a usable
	// server before returning an error; defaults to 10s when zero.
	ServerSelectionTimeout time.Duration
}

// Driver is the MongoDB implementation of db.Driver.
type Driver struct {
	client       *mongo.Client
	db           *mongo.Database
	searchFields sync.Map // collection (string) -> []string
	bus          *mongoBus
	closeOnce    sync.Once
}

// New connects to MongoDB, pings the server, and returns a ready-to-use Driver.
func New(ctx context.Context, cfg Config) (*Driver, error) {
	if cfg.URI == "" {
		return nil, fmt.Errorf("mongo: empty URI")
	}
	if cfg.Database == "" {
		cfg.Database = "snooze"
	}
	if cfg.ServerSelectionTimeout == 0 {
		cfg.ServerSelectionTimeout = 10 * time.Second
	}
	opts := options.Client().
		ApplyURI(cfg.URI).
		SetServerSelectionTimeout(cfg.ServerSelectionTimeout)
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("mongo: connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ServerSelectionTimeout)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo: ping: %w", err)
	}
	d := &Driver{
		client: client,
		db:     client.Database(cfg.Database),
	}
	d.bus = newMongoBus(d)
	return d, nil
}

// Close disconnects from MongoDB. Idempotent.
func (d *Driver) Close() error {
	var err error
	d.closeOnce.Do(func() {
		if d.bus != nil {
			_ = d.bus.Close()
		}
		err = d.client.Disconnect(context.Background())
	})
	return err
}

// Watcher returns the per-driver change-stream-backed event bus.
func (d *Driver) Watcher() syncer.Bus { return d.bus }

// coll returns the *mongo.Collection handle for the given logical collection
// name.
func (d *Driver) coll(name string) *mongo.Collection {
	return d.db.Collection(name)
}

// searchFieldsFor returns the registered search fields for a collection or nil.
func (d *Driver) searchFieldsFor(collection string) []string {
	if v, ok := d.searchFields.Load(collection); ok {
		return v.([]string)
	}
	return nil
}

// CreateIndex records the list of fields used by SEARCH for the collection.
// MongoDB does not need an explicit index for $regex with a leading-anchored
// pattern to use an index, but for parity with the Python implementation the
// fields are simply remembered here.
func (d *Driver) CreateIndex(_ context.Context, collection string, fields []string) error {
	cp := make([]string, len(fields))
	copy(cp, fields)
	d.searchFields.Store(collection, cp)
	return nil
}

// ListCollections returns the names of every collection in the database.
func (d *Driver) ListCollections(ctx context.Context) ([]string, error) {
	names, err := d.db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("mongo: list collections: %w", err)
	}
	return names, nil
}

// Drop deletes a whole collection. No error if the collection does not exist.
func (d *Driver) Drop(ctx context.Context, collection string) error {
	if err := d.coll(collection).Drop(ctx); err != nil {
		return fmt.Errorf("mongo: drop %q: %w", collection, err)
	}
	return nil
}

// Convert compiles a condition.Cond into a bson.M filter suitable as a query.
func (d *Driver) Convert(_ context.Context, cond condition.Cond, searchFields []string) (dbpkg.DriverQuery, error) {
	q, err := Convert(cond, searchFields)
	if err != nil {
		return nil, err
	}
	return q, nil
}

// Search runs a find against the collection. Returns the page of documents and
// the total count.
func (d *Driver) Search(ctx context.Context, collection string, cond condition.Cond, page dbpkg.Page) ([]dbpkg.Document, int, error) {
	filter, err := Convert(cond, d.searchFieldsFor(collection))
	if err != nil {
		return nil, 0, err
	}
	coll := d.coll(collection)
	orderBy := page.OrderBy
	if orderBy == "" {
		orderBy = "_id"
	}
	dir := 1
	if !page.Asc {
		dir = -1
	}
	if page.OnlyOne {
		findOneOpts := options.FindOne().SetSort(bson.D{{Key: orderBy, Value: dir}})
		var out bson.M
		err := coll.FindOne(ctx, filter, findOneOpts).Decode(&out)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, fmt.Errorf("mongo: find_one: %w", err)
		}
		return []dbpkg.Document{decodeDoc(out)}, 1, nil
	}
	findOpts := options.Find().SetSort(bson.D{{Key: orderBy, Value: dir}})
	if page.PerPage > 0 {
		skip := int64(page.PageNb-1) * int64(page.PerPage)
		if skip < 0 {
			skip = 0
		}
		findOpts.SetSkip(skip).SetLimit(int64(page.PerPage))
	}
	cur, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, 0, fmt.Errorf("mongo: find: %w", err)
	}
	defer cur.Close(ctx) //nolint:errcheck
	var docs []dbpkg.Document
	for cur.Next(ctx) {
		var m bson.M
		if err := cur.Decode(&m); err != nil {
			return nil, 0, fmt.Errorf("mongo: decode: %w", err)
		}
		docs = append(docs, decodeDoc(m))
	}
	if err := cur.Err(); err != nil {
		return nil, 0, fmt.Errorf("mongo: cursor: %w", err)
	}
	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("mongo: count: %w", err)
	}
	return docs, int(total), nil
}

// GetOne returns the first document matching match, or db.ErrNotFound.
func (d *Driver) GetOne(ctx context.Context, collection string, match dbpkg.Document) (dbpkg.Document, error) {
	var out bson.M
	err := d.coll(collection).FindOne(ctx, bson.M(match)).Decode(&out)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, dbpkg.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mongo: get_one: %w", err)
	}
	return decodeDoc(out), nil
}

// Write inserts/updates/replaces a batch of documents. Implements the
// primary-key / duplicate-policy / constant-field semantics ported from the
// Python BackendDB.write.
func (d *Driver) Write(ctx context.Context, collection string, docs []dbpkg.Document, opts dbpkg.WriteOptions) (dbpkg.WriteResult, error) {
	res := dbpkg.WriteResult{}
	coll := d.coll(collection)
	updateTime := opts.UpdateTime
	policy := opts.DuplicatePolicy
	if policy == "" {
		policy = "update"
	}
	primary := opts.Primary
	constants := opts.Constant
	var toInsert []any
	now := time.Now().UTC().Unix()
	for _, raw := range docs {
		tobj := cloneDoc(raw)
		delete(tobj, "_id")
		delete(tobj, "_old")
		if updateTime {
			tobj["date_epoch"] = float64(now)
		}
		var primaryQuery bson.M
		var primaryResult bson.M
		if len(primary) > 0 && allPrimariesPresent(tobj, primary) {
			primaryQuery = buildPrimaryQuery(tobj, primary)
			pr := bson.M{}
			err := coll.FindOne(ctx, primaryQuery).Decode(&pr)
			if err == nil {
				primaryResult = pr
			} else if !errors.Is(err, mongo.ErrNoDocuments) {
				return res, fmt.Errorf("mongo: write find primary: %w", err)
			}
		}
		add := false
		if uidVal, ok := tobj["uid"].(string); ok && uidVal != "" {
			existing := bson.M{}
			err := coll.FindOne(ctx, bson.M{"uid": uidVal}).Decode(&existing)
			if errors.Is(err, mongo.ErrNoDocuments) {
				rej := dbpkg.Rejection{
					UID:     uidVal,
					Reason:  fmt.Sprintf("UID %s not found. Skipping...", uidVal),
					Payload: tobj,
				}
				res.Rejected = append(res.Rejected, rej)
				continue
			}
			if err != nil {
				return res, fmt.Errorf("mongo: write find uid: %w", err)
			}
			if primaryResult != nil && primaryResult["uid"] != uidVal {
				res.Rejected = append(res.Rejected, dbpkg.Rejection{
					UID:     uidVal,
					Reason:  fmt.Sprintf("found another document with same primary %v", primary),
					Payload: tobj,
				})
				continue
			}
			if len(constants) > 0 && anyConstantDiffers(existing, tobj, constants) {
				res.Rejected = append(res.Rejected, dbpkg.Rejection{
					UID:     uidVal,
					Reason:  fmt.Sprintf("constant field(s) %v changed", constants),
					Payload: tobj,
				})
				continue
			}
			if policy == "replace" {
				if _, err := coll.ReplaceOne(ctx, bson.M{"uid": uidVal}, bson.M(tobj)); err != nil {
					return res, fmt.Errorf("mongo: replace: %w", err)
				}
				res.Replaced = append(res.Replaced, uidVal)
			} else {
				if _, err := coll.UpdateOne(ctx, bson.M{"uid": uidVal}, bson.M{"$set": bson.M(tobj)}); err != nil {
					return res, fmt.Errorf("mongo: update: %w", err)
				}
				res.Updated = append(res.Updated, uidVal)
			}
		} else if len(primary) > 0 {
			if primaryResult != nil {
				if len(constants) > 0 && anyConstantDiffers(primaryResult, tobj, constants) {
					res.Rejected = append(res.Rejected, dbpkg.Rejection{
						Reason:  fmt.Sprintf("constant field(s) %v changed under primary %v", constants, primary),
						Payload: tobj,
					})
					continue
				}
				switch policy {
				case "insert":
					add = true
				case "reject":
					res.Rejected = append(res.Rejected, dbpkg.Rejection{
						Reason:  fmt.Sprintf("another document exists with the same %v", primary),
						Payload: tobj,
					})
					continue
				case "replace":
					if existingUID, ok := primaryResult["uid"].(string); ok && existingUID != "" {
						tobj["uid"] = existingUID
					}
					if _, err := coll.ReplaceOne(ctx, primaryQuery, bson.M(tobj)); err != nil {
						return res, fmt.Errorf("mongo: replace by primary: %w", err)
					}
					if uidStr, _ := tobj["uid"].(string); uidStr != "" {
						res.Replaced = append(res.Replaced, uidStr)
					} else {
						res.Replaced = append(res.Replaced, "")
					}
				default:
					if _, err := coll.UpdateOne(ctx, primaryQuery, bson.M{"$set": bson.M(tobj)}); err != nil {
						return res, fmt.Errorf("mongo: update by primary: %w", err)
					}
					if uidStr, _ := primaryResult["uid"].(string); uidStr != "" {
						res.Updated = append(res.Updated, uidStr)
					} else {
						res.Updated = append(res.Updated, "")
					}
				}
			} else {
				add = true
			}
		} else {
			add = true
		}
		if add {
			uidVal, _ := tobj["uid"].(string)
			if uidVal == "" {
				uidVal = uuid.NewString()
				tobj["uid"] = uidVal
			}
			toInsert = append(toInsert, bson.M(tobj))
			res.Added = append(res.Added, uidVal)
		}
	}
	if len(toInsert) > 0 {
		if _, err := coll.InsertMany(ctx, toInsert); err != nil {
			return res, fmt.Errorf("mongo: insert_many: %w", err)
		}
	}
	return res, nil
}

// ReplaceOne replaces a document. Returns the matched count.
func (d *Driver) ReplaceOne(ctx context.Context, collection string, match dbpkg.Document, doc dbpkg.Document, updateTime bool) (int, error) {
	newDoc := cloneDoc(doc)
	delete(newDoc, "_id")
	for k, v := range match {
		newDoc[k] = v
	}
	if updateTime {
		newDoc["date_epoch"] = float64(time.Now().UTC().Unix())
	}
	res, err := d.coll(collection).ReplaceOne(ctx, bson.M(match), bson.M(newDoc), options.Replace().SetUpsert(true))
	if err != nil {
		return 0, fmt.Errorf("mongo: replace_one: %w", err)
	}
	return int(res.MatchedCount), nil
}

// UpdateOne upserts a partial patch by uid.
func (d *Driver) UpdateOne(ctx context.Context, collection, uid string, patch dbpkg.Document, updateTime bool) error {
	newDoc := cloneDoc(patch)
	delete(newDoc, "_id")
	if updateTime {
		newDoc["date_epoch"] = float64(time.Now().UTC().Unix())
	}
	update := bson.M{
		"$set":         bson.M(newDoc),
		"$setOnInsert": bson.M{"uid": uid},
	}
	if _, err := d.coll(collection).UpdateOne(ctx, bson.M{"uid": uid}, update, options.UpdateOne().SetUpsert(true)); err != nil {
		return fmt.Errorf("mongo: update_one: %w", err)
	}
	return nil
}

// Delete removes every document matching cond. When the condition is empty
// (AlwaysTrue) and force is false the call refuses to wipe the collection.
func (d *Driver) Delete(ctx context.Context, collection string, cond condition.Cond, force bool) (int, error) {
	if cond.IsZero() && !force {
		return 0, nil
	}
	filter, err := Convert(cond, d.searchFieldsFor(collection))
	if err != nil {
		return 0, err
	}
	res, err := d.coll(collection).DeleteMany(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("mongo: delete_many: %w", err)
	}
	return int(res.DeletedCount), nil
}

// IncMany increments a field on every document matching cond.
func (d *Driver) IncMany(ctx context.Context, collection, field string, cond condition.Cond, delta int64) (int, error) {
	filter, err := Convert(cond, d.searchFieldsFor(collection))
	if err != nil {
		return 0, err
	}
	res, err := d.coll(collection).UpdateMany(ctx, filter, bson.M{"$inc": bson.M{field: delta}})
	if err != nil {
		return 0, fmt.Errorf("mongo: inc_many: %w", err)
	}
	return int(res.MatchedCount), nil
}

// SetFields sets every (field -> value) pair on the documents matching cond.
func (d *Driver) SetFields(ctx context.Context, collection string, fields dbpkg.Document, cond condition.Cond) (int, error) {
	filter, err := Convert(cond, d.searchFieldsFor(collection))
	if err != nil {
		return 0, err
	}
	res, err := d.coll(collection).UpdateMany(ctx, filter, bson.M{"$set": bson.M(fields)})
	if err != nil {
		return 0, fmt.Errorf("mongo: set_fields: %w", err)
	}
	return int(res.MatchedCount), nil
}

// AppendList pushes each value to the named list field for matching documents.
func (d *Driver) AppendList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	filter, err := Convert(cond, d.searchFieldsFor(collection))
	if err != nil {
		return 0, err
	}
	push := bson.M{}
	for f, vs := range fields {
		push[f] = bson.M{"$each": vs}
	}
	res, err := d.coll(collection).UpdateMany(ctx, filter, bson.M{"$push": push})
	if err != nil {
		return 0, fmt.Errorf("mongo: append_list: %w", err)
	}
	return int(res.ModifiedCount), nil
}

// PrependList inserts each value at position 0 of the named list field.
func (d *Driver) PrependList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	filter, err := Convert(cond, d.searchFieldsFor(collection))
	if err != nil {
		return 0, err
	}
	push := bson.M{}
	for f, vs := range fields {
		push[f] = bson.M{"$each": vs, "$position": 0}
	}
	res, err := d.coll(collection).UpdateMany(ctx, filter, bson.M{"$push": push})
	if err != nil {
		return 0, fmt.Errorf("mongo: prepend_list: %w", err)
	}
	return int(res.ModifiedCount), nil
}

// RemoveList removes each value from the named list field.
func (d *Driver) RemoveList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	filter, err := Convert(cond, d.searchFieldsFor(collection))
	if err != nil {
		return 0, err
	}
	pull := bson.M{}
	for f, vs := range fields {
		pull[f] = bson.M{"$in": vs}
	}
	res, err := d.coll(collection).UpdateMany(ctx, filter, bson.M{"$pull": pull})
	if err != nil {
		return 0, fmt.Errorf("mongo: remove_list: %w", err)
	}
	return int(res.ModifiedCount), nil
}

// Backup dumps every collection (minus excludes) as BSON-extended JSON files.
func (d *Driver) Backup(ctx context.Context, dir string, exclude []string) error {
	excl := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		excl[e] = struct{}{}
	}
	colls, err := d.ListCollections(ctx)
	if err != nil {
		return err
	}
	for _, c := range colls {
		if _, skip := excl[c]; skip {
			continue
		}
		if err := d.backupOne(ctx, dir, c); err != nil {
			return fmt.Errorf("mongo: backup %q: %w", c, err)
		}
	}
	return nil
}

func (d *Driver) backupOne(ctx context.Context, dir, collection string) error {
	cur, err := d.coll(collection).Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cur.Close(ctx) //nolint:errcheck
	var docs []bson.M
	for cur.Next(ctx) {
		var m bson.M
		if err := cur.Decode(&m); err != nil {
			return err
		}
		docs = append(docs, m)
	}
	if err := cur.Err(); err != nil {
		return err
	}
	data, err := bson.MarshalExtJSON(bson.M{"data": docs}, false, false)
	if err != nil {
		return err
	}
	return writeBackupFile(dir, collection+".json", data)
}

// cloneDoc shallow-copies a Document so callers' maps aren't mutated.
func cloneDoc(in dbpkg.Document) dbpkg.Document {
	out := make(dbpkg.Document, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// decodeDoc strips Mongo-only fields and returns a plain Document.
func decodeDoc(m bson.M) dbpkg.Document {
	delete(m, "_id")
	return dbpkg.Document(m)
}

// allPrimariesPresent reports whether every dotted primary path resolves to a
// non-zero value inside the document.
func allPrimariesPresent(doc dbpkg.Document, primary []string) bool {
	for _, p := range primary {
		if v, ok := digDot(doc, p); !ok || isZero(v) {
			return false
		}
	}
	return true
}

// buildPrimaryQuery constructs the bson filter selecting a primary tuple.
func buildPrimaryQuery(doc dbpkg.Document, primary []string) bson.M {
	if len(primary) == 1 {
		v, _ := digDot(doc, primary[0])
		return bson.M{primary[0]: v}
	}
	parts := make([]bson.M, 0, len(primary))
	for _, p := range primary {
		v, _ := digDot(doc, p)
		parts = append(parts, bson.M{p: v})
	}
	return bson.M{"$and": parts}
}

// anyConstantDiffers returns true if any constant field has a different value
// in the new vs old document.
func anyConstantDiffers(old, newDoc dbpkg.Document, constants []string) bool {
	for _, c := range constants {
		if !equalDeep(old[c], newDoc[c]) {
			return true
		}
	}
	return false
}

// equalDeep is a forgiving equality used to detect changes on constant fields.
// It mirrors Python's `!=` semantics closely enough for our test corpus:
// numeric types are compared by value, everything else by fmt.Sprintf.
func equalDeep(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// digDot resolves a dotted path inside doc (supports nested maps; integer
// path components select array elements when the field is a slice).
func digDot(doc dbpkg.Document, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var cur any = doc
	for _, p := range parts {
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[p]
			if !ok {
				return nil, false
			}
			cur = next
		default:
			return nil, false
		}
	}
	return cur, true
}

// isZero reports whether a primary-key value should be considered missing.
func isZero(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case int:
		return x == 0
	case int64:
		return x == 0
	case float64:
		return x == 0
	case bool:
		return !x
	}
	return false
}
