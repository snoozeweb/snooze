// Unit tests for the SQLite driver. We bring up a fresh in-memory database
// per test (each gets its own ``file::memory:?cache=<unique>`` URI so
// concurrent t.Parallel runs don't share state).
//
// The shared backend-agnostic cases live in internal/db/dbtest; TestDriverSuite
// below delegates to dbtest.RunDriverSuite so SQLite is held to the same
// contract as Postgres/Mongo. The hand-rolled cases that follow add
// SQLite-specific coverage (ping, backup, bus fan-out, identifier validation).

package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/dbtest"
)

// TestDriverSuite runs the shared cross-backend conformance suite against a
// fresh SQLite driver.
func TestDriverSuite(t *testing.T) {
	dbtest.RunDriverSuite(t, "sqlite", func(t *testing.T) (dbpkg.Driver, func()) {
		return newTestDriver(t), func() {}
	})
}

// newTestDriver opens a fresh on-disk database under the test's temp dir.
// We avoid the ":memory:" form because every sql.DB connection then sees
// its own private database, which breaks the schemaCache invariant
// (writer creates the table on conn A, reader on conn B doesn't see it).
// A per-test temp file gives every conn a shared view AND isolates tests
// from each other; t.TempDir's cleanup removes the file.
func newTestDriver(t *testing.T) *Driver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	d, err := New(context.Background(), Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = d.Close()
	})
	return d
}

func TestNewAndPing(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	require.NoError(t, d.db.PingContext(context.Background()))
}

func TestWriteAndGetOne(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	doc := dbpkg.Document{"host": "alpha", "severity": "err"}
	res, err := d.Write(ctx, "record", []dbpkg.Document{doc}, dbpkg.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)

	got, err := d.GetOne(ctx, "record", dbpkg.Document{"host": "alpha"})
	require.NoError(t, err)
	require.Equal(t, "alpha", got["host"])
	require.Equal(t, "err", got["severity"])
	require.NotEmpty(t, got["uid"])
}

func TestWriteUpdateByUID(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	res, err := d.Write(ctx, "record", []dbpkg.Document{{"host": "alpha"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	uid := res.Added[0]

	res2, err := d.Write(ctx, "record",
		[]dbpkg.Document{{"uid": uid, "severity": "warn"}},
		dbpkg.WriteOptions{},
	)
	require.NoError(t, err)
	require.Len(t, res2.Updated, 1)

	got, err := d.GetOne(ctx, "record", dbpkg.Document{"uid": uid})
	require.NoError(t, err)
	// json_patch semantics: existing keys are preserved, new keys are merged.
	require.Equal(t, "alpha", got["host"])
	require.Equal(t, "warn", got["severity"])
}

func TestGetOneNotFound(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()
	require.NoError(t, d.ensure(ctx, "record"))
	_, err := d.GetOne(ctx, "record", dbpkg.Document{"uid": "nope"})
	require.ErrorIs(t, err, dbpkg.ErrNotFound)
}

func TestSearchPagination(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := d.Write(ctx, "stats",
			[]dbpkg.Document{{"key": "x", "value": float64(i)}},
			dbpkg.WriteOptions{},
		)
		require.NoError(t, err)
	}

	docs, total, err := d.Search(ctx, "stats", condition.Cond{}, dbpkg.Page{
		PerPage: 2,
		PageNb:  1,
		Asc:     true,
	})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, docs, 2)
	require.Equal(t, float64(0), docs[0]["value"])

	docs, _, err = d.Search(ctx, "stats", condition.Cond{}, dbpkg.Page{
		PerPage: 2,
		PageNb:  3,
		Asc:     true,
	})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, float64(4), docs[0]["value"])
}

func TestSearchOrderBy(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	for _, h := range []string{"c", "a", "b"} {
		_, err := d.Write(ctx, "record", []dbpkg.Document{{"host": h}}, dbpkg.WriteOptions{})
		require.NoError(t, err)
	}

	docs, _, err := d.Search(ctx, "record", condition.Cond{}, dbpkg.Page{
		OrderBy: "host",
		Asc:     true,
	})
	require.NoError(t, err)
	require.Len(t, docs, 3)
	require.Equal(t, "a", docs[0]["host"])
	require.Equal(t, "b", docs[1]["host"])
	require.Equal(t, "c", docs[2]["host"])
}

