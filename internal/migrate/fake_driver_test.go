package migrate

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

// fakeDriver is a minimal in-memory db.Driver for migration tests.
// It supports Search/GetOne/Write/SetFields/ListCollections; all else
// returns errUnsupported.
type fakeDriver struct {
	mu          sync.Mutex
	collections map[string][]db.Document
}

var errUnsupported = errors.New("fakeDriver: not implemented")

func newFakeDriver() *fakeDriver {
	return &fakeDriver{collections: map[string][]db.Document{}}
}

func (f *fakeDriver) seed(collection string, docs ...db.Document) {
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

func (f *fakeDriver) docs(collection string) []db.Document {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.collections[collection]
	out := make([]db.Document, len(rows))
	copy(out, rows)
	return out
}

func (f *fakeDriver) Search(_ context.Context, collection string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.collections[collection]
	out := make([]db.Document, len(rows))
	copy(out, rows)
	return out, len(out), nil
}

func (f *fakeDriver) GetOne(_ context.Context, collection string, match db.Document) (db.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, doc := range f.collections[collection] {
		ok := true
		for k, want := range match {
			if got, exists := doc[k]; !exists || got != want {
				ok = false
				break
			}
		}
		if ok {
			cp := make(db.Document, len(doc))
			for k, v := range doc {
				cp[k] = v
			}
			return cp, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeDriver) Write(_ context.Context, collection string, docs []db.Document, opts db.WriteOptions) (db.WriteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var res db.WriteResult
	for _, doc := range docs {
		idx := -1
		// Mirror the real backends: when the doc carries a uid that already
		// exists, update that row in place by uid regardless of the Primary
		// set (the Primary only governs uid-less upserts). This is what lets a
		// PK-rewrite write (Primary excludes uid) update a pre-existing row
		// instead of inserting a duplicate.
		if uid, ok := doc["uid"].(string); ok && uid != "" {
			for i, existing := range f.collections[collection] {
				if eu, eok := existing["uid"].(string); eok && eu == uid {
					idx = i
					break
				}
			}
		}
		if idx < 0 && len(opts.Primary) > 0 {
			matchFilter := make(db.Document, len(opts.Primary))
			for _, k := range opts.Primary {
				matchFilter[k] = doc[k]
			}
			for i, existing := range f.collections[collection] {
				ok := true
				for k, want := range matchFilter {
					if got, ex := existing[k]; !ex || got != want {
						ok = false
						break
					}
				}
				if ok {
					idx = i
					break
				}
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

// SetFields sets fields in place on every row matching cond, returning the
// count of matched rows. It honors the condition (unlike a naive set-all) so
// the migration's "only stamp rows lacking tenant_id" semantics are exercised —
// this is what keeps the dedup/idempotency tests meaningful.
func (f *fakeDriver) SetFields(_ context.Context, collection string, fields db.Document, cond condition.Cond) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.collections[collection]
	matched := 0
	for i := range rows {
		if !condMatches(cond, rows[i]) {
			continue
		}
		for k, v := range fields {
			rows[i][k] = v
		}
		matched++
	}
	return matched, nil
}

// condMatches is a minimal evaluator covering the operators the migration uses
// (the zero/AlwaysTrue match-all, NOT, EXISTS, =, AND). Anything else matches
// all rows so the fake never silently under-stamps.
func condMatches(c condition.Cond, doc db.Document) bool {
	switch c.Op {
	case condition.OpAlwaysTrue:
		return true
	case condition.OpExists:
		_, ok := doc[c.Field]
		return ok
	case condition.OpNot:
		if len(c.Children) == 0 {
			return true
		}
		return !condMatches(c.Children[0], doc)
	case condition.OpEq:
		v, ok := doc[c.Field]
		return ok && v == c.Value
	case condition.OpAnd:
		for _, ch := range c.Children {
			if !condMatches(ch, doc) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func (f *fakeDriver) ListCollections(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.collections))
	for k := range f.collections {
		out = append(out, k)
	}
	return out, nil
}

func (f *fakeDriver) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, errUnsupported
}
func (f *fakeDriver) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, errUnsupported
}
func (f *fakeDriver) UpdateOne(context.Context, string, string, db.Document, bool) error {
	return errUnsupported
}
func (f *fakeDriver) Delete(context.Context, string, condition.Cond, bool) (int, error) {
	return 0, errUnsupported
}
func (f *fakeDriver) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error {
	return errUnsupported
}
func (f *fakeDriver) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, errUnsupported
}
func (f *fakeDriver) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
	return 0, errUnsupported
}
func (f *fakeDriver) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, errUnsupported
}
func (f *fakeDriver) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, errUnsupported
}
func (f *fakeDriver) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, errUnsupported
}
func (f *fakeDriver) CreateIndex(context.Context, string, []string) error { return nil }
func (f *fakeDriver) Drop(_ context.Context, collection string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.collections, collection)
	return nil
}
func (f *fakeDriver) Backup(context.Context, string, []string) error      { return nil }
func (f *fakeDriver) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (f *fakeDriver) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (f *fakeDriver) CleanupOrphans(context.Context, string) (int, error) { return 0, nil }
func (f *fakeDriver) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (f *fakeDriver) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (f *fakeDriver) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (f *fakeDriver) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (f *fakeDriver) Watcher() syncer.Bus { return nil }
func (f *fakeDriver) Close() error        { return nil }
