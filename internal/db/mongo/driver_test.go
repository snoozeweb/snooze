package mongo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"

	"github.com/japannext/snooze/internal/condition"
	dbpkg "github.com/japannext/snooze/internal/db"
)

// startMongo spins up a single-node replica-set MongoDB via testcontainers and
// returns a connected Driver plus a cleanup function. Change-stream support
// (used by Watcher) requires the replica-set flag, so we always enable it.
func startMongo(t *testing.T) (*Driver, func()) {
	t.Helper()
	ctx := context.Background()
	container, err := tcmongo.Run(ctx, "mongo:7", tcmongo.WithReplicaSet("rs0"))
	if err != nil {
		t.Skipf("testcontainers mongo unavailable: %v", err)
	}
	uri, err := container.ConnectionString(ctx)
	require.NoError(t, err)
	d, err := New(ctx, Config{
		URI:                    uri,
		Database:               "snoozetest",
		ServerSelectionTimeout: 15 * time.Second,
	})
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		t.Fatalf("connect: %v", err)
	}
	cleanup := func() {
		_ = d.Close()
		_ = testcontainers.TerminateContainer(container)
	}
	return d, cleanup
}

// TestDriver_WriteSearchDelete exercises the round-trip Write→Search→Delete
// path; it's the smallest meaningful happy-path integration test.
func TestDriver_WriteSearchDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping under -short")
	}
	d, cleanup := startMongo(t)
	defer cleanup()
	ctx := context.Background()
	_, err := d.Write(ctx, "record", []dbpkg.Document{
		{"a": "1", "b": "2"},
	}, dbpkg.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
	docs, total, err := d.Search(ctx, "record", condition.Equals("a", "1"), dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, docs, 1)
	require.Equal(t, "2", docs[0]["b"])

	deleted, err := d.Delete(ctx, "record", condition.Equals("a", "1"), false)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
}

// TestDriver_GetOneNotFound makes sure the sentinel error mapping at the
// driver boundary works (mongo.ErrNoDocuments → db.ErrNotFound).
func TestDriver_GetOneNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping under -short")
	}
	d, cleanup := startMongo(t)
	defer cleanup()
	_, err := d.GetOne(context.Background(), "record", dbpkg.Document{"uid": "does-not-exist"})
	require.Error(t, err)
	require.True(t, errors.Is(err, dbpkg.ErrNotFound), "want ErrNotFound, got %v", err)
}

// TestDriver_BulkIncrementUpsert covers the bulk write path with upsert.
func TestDriver_BulkIncrementUpsert(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping under -short")
	}
	d, cleanup := startMongo(t)
	defer cleanup()
	ctx := context.Background()
	_, err := d.Write(ctx, "stat", []dbpkg.Document{
		{"name": "stat 1", "hits": int64(0)},
		{"name": "stat 2", "hits": int64(40)},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	ops := []dbpkg.IncrementOp{
		{Search: dbpkg.Document{"name": "stat 2"}, Deltas: map[string]int64{"hits": 2}},
		{Search: dbpkg.Document{"name": "stat 3"}, Deltas: map[string]int64{"hits": 1}},
	}
	require.NoError(t, d.BulkIncrement(ctx, "stat", ops, true))
	docs, _, err := d.Search(ctx, "stat", condition.Cond{}, dbpkg.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 3)
}
