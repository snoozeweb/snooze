// Package db defines the Driver interface that every storage backend
// (Postgres, SQLite, MongoDB) implements, plus the shared value types.
package db

import (
	"context"
	"time"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/syncer"
)

// Document is a free-form record payload. The plugin layer wraps these in
// typed views (e.g. snoozetypes.Record) at the API boundary.
type Document = map[string]any

// Page describes how Search should slice and sort results.
//
// PerPage=0 means no limit. PageNb is 1-indexed to match Mongo conventions.
type Page struct {
	OrderBy string // field name; "" means insertion order (seq on SQL backends, _id on Mongo)
	PerPage int
	PageNb  int
	Asc     bool
	OnlyOne bool // short-circuit: stop after the first match
}

// WriteOptions controls Write semantics.
type WriteOptions struct {
	// Primary is the set of fields that, together, uniquely identify a record
	// for upsert purposes. Empty Primary means uid-based identity.
	Primary []string
	// Constant fields cannot be modified after insert (the driver rejects writes
	// that change them).
	Constant []string
	// DuplicatePolicy is "update" (default), "replace", "insert" (always create
	// a fresh uid), or "reject".
	DuplicatePolicy string
	// UpdateTime, when true, sets `updated_at` on every touched row. Default true.
	UpdateTime bool
}

// WriteResult lists the uids touched by Write. Payloads are not echoed back.
type WriteResult struct {
	Added    []string
	Updated  []string
	Replaced []string
	Rejected []Rejection
}

type Rejection struct {
	UID     string
	Reason  string
	Payload Document
}

// IncrementOp is a single search→delta pair for BulkIncrement.
type IncrementOp struct {
	Search Document         // match filter
	Deltas map[string]int64 // field → delta
}

// StatsBucket is a single time slice produced by ComputeStats.
type StatsBucket struct {
	Bucket string
	Series []KV
}

type KV struct {
	Key   string
	Value float64
}

// DriverQuery is an opaque per-driver compiled query, returned by Convert and
// fed back into driver methods that accept it. Drivers type-assert internally.
type DriverQuery any

// Driver is the contract every storage backend implements. Methods that return
// counts return -1 on a backend that cannot cheaply compute the count.
type Driver interface {
	// Query
	Search(ctx context.Context, collection string, cond condition.Cond, page Page) (docs []Document, total int, err error)
	GetOne(ctx context.Context, collection string, match Document) (Document, error) // returns (nil, ErrNotFound) on miss
	Convert(ctx context.Context, cond condition.Cond, searchFields []string) (DriverQuery, error)

	// CRUD
	Write(ctx context.Context, collection string, docs []Document, opts WriteOptions) (WriteResult, error)
	ReplaceOne(ctx context.Context, collection string, match Document, doc Document, updateTime bool) (matched int, err error)
	UpdateOne(ctx context.Context, collection, uid string, patch Document, updateTime bool) error
	Delete(ctx context.Context, collection string, cond condition.Cond, force bool) (deleted int, err error)

	// Bulk
	BulkIncrement(ctx context.Context, collection string, ops []IncrementOp, upsert bool) error
	IncMany(ctx context.Context, collection, field string, cond condition.Cond, delta int64) (matched int, err error)
	SetFields(ctx context.Context, collection string, fields Document, cond condition.Cond) (matched int, err error)
	AppendList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (matched int, err error)
	PrependList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (matched int, err error)
	RemoveList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (matched int, err error)

	// Maintenance
	CreateIndex(ctx context.Context, collection string, fields []string) error
	ListCollections(ctx context.Context) ([]string, error)
	Drop(ctx context.Context, collection string) error
	Backup(ctx context.Context, dir string, exclude []string) error
	CleanupTimeout(ctx context.Context, collection string) (deleted int, err error)
	CleanupComments(ctx context.Context) (deleted int, err error)
	CleanupOrphans(ctx context.Context, collection string) (deleted int, err error)
	CleanupAuditLogs(ctx context.Context, olderThan time.Duration) (deleted int, err error)
	ComputeStats(ctx context.Context, collection string, from, to time.Time, groupBy string) ([]StatsBucket, error)
	RenumberField(ctx context.Context, collection, field string) error

	// Watcher returns the per-driver event bus used by the syncer. SQLite returns
	// an in-process bus; Postgres a LISTEN/NOTIFY-backed bus; Mongo a
	// change-stream-backed bus.
	Watcher() syncer.Bus

	// Close releases resources. Idempotent.
	Close() error
}
