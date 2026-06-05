package mongo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/dbtest"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestDriverSuite runs the shared cross-backend conformance suite. One
// container serves the whole suite; collections are dropped before each case
// for isolation (spinning a container per case would be far too slow).
func TestDriverSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping under -short")
	}
	drv, cleanup := startMongo(t)
	defer cleanup()
	dbtest.RunDriverSuite(t, "mongo", func(t *testing.T) (dbpkg.Driver, func()) {
		cols, err := drv.ListCollections(context.Background())
		require.NoError(t, err)
		for _, c := range cols {
			require.NoError(t, drv.Drop(context.Background(), c))
		}
		return drv, func() {}
	})
}

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
	ctx := snoozetypes.WithPlatformScope(context.Background())
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
	ctx := snoozetypes.WithPlatformScope(context.Background())
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

// TestDriver_UnsetFields verifies $unset truly removes the key so a subsequent
// EXISTS ($exists:true) query no longer matches — the portable field-delete a
// $set merge cannot provide.
func TestDriver_UnsetFields(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping under -short")
	}
	d, cleanup := startMongo(t)
	defer cleanup()
	ctx := snoozetypes.WithPlatformScope(context.Background())

	_, err := d.Write(ctx, "record", []dbpkg.Document{
		{"host": "h", "snoozed": "Warnings"},
		{"host": "h", "snoozed": "Warnings"},
		{"host": "h"},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	_, total, err := d.Search(ctx, "record", condition.Exists("snoozed"), dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 2, total)

	n, err := d.UnsetFields(ctx, "record", []string{"snoozed"}, condition.Equals("host", "h"))
	require.NoError(t, err)
	require.Equal(t, 2, n)

	_, total, err = d.Search(ctx, "record", condition.Exists("snoozed"), dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 0, total)

	n, err = d.UnsetFields(ctx, "record", []string{"snoozed"}, condition.Equals("host", "h"))
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

// TestDriver_UpdateOneToleratesUIDInPatch guards a mongo-only regression: the
// CRUD layer stamps the record uid onto the patch (so Validate/WriteTransformer
// can identify the row), and UpdateOne sets uid via $setOnInsert. If $set ALSO
// carries uid, Mongo rejects the write with "Updating the path 'uid' would
// create a conflict at 'uid'". UpdateOne must not $set the identity field.
func TestDriver_UpdateOneToleratesUIDInPatch(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping under -short")
	}
	d, cleanup := startMongo(t)
	defer cleanup()
	ctx := snoozetypes.WithPlatformScope(context.Background())

	_, err := d.Write(ctx, "record", []dbpkg.Document{{"uid": "u1", "x": int64(1)}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	// A patch that includes uid (matching the row) must NOT error.
	require.NoError(t, d.UpdateOne(ctx, "record", "u1", dbpkg.Document{"uid": "u1", "x": int64(2)}, true))

	docs, total, err := d.Search(ctx, "record", condition.Equals("uid", "u1"), dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, total, "must update the existing row, not insert a second")
	require.EqualValues(t, 2, docs[0]["x"])
	require.Equal(t, "u1", docs[0]["uid"])
}