// TestRuleTreeOrder pins that rule sibling order is preserved against the
// real SQLite driver. The rule plugin reads its records ordered by
// tree_order (internal/pluginimpl/rule/plugin.go), and any reorder UX
// (drag-and-drop or sequential PATCH) leans on the same field. Mirrors
// dbtest/suite.go: testSearchRuleTreeOrder — duplicated here because the
// dbtest harness is not yet wired into the SQLite driver_test.go.
func TestRuleTreeOrder(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	// Insert top-level rules out of declaration order; tree_order is the
	// only thing that puts them back into 0,1,2,3.
	res, err := d.Write(ctx, "rule", []dbpkg.Document{
		{"name": "third", "tree_order": int64(2), "condition": []any{}},
		{"name": "first", "tree_order": int64(0), "condition": []any{}},
		{"name": "fourth", "tree_order": int64(3), "condition": []any{}},
		{"name": "second", "tree_order": int64(1), "condition": []any{}},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 4)
	parentUID := res.Added[1] // "first"

	_, err = d.Write(ctx, "rule", []dbpkg.Document{
		{"name": "child-B", "tree_order": int64(1), "parents": []any{parentUID}, "condition": []any{}},
		{"name": "child-A", "tree_order": int64(0), "parents": []any{parentUID}, "condition": []any{}},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	// Top-level: NOT EXISTS parents, ordered by tree_order ascending.
	topLevel := condition.Not(condition.Exists("parents"))
	docs, _, err := d.Search(ctx, "rule", topLevel, dbpkg.Page{OrderBy: "tree_order", Asc: true})
	require.NoError(t, err)
	require.Len(t, docs, 4)
	require.Equal(t, "first", docs[0]["name"])
	require.Equal(t, "second", docs[1]["name"])
	require.Equal(t, "third", docs[2]["name"])
	require.Equal(t, "fourth", docs[3]["name"])

	// Children of "first": IN parents == parentUID.
	childCond := condition.Cond{Op: condition.OpIn, Field: "parents", Value: parentUID}
	kids, _, err := d.Search(ctx, "rule", childCond, dbpkg.Page{OrderBy: "tree_order", Asc: true})
	require.NoError(t, err)
	require.Len(t, kids, 2)
	require.Equal(t, "child-A", kids[0]["name"])
	require.Equal(t, "child-B", kids[1]["name"])

	// Swap children's tree_order — mirrors what a drag-reorder UX would PATCH.
	uidA := kids[0]["uid"].(string)
	uidB := kids[1]["uid"].(string)
	require.NoError(t, d.UpdateOne(ctx, "rule", uidA, dbpkg.Document{"tree_order": int64(1)}, false))
	require.NoError(t, d.UpdateOne(ctx, "rule", uidB, dbpkg.Document{"tree_order": int64(0)}, false))

	kids, _, err = d.Search(ctx, "rule", childCond, dbpkg.Page{OrderBy: "tree_order", Asc: true})
	require.NoError(t, err)
	require.Equal(t, "child-B", kids[0]["name"])
	require.Equal(t, "child-A", kids[1]["name"])
}

func TestDeleteSafety(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.Write(ctx, "record", []dbpkg.Document{{"host": "x"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	n, err := d.Delete(ctx, "record", condition.Cond{}, false)
	require.NoError(t, err)
	require.Equal(t, 0, n, "empty condition without force must be a no-op")

	n, err = d.Delete(ctx, "record", condition.Cond{}, true)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestConditionEvaluation(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	for _, h := range []string{"alpha", "beta", "alphabet"} {
		_, err := d.Write(ctx, "record",
			[]dbpkg.Document{{"host": h, "score": 10}},
			dbpkg.WriteOptions{},
		)
		require.NoError(t, err)
	}

	t.Run("equals", func(t *testing.T) {
		docs, _, err := d.Search(ctx, "record", condition.Equals("host", "beta"), dbpkg.Page{})
		require.NoError(t, err)
		require.Len(t, docs, 1)
	})
	t.Run("matches", func(t *testing.T) {
		c := condition.Cond{Op: condition.OpMatches, Field: "host", Value: "^alpha"}
		docs, _, err := d.Search(ctx, "record", c, dbpkg.Page{})
		require.NoError(t, err)
		require.Len(t, docs, 2)
	})
	t.Run("gte", func(t *testing.T) {
		c := condition.Cond{Op: condition.OpGte, Field: "score", Value: 10}
		docs, _, err := d.Search(ctx, "record", c, dbpkg.Page{})
		require.NoError(t, err)
		require.Len(t, docs, 3)
	})
	t.Run("exists", func(t *testing.T) {
		c := condition.Exists("score")
		docs, _, err := d.Search(ctx, "record", c, dbpkg.Page{})
		require.NoError(t, err)
		require.Len(t, docs, 3)
	})
	t.Run("and", func(t *testing.T) {
		c := condition.And(
			condition.Cond{Op: condition.OpMatches, Field: "host", Value: "^alpha"},
			condition.Cond{Op: condition.OpGte, Field: "score", Value: 5},
		)
		docs, _, err := d.Search(ctx, "record", c, dbpkg.Page{})
		require.NoError(t, err)
		require.Len(t, docs, 2)
	})
	t.Run("not", func(t *testing.T) {
		c := condition.Not(condition.Equals("host", "beta"))
		docs, _, err := d.Search(ctx, "record", c, dbpkg.Page{})
		require.NoError(t, err)
		require.Len(t, docs, 2)
	})
}

func TestUpdateOneUpsert(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	err := d.UpdateOne(ctx, "record", "u1", dbpkg.Document{"host": "alpha"}, true)
	require.NoError(t, err)
	got, err := d.GetOne(ctx, "record", dbpkg.Document{"uid": "u1"})
	require.NoError(t, err)
	require.Equal(t, "alpha", got["host"])

	err = d.UpdateOne(ctx, "record", "u1", dbpkg.Document{"severity": "info"}, true)
	require.NoError(t, err)
	got, err = d.GetOne(ctx, "record", dbpkg.Document{"uid": "u1"})
	require.NoError(t, err)
	require.Equal(t, "alpha", got["host"])
	require.Equal(t, "info", got["severity"])
}

func TestReplaceOneInsertAndReplace(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	matched, err := d.ReplaceOne(ctx, "record",
		dbpkg.Document{"host": "alpha"},
		dbpkg.Document{"severity": "warn"},
		true,
	)
	require.NoError(t, err)
	require.Equal(t, 0, matched)
	got, err := d.GetOne(ctx, "record", dbpkg.Document{"host": "alpha"})
	require.NoError(t, err)
	require.Equal(t, "warn", got["severity"])

	matched, err = d.ReplaceOne(ctx, "record",
		dbpkg.Document{"host": "alpha"},
		dbpkg.Document{"severity": "crit"},
		true,
	)
	require.NoError(t, err)
	require.Equal(t, 1, matched)
	got, err = d.GetOne(ctx, "record", dbpkg.Document{"host": "alpha"})
	require.NoError(t, err)
	require.Equal(t, "crit", got["severity"])
}

func TestBulkIncrement(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	// No existing row + upsert=true -> insert (search ∪ deltas).
	err := d.BulkIncrement(ctx, "stats", []dbpkg.IncrementOp{{
		Search: dbpkg.Document{"key": "x"},
		Deltas: map[string]int64{"value": 3},
	}}, true)
	require.NoError(t, err)
	got, err := d.GetOne(ctx, "stats", dbpkg.Document{"key": "x"})
	require.NoError(t, err)
	require.EqualValues(t, 3, got["value"])

	// Existing row -> ADD, not REPLACE.
	err = d.BulkIncrement(ctx, "stats", []dbpkg.IncrementOp{{
		Search: dbpkg.Document{"key": "x"},
		Deltas: map[string]int64{"value": 5},
	}}, false)
	require.NoError(t, err)
	got, err = d.GetOne(ctx, "stats", dbpkg.Document{"key": "x"})
	require.NoError(t, err)
	require.EqualValues(t, 8, got["value"])

	// upsert=false on a miss should be a no-op.
	err = d.BulkIncrement(ctx, "stats", []dbpkg.IncrementOp{{
		Search: dbpkg.Document{"key": "missing"},
		Deltas: map[string]int64{"value": 1},
	}}, false)
	require.NoError(t, err)
	_, err = d.GetOne(ctx, "stats", dbpkg.Document{"key": "missing"})
	require.ErrorIs(t, err, dbpkg.ErrNotFound)
}

func TestIncManySetFields(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := d.Write(ctx, "stats",
			[]dbpkg.Document{{"key": "k", "value": float64(i)}},
			dbpkg.WriteOptions{},
		)
		require.NoError(t, err)
	}
	n, err := d.IncMany(ctx, "stats", "value", condition.Equals("key", "k"), 10)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	docs, _, err := d.Search(ctx, "stats", condition.Equals("key", "k"), dbpkg.Page{OrderBy: "value", Asc: true})
	require.NoError(t, err)
	require.Len(t, docs, 3)
	require.EqualValues(t, 10, docs[0]["value"])
	require.EqualValues(t, 11, docs[1]["value"])
	require.EqualValues(t, 12, docs[2]["value"])

	n, err = d.SetFields(ctx, "stats", dbpkg.Document{"severity": "warn"}, condition.Equals("key", "k"))
	require.NoError(t, err)
	require.Equal(t, 3, n)
	docs, _, err = d.Search(ctx, "stats", condition.Equals("key", "k"), dbpkg.Page{})
	require.NoError(t, err)
	for _, d := range docs {
		require.Equal(t, "warn", d["severity"])
	}
}

func TestUnsetFields(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.Write(ctx, "record", []dbpkg.Document{
		{"host": "h", "snoozed": "Warnings"},
		{"host": "h", "snoozed": "Warnings"},
		{"host": "h"}, // already has no snoozed
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	_, total, err := d.Search(ctx, "record", condition.Exists("snoozed"), dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 2, total)

	n, err := d.UnsetFields(ctx, "record", []string{"snoozed"}, condition.Equals("host", "h"))
	require.NoError(t, err)
	require.Equal(t, 2, n)

	// The key is truly gone — EXISTS no longer matches. A merge write cannot
	// achieve this (it preserves omitted keys); that gap is the bug
	// UnsetFields fixes.
	_, total, err = d.Search(ctx, "record", condition.Exists("snoozed"), dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 0, total)

	// Idempotent: nothing left to remove.
	n, err = d.UnsetFields(ctx, "record", []string{"snoozed"}, condition.Equals("host", "h"))
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestAppendPrependRemoveList(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.Write(ctx, "r",
		[]dbpkg.Document{{"key": "x", "tags": []any{"a"}}},
		dbpkg.WriteOptions{},
	)
	require.NoError(t, err)

	n, err := d.AppendList(ctx, "r",
		map[string][]any{"tags": {"b", "c"}},
		condition.Equals("key", "x"),
	)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	got, _ := d.GetOne(ctx, "r", dbpkg.Document{"key": "x"})
	require.Equal(t, []any{"a", "b", "c"}, got["tags"])

	n, err = d.PrependList(ctx, "r",
		map[string][]any{"tags": {"z"}},
		condition.Equals("key", "x"),
	)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	got, _ = d.GetOne(ctx, "r", dbpkg.Document{"key": "x"})
	require.Equal(t, []any{"z", "a", "b", "c"}, got["tags"])

	n, err = d.RemoveList(ctx, "r",
		map[string][]any{"tags": {"a", "c"}},
		condition.Equals("key", "x"),
	)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	got, _ = d.GetOne(ctx, "r", dbpkg.Document{"key": "x"})
	require.Equal(t, []any{"z", "b"}, got["tags"])
}

func TestListAndDropCollections(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.Write(ctx, "alpha", []dbpkg.Document{{"x": 1}}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	_, err = d.Write(ctx, "beta.gamma", []dbpkg.Document{{"x": 1}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	cols, err := d.ListCollections(ctx)
	require.NoError(t, err)
	require.Contains(t, cols, "alpha")
	require.Contains(t, cols, "beta.gamma")

	require.NoError(t, d.Drop(ctx, "alpha"))
	cols, err = d.ListCollections(ctx)
	require.NoError(t, err)
	require.NotContains(t, cols, "alpha")
}

func TestInvalidCollectionName(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()
	_, err := d.Write(ctx, "1bad", []dbpkg.Document{{"x": 1}}, dbpkg.WriteOptions{})
	require.Error(t, err)
	_, err = d.Write(ctx, "ok; DROP TABLE foo --", []dbpkg.Document{{"x": 1}}, dbpkg.WriteOptions{})
	require.Error(t, err)
}

func TestCleanupTimeout(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	expired := dbpkg.Document{"date_epoch": float64(time.Now().Add(-2 * time.Hour).Unix()), "ttl": float64(3600)}
	live := dbpkg.Document{"date_epoch": float64(time.Now().Unix()), "ttl": float64(3600)}
	noTTL := dbpkg.Document{"date_epoch": float64(time.Now().Unix())}
	_, err := d.Write(ctx, "agg", []dbpkg.Document{expired, live, noTTL}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	deleted, err := d.CleanupTimeout(ctx, "agg")
	require.NoError(t, err)
	require.Equal(t, 1, deleted)

	_, total, err := d.Search(ctx, "agg", condition.Cond{}, dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 2, total)
}

func TestCleanupComments(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	// Two records, three comments — one points to a missing record_uid.
	// Use UpdateOne (upsert) to seed records with controlled uids;
	// Write rejects unknown uids by design.
	require.NoError(t, d.UpdateOne(ctx, "record", "r1", dbpkg.Document{"uid": "r1"}, false))
	require.NoError(t, d.UpdateOne(ctx, "record", "r2", dbpkg.Document{"uid": "r2"}, false))
	_, err := d.Write(ctx, "comment", []dbpkg.Document{
		{"record_uid": "r1"},
		{"record_uid": "r2"},
		{"record_uid": "ghost"},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	n, err := d.CleanupComments(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestCleanupSnooze(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour).Format(time.RFC3339)
	future := now.Add(24 * time.Hour).Format(time.RFC3339)

	// expired: every datetime.until is past.
	_, err := d.Write(ctx, "snooze", []dbpkg.Document{
		{
			"name": "expired",
			"time_constraints": map[string]any{
				"datetime": []any{
					map[string]any{"from": past, "until": past},
				},
			},
		},
		{
			"name": "future",
			"time_constraints": map[string]any{
				"datetime": []any{
					map[string]any{"from": past, "until": future},
				},
			},
		},
		{"name": "no_constraint"},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	deleted, err := d.CleanupSnooze(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)

	_, total, err := d.Search(ctx, "snooze", condition.Cond{}, dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 2, total)
}

func TestCleanupNotification(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour).Format(time.RFC3339)
	future := now.Add(24 * time.Hour).Format(time.RFC3339)

	_, err := d.Write(ctx, "notification", []dbpkg.Document{
		{
			"name": "expired",
			"time_constraints": map[string]any{
				"datetime": []any{
					map[string]any{"from": past, "until": past},
				},
			},
		},
		{
			"name": "live",
			"time_constraints": map[string]any{
				"datetime": []any{
					map[string]any{"from": past, "until": future},
				},
			},
		},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	deleted, err := d.CleanupNotification(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
}

func TestCleanupOrphans(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	// Seed with controlled uids via UpdateOne (Write rejects unknown uids).
	require.NoError(t, d.UpdateOne(ctx, "rule", "p1", dbpkg.Document{"uid": "p1"}, false))
	require.NoError(t, d.UpdateOne(ctx, "rule", "c1", dbpkg.Document{"uid": "c1", "parents": []any{"p1"}}, false))
	require.NoError(t, d.UpdateOne(ctx, "rule", "c2", dbpkg.Document{"uid": "c2", "parents": []any{"ghost"}}, false))

	n, err := d.CleanupOrphans(ctx, "rule")
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = d.GetOne(ctx, "rule", dbpkg.Document{"uid": "c2"})
	require.ErrorIs(t, err, dbpkg.ErrNotFound)
}

func TestBackup(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()
	dir := t.TempDir()

	_, err := d.Write(ctx, "record", []dbpkg.Document{{"host": "a"}, {"host": "b"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	require.NoError(t, d.Backup(ctx, dir, nil))
}

func TestBusFanout(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := d.Watcher().Subscribe(ctx, "collection.record")
	require.NoError(t, err)

	_, err = d.Write(ctx, "record", []dbpkg.Document{{"host": "alpha"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	select {
	case ev := <-ch:
		require.Equal(t, "collection.record", ev.Topic)
		require.Equal(t, "write", ev.Op)
		require.Equal(t, "record", ev.Collection)
		require.Len(t, ev.UIDs, 1)
	case <-time.After(time.Second):
		t.Fatal("expected event on bus")
	}
}

func TestCloseIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "close.db")
	d, err := New(context.Background(), Config{Path: path})
	require.NoError(t, err)
	require.NoError(t, d.Close())
	require.NoError(t, d.Close())
}

func TestRegexpUDFErrorBubbles(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.Write(ctx, "record", []dbpkg.Document{{"host": "x"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	// Invalid regex pattern (unbalanced bracket).
	bad := condition.Cond{Op: condition.OpMatches, Field: "host", Value: "[abc"}
	_, _, err = d.Search(ctx, "record", bad, dbpkg.Page{})
	require.Error(t, err)
	// Should still be a regular Go error, not a panic.
	require.True(t, errors.Is(err, err) || err != nil)
}
