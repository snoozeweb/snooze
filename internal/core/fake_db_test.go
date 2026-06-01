package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
)

// capturedIncrement records a single BulkIncrement op captured by fakeDB.
type capturedIncrement struct {
	collection string
	op         db.IncrementOp
}

// fakeDB is a minimal in-memory db.Driver good enough for the core tests.
// It supports Search/GetOne/Write with primary-key upsert; everything else
// returns errUnsup or a sensible zero value.
type fakeDB struct {
	mu          sync.Mutex
	collections map[string][]db.Document
	writes      map[string]int // collection → write call count
	increments  []capturedIncrement
}

var errUnsup = errors.New("fakeDB: not implemented")

func newFakeDB() *fakeDB {
	return &fakeDB{
		collections: map[string][]db.Document{},
		writes:      map[string]int{},
	}
}

func (f *fakeDB) seed(collection string, docs ...db.Document) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range docs {
		cp := make(db.Document, len(d))
		for k, v := range d {
			cp[k] = v
		}
		if _, ok := cp["uid"]; !ok {
			cp["uid"] = uuid.NewString()
		}
		f.collections[collection] = append(f.collections[collection], cp)
	}
}

// docs returns a copy of the docs in collection.
func (f *fakeDB) docs(collection string) []db.Document {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.collections[collection]
	out := make([]db.Document, len(rows))
	copy(out, rows)
	return out
}

// writeCount returns the number of Write() calls that have targeted collection.
func (f *fakeDB) writeCount(collection string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes[collection]
}

// matchesDoc reports whether doc matches every key/value in filter.
func matchesDoc(doc, filter db.Document) bool {
	for k, want := range filter {
		got, ok := doc[k]
		if !ok || got != want {
			return false
		}
	}
	return true
}

func (f *fakeDB) Search(_ context.Context, collection string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.collections[collection]
	out := make([]db.Document, len(rows))
	copy(out, rows)
	return out, len(out), nil
}

func (f *fakeDB) GetOne(_ context.Context, collection string, match db.Document) (db.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, doc := range f.collections[collection] {
		if matchesDoc(doc, match) {
			cp := make(db.Document, len(doc))
			for k, v := range doc {
				cp[k] = v
			}
			return cp, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeDB) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, errUnsup
}

func (f *fakeDB) Write(_ context.Context, collection string, docs []db.Document, opts db.WriteOptions) (db.WriteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes[collection]++
	var res db.WriteResult
	for _, doc := range docs {
		var match db.Document
		if len(opts.Primary) > 0 {
			match = make(db.Document, len(opts.Primary))
			for _, k := range opts.Primary {
				match[k] = doc[k]
			}
		} else if uid, ok := doc["uid"].(string); ok && uid != "" {
			match = db.Document{"uid": uid}
		}
		idx := -1
		for i, existing := range f.collections[collection] {
			if matchesDoc(existing, match) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			for k, v := range doc {
				f.collections[collection][idx][k] = v
			}
			res.Updated = append(res.Updated, f.collections[collection][idx]["uid"].(string))
		} else {
			cp := make(db.Document, len(doc))
			for k, v := range doc {
				cp[k] = v
			}
			if _, ok := cp["uid"]; !ok {
				cp["uid"] = uuid.NewString()
			}
			f.collections[collection] = append(f.collections[collection], cp)
			res.Added = append(res.Added, cp["uid"].(string))
		}
	}
	return res, nil
}

func (f *fakeDB) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, errUnsup
}
func (f *fakeDB) UpdateOne(context.Context, string, string, db.Document, bool) error {
	return errUnsup
}
func (f *fakeDB) Delete(context.Context, string, condition.Cond, bool) (int, error) {
	return 0, errUnsup
}
func (f *fakeDB) BulkIncrement(_ context.Context, collection string, ops []db.IncrementOp, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, op := range ops {
		f.increments = append(f.increments, capturedIncrement{collection: collection, op: op})
	}
	return nil
}

// capturedIncrements returns a snapshot of all BulkIncrement ops for
// the given collection.
func (f *fakeDB) capturedIncrements(collection string) []capturedIncrement {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []capturedIncrement
	for _, ci := range f.increments {
		if ci.collection == collection {
			out = append(out, ci)
		}
	}
	return out
}
func (f *fakeDB) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, nil
}
func (f *fakeDB) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) CreateIndex(context.Context, string, []string) error { return nil }
func (f *fakeDB) ListCollections(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.collections))
	for k := range f.collections {
		out = append(out, k)
	}
	return out, nil
}
func (f *fakeDB) Drop(_ context.Context, collection string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.collections, collection)
	return nil
}
func (f *fakeDB) Backup(context.Context, string, []string) error      { return nil }
func (f *fakeDB) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (f *fakeDB) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (f *fakeDB) CleanupOrphans(context.Context, string) (int, error) {
	return 0, nil
}
func (f *fakeDB) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (f *fakeDB) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (f *fakeDB) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (f *fakeDB) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (f *fakeDB) RenumberField(context.Context, string, string) error { return nil }
func (f *fakeDB) Watcher() syncer.Bus                                 { return nil }
func (f *fakeDB) Close() error                                        { return nil }
