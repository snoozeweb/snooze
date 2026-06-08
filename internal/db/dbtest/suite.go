// Package dbtest provides a backend-agnostic conformance test pack that
// every db.Driver implementation must pass. Driver tests wire a factory and
// call RunDriverSuite.
package dbtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Factory returns a freshly initialised driver and a teardown closure. The
// teardown is invoked after the subtest finishes.
type Factory func(t *testing.T) (db.Driver, func())

// RunDriverSuite executes every backend-agnostic case against the driver
// produced by factory. Drivers wire their own setup (containers, on-disk
// SQLite, etc) into factory.
func RunDriverSuite(t *testing.T, name string, factory Factory) {
	t.Helper()
	cases := []struct {
		name string
		run  func(*testing.T, db.Driver)
	}{
		{"Write", testWrite},
		{"SearchOperators", testSearchOperators},
		{"SearchAndOr", testSearchAndOr},
		{"SearchContains", testSearchContains},
		{"SearchIn", testSearchIn},
		{"SearchNested", testSearchNested},
		{"SearchPagination", testSearchPagination},
		{"SearchOrderBy", testSearchOrderBy},
		{"SearchRuleTreeOrder", testSearchRuleTreeOrder},
		{"SearchOnlyOne", testSearchOnlyOne},
		{"Update", testUpdate},
		{"Replace", testReplace},
		{"Delete", testDelete},
		{"DeleteAllRequiresForce", testDeleteAllRequiresForce},
		{"BulkIncrement", testBulkIncrement},
		{"BulkIncrementUpsert", testBulkIncrementUpsert},
		{"IncMany", testIncMany},
		{"SetFields", testSetFields},
		{"UnsetFields", testUnsetFields},
		{"AppendList", testAppendList},
		{"PrependList", testPrependList},
		{"RemoveList", testRemoveList},
		{"Drop", testDrop},
		{"DropCreate", testDropCreate},
		{"Index", testIndex},
		{"CleanupTimeout", testCleanupTimeout},
		{"CleanupComments", testCleanupComments},
		{"CleanupOrphans", testCleanupOrphans},
		{"CleanupAuditLogs", testCleanupAuditLogs},
		{"CleanupSnooze", testCleanupSnooze},
		{"CleanupNotification", testCleanupNotification},
		{"ComputeStats", testComputeStats},
		{"WriteStampsTenantID", testWriteStampsTenantID},
		{"WriteUpsertTenantFenced", testWriteUpsertTenantFenced},
		{"BulkIncrementTenantIsolation", testBulkIncrementTenantIsolation},
		{"AsyncWriterTenantIsolation", testAsyncWriterTenantIsolation},
	}
	for _, c := range cases {
		t.Run(name+"/"+c.name, func(t *testing.T) {
			drv, teardown := factory(t)
			defer teardown()
			c.run(t, drv)
		})
	}
}

// ctx runs the backend-mechanics conformance suite under platform scope so the
// driver skips tenant_id injection entirely (no predicate on reads, no stamp on
// writes). These cases exercise the boolean translator, pagination, bulk
// mutators and cleanup jobs — not tenancy — so a naked tenant context would
// either fail closed (scoped collection, no tenant) or require write-side
// stamping that lands in a later task. Cross-tenant isolation and the
// fail-closed contract are covered separately by RunTenantIsolationSuite.
func ctx() context.Context {
	return snoozetypes.WithPlatformScope(context.Background())
}

func mustCond(t *testing.T, v any) condition.Cond {
	t.Helper()
	c, err := condition.FromList(v)
	require.NoError(t, err)
	return c
}

func mustWrite(t *testing.T, drv db.Driver, collection string, docs ...db.Document) db.WriteResult {
	t.Helper()
	res, err := drv.Write(ctx(), collection, docs, db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)
	return res
}

func search(t *testing.T, drv db.Driver, collection string, c condition.Cond) ([]db.Document, int) {
	t.Helper()
	docs, total, err := drv.Search(ctx(), collection, c, db.Page{})
	require.NoError(t, err)
	return docs, total
}

func testWrite(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record", db.Document{"a": "1", "b": "2"})
	docs, _ := search(t, drv, "record", condition.Cond{})
	require.Len(t, docs, 1)
	require.Equal(t, "1", docs[0]["a"])
	require.Equal(t, "2", docs[0]["b"])
}

func testSearchOperators(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": int64(1), "b": int64(2), "d": int64(1)},
		db.Document{"a": int64(30), "b": int64(40), "c": "tata", "d": int64(1)},
	)
	cases := []struct {
		name  string
		cond  any
		count int
	}{
		{"and_neq", []any{"AND", []any{"=", "a", int64(1)}, []any{"!=", "b", int64(40)}}, 1},
		{"or_eq", []any{"OR", []any{"=", "a", int64(1)}, []any{"=", "a", int64(30)}}, 2},
		{"matches", []any{"MATCHES", "c", "ta.*"}, 1},
		{"not_eq", []any{"NOT", []any{"=", "a", int64(1)}}, 1},
		{"exists", []any{"EXISTS", "c"}, 1},
		{"gt", []any{">", "a", int64(1)}, 1},
		{"eq_str_miss", []any{"=", "c", "toto"}, 0},
		{"nested_or", []any{"AND", []any{"=", "b", int64(2)}, []any{"OR", []any{"=", "d", int64(2)}, []any{"=", "d", int64(1)}}}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, total := search(t, drv, "record", mustCond(t, c.cond))
			require.Equal(t, c.count, total)
		})
	}
}

func testSearchAndOr(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": int64(1), "b": int64(2), "c": int64(3)},
		db.Document{"c": int64(3)},
	)
	_, total := search(t, drv, "record",
		mustCond(t, []any{"AND", []any{"=", "a", int64(1)}, []any{"=", "b", int64(2)}, []any{"=", "c", int64(3)}}))
	require.Equal(t, 1, total)
	_, total = search(t, drv, "record",
		mustCond(t, []any{"OR", []any{"!=", "a", int64(1)}, []any{"!=", "b", int64(2)}, []any{"=", "c", int64(3)}}))
	require.Equal(t, 2, total)
}

func testSearchContains(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": []any{"00", "11", "22", int64(9)}},
		db.Document{"a": []any{"00", "1", "2"}},
		db.Document{"a": []any{"00", "1", "4"}},
		db.Document{"b": "5"},
	)
	_, total := search(t, drv, "record", mustCond(t, []any{"CONTAINS", "a", "1"}))
	require.Equal(t, 3, total)
}

func testSearchIn(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": []any{"00", "11", "22", int64(9)}},
		db.Document{"a": []any{"00", "1", "2"}},
		db.Document{"a": []any{"00", "1", "4"}},
		db.Document{"b": "5"},
	)
	_, total := search(t, drv, "record", mustCond(t, []any{"IN", "1", "a"}))
	require.Equal(t, 2, total)
}

func testSearchNested(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": []any{int64(1), int64(2)}, "b": map[string]any{"c": int64(2), "d": int64(3)}},
	)
	_, total := search(t, drv, "record", mustCond(t, []any{"=", "b.c", int64(2)}))
	require.Equal(t, 1, total)
}

func testSearchPagination(t *testing.T, drv db.Driver) {
	docs := make([]db.Document, 5)
	for i := range docs {
		docs[i] = db.Document{"a": int64(i + 1), "b": "2"}
	}
	mustWrite(t, drv, "record", docs...)
	results, total, err := drv.Search(ctx(), "record", mustCond(t, []any{"=", "b", "2"}), db.Page{PerPage: 2})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, 5, total)
	results, _, err = drv.Search(ctx(), "record", mustCond(t, []any{"=", "b", "2"}), db.Page{PerPage: 2, PageNb: 3})
	require.NoError(t, err)
	require.Len(t, results, 1)
}

func testSearchOrderBy(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": "1"}, db.Document{"a": "3"}, db.Document{"a": "2"},
		db.Document{"a": "5"}, db.Document{"a": "4"},
	)
	docs, _, err := drv.Search(ctx(), "record", condition.Cond{}, db.Page{OrderBy: "a", Asc: false})
	require.NoError(t, err)
	require.NotEmpty(t, docs)
	require.Equal(t, "5", docs[0]["a"])
}

// testSearchRuleTreeOrder pins that rule sibling order is preserved across
// every backend. The rule plugin reads its records ordered by `tree_order`
// (see internal/pluginimpl/rule/plugin.go), and reorder UX hangs off the
// same field. A regression here would silently scramble rule evaluation
// order on whichever backend broke. Also exercises an `IN parents`
// search, which is how the rule plugin loads each level of the tree.
func testSearchRuleTreeOrder(t *testing.T, drv db.Driver) {
	// Insert top-level rules out of declaration order so the OrderBy is the
	// only thing that puts them back in 0,1,2,3.
	res := mustWrite(t, drv, "rule",
		db.Document{"name": "third", "tree_order": int64(2), "condition": []any{}},
		db.Document{"name": "first", "tree_order": int64(0), "condition": []any{}},
		db.Document{"name": "fourth", "tree_order": int64(3), "condition": []any{}},
		db.Document{"name": "second", "tree_order": int64(1), "condition": []any{}},
	)
	require.Len(t, res.Added, 4)
	parentUID := res.Added[1] // "first"

	// Insert two children of "first" with sibling order reversed.
	mustWrite(t, drv, "rule",
		db.Document{"name": "child-B", "tree_order": int64(1), "parents": []any{parentUID}, "condition": []any{}},
		db.Document{"name": "child-A", "tree_order": int64(0), "parents": []any{parentUID}, "condition": []any{}},
	)

	// Top-level rules: NOT EXISTS parents, ordered by tree_order ascending.
	// This mirrors the plugin's load (rule/plugin.go ~line 117).
	topLevel := condition.Not(condition.Exists("parents"))
	docs, _, err := drv.Search(ctx(), "rule", topLevel, db.Page{OrderBy: "tree_order", Asc: true})
	require.NoError(t, err)
	require.Len(t, docs, 4)
	require.Equal(t, []string{"first", "second", "third", "fourth"},
		[]string{
			docs[0]["name"].(string),
			docs[1]["name"].(string),
			docs[2]["name"].(string),
			docs[3]["name"].(string),
		})

	// Children of "first": IN parents == parentUID, ordered by tree_order.
	// Mirrors rule/plugin.go ~line 120-121.
	childCond := condition.Cond{Op: condition.OpIn, Field: "parents", Value: parentUID}
	kids, _, err := drv.Search(ctx(), "rule", childCond, db.Page{OrderBy: "tree_order", Asc: true})
	require.NoError(t, err)
	require.Len(t, kids, 2)
	require.Equal(t, "child-A", kids[0]["name"])
	require.Equal(t, "child-B", kids[1]["name"])

	// Reorder via UpdateOne: swap children's tree_order. This mirrors what
	// a drag-and-drop UX would do (PATCH each affected sibling).
	uidA := kids[0]["uid"].(string)
	uidB := kids[1]["uid"].(string)
	require.NoError(t, drv.UpdateOne(ctx(), "rule", uidA, db.Document{"tree_order": int64(1)}, false))
	require.NoError(t, drv.UpdateOne(ctx(), "rule", uidB, db.Document{"tree_order": int64(0)}, false))

	kids, _, err = drv.Search(ctx(), "rule", childCond, db.Page{OrderBy: "tree_order", Asc: true})
	require.NoError(t, err)
	require.Equal(t, "child-B", kids[0]["name"])
	require.Equal(t, "child-A", kids[1]["name"])
}

func testSearchOnlyOne(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": "1", "b": "1"},
		db.Document{"a": "1", "b": "2"},
	)
	docs, total, err := drv.Search(ctx(), "record", mustCond(t, []any{"=", "a", "1"}), db.Page{OnlyOne: true})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, docs, 1)
}

func testUpdate(t *testing.T, drv db.Driver) {
	res := mustWrite(t, drv, "record", db.Document{"a": "1", "b": "2"})
	require.Len(t, res.Added, 1)
	uid := res.Added[0]
	require.NoError(t, drv.UpdateOne(ctx(), "record", uid, db.Document{"b": "3"}, false))
	got, err := drv.GetOne(ctx(), "record", db.Document{"uid": uid})
	require.NoError(t, err)
	require.Equal(t, "3", got["b"])
}

func testReplace(t *testing.T, drv db.Driver) {
	res := mustWrite(t, drv, "record", db.Document{"a": "1", "b": "2"})
	uid := res.Added[0]
	matched, err := drv.ReplaceOne(ctx(), "record", db.Document{"uid": uid}, db.Document{"a": "9"}, false)
	require.NoError(t, err)
	require.Equal(t, 1, matched)
	got, err := drv.GetOne(ctx(), "record", db.Document{"uid": uid})
	require.NoError(t, err)
	require.Equal(t, "9", got["a"])
	_, hasB := got["b"]
	require.False(t, hasB)
}

func testDelete(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": "1", "b": "2"},
		db.Document{"a": "30", "b": "40"},
		db.Document{"a": "100", "b": "400"},
	)
	c := mustCond(t, []any{"OR", []any{"=", "a", "1"}, []any{"=", "a", "30"}})
	deleted, err := drv.Delete(ctx(), "record", c, false)
	require.NoError(t, err)
	require.Equal(t, 2, deleted)
	docs, _ := search(t, drv, "record", c)
	require.Empty(t, docs)
}

func testDeleteAllRequiresForce(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": "1", "b": "2"}, db.Document{"a": "30", "b": "40"},
	)
	got, err := drv.Delete(ctx(), "record", condition.Cond{}, false)
	require.NoError(t, err)
	require.Equal(t, 0, got)
	got, err = drv.Delete(ctx(), "record", condition.Cond{}, true)
	require.NoError(t, err)
	require.Equal(t, 2, got)
}

func testBulkIncrement(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "counter",
		db.Document{"name": "counter 1", "hits": int64(0)},
		db.Document{"name": "counter 2", "hits": int64(40)},
	)
	ops := []db.IncrementOp{
		{Search: db.Document{"name": "counter 2"}, Deltas: map[string]int64{"hits": 2}},
		{Search: db.Document{"name": "counter missing"}, Deltas: map[string]int64{"hits": 1}},
	}
	require.NoError(t, drv.BulkIncrement(ctx(), "counter", ops, false))
	docs, _ := search(t, drv, "counter", condition.Cond{})
	require.Len(t, docs, 2)
}

func testBulkIncrementUpsert(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "stat",
		db.Document{"name": "stat 1", "hits": int64(0)},
		db.Document{"name": "stat 2", "hits": int64(40)},
	)
	ops := []db.IncrementOp{
		{Search: db.Document{"name": "stat 2"}, Deltas: map[string]int64{"hits": 2}},
		{Search: db.Document{"name": "stat 3"}, Deltas: map[string]int64{"hits": 1}},
	}
	require.NoError(t, drv.BulkIncrement(ctx(), "stat", ops, true))
	docs, _ := search(t, drv, "stat", condition.Cond{})
	require.Len(t, docs, 3)
}

func testIncMany(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": "1", "count": int64(1)},
		db.Document{"a": "1", "count": int64(2)},
	)
	matched, err := drv.IncMany(ctx(), "record", "count", mustCond(t, []any{"=", "a", "1"}), 2)
	require.NoError(t, err)
	require.Equal(t, 2, matched)
}

func testSetFields(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": "1"},
		db.Document{"b": "1", "c": "1"},
		db.Document{"b": "1"},
	)
	matched, err := drv.SetFields(ctx(), "record",
		db.Document{"c": "2", "d": "1"}, mustCond(t, []any{"=", "b", "1"}))
	require.NoError(t, err)
	require.Equal(t, 2, matched)
}

func testUnsetFields(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"host": "h", "snoozed": "Warnings"},
		db.Document{"host": "h", "snoozed": "Warnings"},
		db.Document{"host": "h"}, // already has no snoozed
	)
	// Two records currently satisfy EXISTS snoozed.
	_, total := search(t, drv, "record", mustCond(t, []any{"EXISTS", "snoozed"}))
	require.Equal(t, 2, total)

	// Unsetting it modifies only the two that carry it.
	matched, err := drv.UnsetFields(ctx(), "record",
		[]string{"snoozed"}, mustCond(t, []any{"=", "host", "h"}))
	require.NoError(t, err)
	require.Equal(t, 2, matched)

	// The key is truly gone — EXISTS snoozed now matches nothing on every
	// backend. (A merge write cannot achieve this: it preserves keys it does
	// not mention, which is the bug UnsetFields exists to fix.)
	_, total = search(t, drv, "record", mustCond(t, []any{"EXISTS", "snoozed"}))
	require.Equal(t, 0, total)

	// Idempotent: re-running modifies nothing.
	matched, err = drv.UnsetFields(ctx(), "record",
		[]string{"snoozed"}, mustCond(t, []any{"=", "host", "h"}))
	require.NoError(t, err)
	require.Equal(t, 0, matched)
}

func testAppendList(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record", db.Document{"id": int64(1), "parents": []any{"a"}})
	matched, err := drv.AppendList(ctx(), "record",
		map[string][]any{"parents": {"b", "c"}}, mustCond(t, []any{"=", "id", int64(1)}))
	require.NoError(t, err)
	require.Equal(t, 1, matched)
}

func testPrependList(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record", db.Document{"id": int64(1), "parents": []any{"c"}})
	matched, err := drv.PrependList(ctx(), "record",
		map[string][]any{"parents": {"a", "b"}}, mustCond(t, []any{"=", "id", int64(1)}))
	require.NoError(t, err)
	require.Equal(t, 1, matched)
}

func testRemoveList(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record", db.Document{"id": int64(1), "parents": []any{"a", "b", "c"}})
	matched, err := drv.RemoveList(ctx(), "record",
		map[string][]any{"parents": {"b", "c"}}, mustCond(t, []any{"=", "id", int64(1)}))
	require.NoError(t, err)
	require.Equal(t, 1, matched)
}

func testDrop(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "test", db.Document{"name": "test"})
	docs, _ := search(t, drv, "test", condition.Cond{})
	require.Len(t, docs, 1)
	require.NoError(t, drv.Drop(ctx(), "test"))
	docs, _ = search(t, drv, "test", condition.Cond{})
	require.Empty(t, docs)
}

func testDropCreate(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "test", db.Document{"name": "test"})
	require.NoError(t, drv.Drop(ctx(), "test"))
	mustWrite(t, drv, "test", db.Document{"name": "test2"})
	docs, _ := search(t, drv, "test", condition.Cond{})
	require.Len(t, docs, 1)
	require.Equal(t, "test2", docs[0]["name"])
}

func testIndex(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record", db.Document{"a": "1"})
	require.NoError(t, drv.CreateIndex(ctx(), "record", []string{"a"}))
}

func testCleanupTimeout(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "record",
		db.Document{"a": "1", "ttl": int64(0), "date_epoch": float64(0)},
		db.Document{"b": "1", "ttl": int64(0), "date_epoch": float64(0)},
		db.Document{"c": "1", "ttl": int64(1)},
		db.Document{"d": "1"},
	)
	deleted, err := drv.CleanupTimeout(ctx(), "record")
	require.NoError(t, err)
	// All three backends keep a record that has ttl but no date_epoch (matching
	// the legacy Python pipeline), so only the {a,b} pair (ttl=0, date_epoch=0)
	// is removed. Record c (ttl, no date_epoch) and d (no ttl) are kept.
	require.Equal(t, 2, deleted)
}

func testCleanupComments(t *testing.T, drv db.Driver) {
	res := mustWrite(t, drv, "record", db.Document{"a": "1"}, db.Document{"b": "1"})
	uids := res.Added
	require.Len(t, uids, 2)
	mustWrite(t, drv, "comment",
		db.Document{"record_uid": uids[0]},
		db.Document{"record_uid": uids[1]},
		db.Document{"record_uid": "random"},
	)
	deleted, err := drv.CleanupComments(ctx())
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
}

func testCleanupOrphans(t *testing.T, drv db.Driver) {
	res, err := drv.CleanupOrphans(ctx(), "record")
	require.NoError(t, err)
	require.Equal(t, 0, res)
}

func testCleanupAuditLogs(t *testing.T, drv db.Driver) {
	// Resolved by date_epoch (the field audit writers populate) on every
	// backend; the action verb is "delete"/"create" (what crud.go emits), not
	// the UI's "deleted" label. An object is pruned when its max-epoch event set
	// includes a delete and that max is older than the threshold.
	//   obj1: latest (200) is delete       -> prune both rows
	//   obj2: latest (300) is create       -> keep
	//   obj3: create+delete tie at max 500 -> delete-at-max present -> prune both
	mustWrite(t, drv, "audit",
		db.Document{"object_id": "obj1", "action": "create", "date_epoch": float64(100)},
		db.Document{"object_id": "obj1", "action": "delete", "date_epoch": float64(200)},
		db.Document{"object_id": "obj2", "action": "delete", "date_epoch": float64(100)},
		db.Document{"object_id": "obj2", "action": "create", "date_epoch": float64(300)},
		db.Document{"object_id": "obj3", "action": "create", "date_epoch": float64(500)},
		db.Document{"object_id": "obj3", "action": "delete", "date_epoch": float64(500)},
	)
	n, err := drv.CleanupAuditLogs(ctx(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, 4, n) // obj1 (2) + obj3 (2); obj2 kept
}

func testComputeStats(t *testing.T, drv db.Driver) {
	// `date` is a timestamp, not a bare number: the SQL backends cast it
	// (`::timestamptz` / strftime) and Mongo stores/compares it as a BSON Date.
	// A time.Time round-trips correctly through every backend's Write.
	when := time.Now().Add(-24 * time.Hour).UTC()
	mustWrite(t, drv, "stats",
		db.Document{"date": when, "key": "a_qty", "value": int64(1)},
		db.Document{"date": when, "key": "a_qty", "value": int64(2)},
		db.Document{"date": when, "key": "b_qty", "value": int64(5)},
	)
	buckets, err := drv.ComputeStats(ctx(), "stats", time.Unix(0, 0), time.Now(), "day")
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	// The per-bucket series must actually be populated, with $sum aggregating
	// repeated keys. This guards the Mongo decode path: nested $push'd documents
	// come back as bson.D (not bson.M), so a naive `e.(bson.M)` type-assert
	// silently drops every entry and yields an empty series.
	series := map[string]float64{}
	for _, kv := range buckets[0].Series {
		series[kv.Key] = kv.Value
	}
	require.Equal(t, map[string]float64{"a_qty": 3, "b_qty": 5}, series)
}

func testCleanupSnooze(t *testing.T, drv db.Driver) {
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour).Format(time.RFC3339)
	future := now.Add(24 * time.Hour).Format(time.RFC3339)
	// expired: every datetime.until is in the past — should be deleted.
	expired := db.Document{
		"name": "expired",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": past, "until": past},
			},
		},
	}
	// future: at least one datetime.until is in the future — kept.
	future1 := db.Document{
		"name": "future",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": past, "until": future},
			},
		},
	}
	// noConstraint: no datetime entries — kept.
	noConstraint := db.Document{"name": "no_constraint"}
	// mixed: one past, one future — kept (not every entry has expired).
	mixed := db.Document{
		"name": "mixed",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": past, "until": past},
				map[string]any{"from": past, "until": future},
			},
		},
	}
	mustWrite(t, drv, "snooze", expired, future1, noConstraint, mixed)
	deleted, err := drv.CleanupSnooze(ctx())
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	_, total, err := drv.Search(ctx(), "snooze", mustCond(t, nil), db.Page{})
	require.NoError(t, err)
	require.Equal(t, 3, total)
}

func testCleanupNotification(t *testing.T, drv db.Driver) {
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour).Format(time.RFC3339)
	future := now.Add(24 * time.Hour).Format(time.RFC3339)
	expired := db.Document{
		"name": "expired",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": past, "until": past},
			},
		},
	}
	live := db.Document{
		"name": "live",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": past, "until": future},
			},
		},
	}
	mustWrite(t, drv, "notification", expired, live)
	deleted, err := drv.CleanupNotification(ctx())
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
}

// testWriteStampsTenantID verifies that Write injects tenant_id into the stored
// document and that a Search under the same tenant returns it, while a Search
// under a different tenant returns nothing.
func testWriteStampsTenantID(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	// Write under tenant alpha.
	res, err := drv.Write(ctxA, "record",
		[]db.Document{{"host": "srv-a"}},
		db.WriteOptions{UpdateTime: false},
	)
	require.NoError(t, err)
	require.Len(t, res.Added, 1)

	// Search under alpha: must see it.
	docs, total, err := drv.Search(ctxA, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "alpha", docs[0]["tenant_id"])

	// Search under beta: must see nothing.
	docs, total, err = drv.Search(ctxB, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Empty(t, docs)
}

// testBulkIncrementTenantIsolation verifies that a BulkIncrement issued under
// one tenant only touches that tenant's rows, even when both tenants hold a row
// with the same primary-key value.
func testBulkIncrementTenantIsolation(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	// Seed one row per tenant via Write.
	opts := db.WriteOptions{Primary: []string{"name"}, UpdateTime: false}
	_, err := drv.Write(ctxA, "stats", []db.Document{{"name": "hits", "count": float64(0)}}, opts)
	require.NoError(t, err)
	_, err = drv.Write(ctxB, "stats", []db.Document{{"name": "hits", "count": float64(0)}}, opts)
	require.NoError(t, err)

	// BulkIncrement under alpha.
	err = drv.BulkIncrement(ctxA, "stats", []db.IncrementOp{
		{Search: db.Document{"name": "hits"}, Deltas: map[string]int64{"count": 5}},
	}, false)
	require.NoError(t, err)

	// Alpha count = 5, beta count = 0.
	docsA, _, err := drv.Search(ctxA, "stats", condition.Equals("name", "hits"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsA, 1)
	require.EqualValues(t, 5, toNumber(docsA[0]["count"]))

	docsB, _, err := drv.Search(ctxB, "stats", condition.Equals("name", "hits"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsB, 1)
	require.EqualValues(t, 0, toNumber(docsB[0]["count"]))
}

// testAsyncWriterTenantIsolation drives a real driver through the process-wide
// asyncwriter.Writer (a singleton that serves all tenants) and proves that two
// tenants incrementing the SAME scoped collection within one flush window do not
// clobber or drop each other's deltas. This is the end-to-end reproduction of
// the cross-tenant data-loss bug: alpha.Increment(+5) then beta.Increment(+9) on
// collection "stats" in one flush must leave alpha=5 AND beta=9 (not beta=0).
func testAsyncWriterTenantIsolation(t *testing.T, drv db.Driver) {
	ctxAlpha := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxBeta := snoozetypes.WithTenant(context.Background(), "beta")

	// Seed one "hits" row per tenant.
	opts := db.WriteOptions{Primary: []string{"name"}, UpdateTime: false}
	_, err := drv.Write(ctxAlpha, "stats", []db.Document{{"name": "hits", "count": float64(0)}}, opts)
	require.NoError(t, err)
	_, err = drv.Write(ctxBeta, "stats", []db.Document{{"name": "hits", "count": float64(0)}}, opts)
	require.NoError(t, err)

	// One process-wide writer; long period so we control the flush explicitly.
	w := asyncwriter.New(drv, time.Hour, nil)
	w.Increment(ctxAlpha, "stats", "count", db.Document{"name": "hits"}, 5)
	w.Increment(ctxBeta, "stats", "count", db.Document{"name": "hits"}, 9)
	require.NoError(t, w.Flush(snoozetypes.WithPlatformScope(context.Background())))

	docsA, _, err := drv.Search(ctxAlpha, "stats", condition.Equals("name", "hits"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsA, 1)
	require.EqualValues(t, 5, toNumber(docsA[0]["count"]))

	docsB, _, err := drv.Search(ctxBeta, "stats", condition.Equals("name", "hits"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsB, 1)
	require.EqualValues(t, 9, toNumber(docsB[0]["count"]))
}

func toNumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

// testWriteUpsertTenantFenced verifies that an upsert in tenant A cannot
// match or overwrite a row written by tenant B when they share the same
// primary key value.
func testWriteUpsertTenantFenced(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	opts := db.WriteOptions{Primary: []string{"name"}, UpdateTime: false}

	// Both tenants write a doc with name="shared".
	resA, err := drv.Write(ctxA, "rule", []db.Document{{"name": "shared", "val": "a"}}, opts)
	require.NoError(t, err)
	require.Len(t, resA.Added, 1)

	resB, err := drv.Write(ctxB, "rule", []db.Document{{"name": "shared", "val": "b"}}, opts)
	require.NoError(t, err)
	require.Len(t, resB.Added, 1)

	// Different uids — these are distinct rows in different tenants.
	require.NotEqual(t, resA.Added[0], resB.Added[0])

	// Alpha sees its own val.
	docsA, _, err := drv.Search(ctxA, "rule", condition.Equals("name", "shared"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsA, 1)
	require.Equal(t, "a", docsA[0]["val"])

	// Beta sees its own val.
	docsB, _, err := drv.Search(ctxB, "rule", condition.Equals("name", "shared"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsB, 1)
	require.Equal(t, "b", docsB[0]["val"])
}

// RunTenantIsolationSuite executes the full cross-tenant isolation and
// fail-closed suite against the driver produced by factory. Separate from
// RunDriverSuite so backends that haven't been updated yet don't block CI.
func RunTenantIsolationSuite(t *testing.T, name string, factory Factory) {
	t.Helper()
	cases := []struct {
		name string
		run  func(*testing.T, db.Driver)
	}{
		{"TenantABIsolation", testTenantABIsolation},
		{"PlatformScopeSeesAll", testPlatformScopeSeesAll},
		{"NakedContextErrors", testNakedContextErrors},
		{"GlobalCollectionNoInjection", testGlobalCollectionNoInjection},
		{"ReplaceOneTenantFenced", testReplaceOneTenantFenced},
		{"ReplaceOnePreservesTenant", testReplaceOnePreservesTenant},
		{"UpdateOneTenantFenced", testUpdateOneTenantFenced},
		{"ReplaceUpdateNakedContextErrors", testReplaceUpdateNakedContextErrors},
		{"GetOneCrossTenant", testGetOneCrossTenant},
		{"WriteForeignUIDCrossTenant", testWriteForeignUIDCrossTenant},
		{"WriteSameTenantUIDCollision", testWriteSameTenantUIDCollision},
		{"DeleteCrossTenant", testDeleteCrossTenant},
		{"MutatorsCrossTenant", testMutatorsCrossTenant},
		{"CleanupPerTenant", testCleanupPerTenant},
		{"CleanupTimeoutPerTenant", testCleanupTimeoutPerTenant},
		{"CleanupCommentsPerTenant", testCleanupCommentsPerTenant},
		{"CleanupAuditLogsPerTenant", testCleanupAuditLogsPerTenant},
		{"CleanupNotificationPerTenant", testCleanupNotificationPerTenant},
		{"ComputeStatsPerTenant", testComputeStatsPerTenant},
		{"ReadWriteNakedContextFailClosed", testReadWriteNakedContextFailClosed},
		{"CleanupNakedContextFailClosed", testCleanupNakedContextFailClosed},
	}
	for _, c := range cases {
		t.Run(name+"/"+c.name, func(t *testing.T) {
			drv, teardown := factory(t)
			defer teardown()
			c.run(t, drv)
		})
	}
}

func testTenantABIsolation(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	mustWriteCtx(ctxA, t, drv, "record", db.Document{"msg": "from-a"})
	mustWriteCtx(ctxB, t, drv, "record", db.Document{"msg": "from-b"})

	docsA, totalA, err := drv.Search(ctxA, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, totalA)
	require.Equal(t, "from-a", docsA[0]["msg"])

	docsB, totalB, err := drv.Search(ctxB, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, totalB)
	require.Equal(t, "from-b", docsB[0]["msg"])
}

func testPlatformScopeSeesAll(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")
	ctxPlat := snoozetypes.WithPlatformScope(context.Background())

	mustWriteCtx(ctxA, t, drv, "record", db.Document{"msg": "from-a"})
	mustWriteCtx(ctxB, t, drv, "record", db.Document{"msg": "from-b"})

	docs, total, err := drv.Search(ctxPlat, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, docs, 2)
}

func testNakedContextErrors(t *testing.T, drv db.Driver) {
	naked := context.Background()

	// Search on a scoped collection must error.
	_, _, err := drv.Search(naked, "record", condition.Cond{}, db.Page{})
	require.Error(t, err)
	require.True(t, errors.Is(err, snoozetypes.ErrNoTenant),
		"Search on naked ctx: error must wrap ErrNoTenant, got: %v", err)

	// Write on a scoped collection must error.
	_, err = drv.Write(naked, "record", []db.Document{{"msg": "x"}}, db.WriteOptions{})
	require.Error(t, err)
	require.True(t, errors.Is(err, snoozetypes.ErrNoTenant),
		"Write on naked ctx: error must wrap ErrNoTenant, got: %v", err)
}

func testGlobalCollectionNoInjection(t *testing.T, drv db.Driver) {
	naked := context.Background()

	// "tenant" is a global collection — naked ctx must NOT error.
	_, err := drv.Write(naked, "tenant", []db.Document{{"id": "test-org", "display_name": "Test"}}, db.WriteOptions{Primary: []string{"id"}})
	require.NoError(t, err, "global collection write must succeed on naked ctx")

	docs, _, err := drv.Search(naked, "tenant", condition.Cond{}, db.Page{})
	require.NoError(t, err, "global collection search must succeed on naked ctx")
	require.NotEmpty(t, docs)
}

// testReplaceOneTenantFenced proves that a ReplaceOne issued under tenant B with
// the same match used by tenant A neither modifies A's document nor leaks across
// the tenant boundary: A keeps its original row, and B gets its own scoped row.
func testReplaceOneTenantFenced(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	// Tenant A writes a doc keyed by name.
	opts := db.WriteOptions{Primary: []string{"name"}, UpdateTime: false}
	resA, err := drv.Write(ctxA, "rule", []db.Document{{"name": "shared", "val": "a"}}, opts)
	require.NoError(t, err)
	require.Len(t, resA.Added, 1)

	// Tenant B ReplaceOne with the SAME match. B's scoped find must MISS A's
	// row, so this becomes a B-scoped upsert-insert; A's row is untouched.
	matched, err := drv.ReplaceOne(ctxB, "rule", db.Document{"name": "shared"}, db.Document{"name": "shared", "val": "b"}, false)
	require.NoError(t, err)
	require.Equal(t, 0, matched, "ReplaceOne under beta must not match alpha's row")

	// A still sees its original, unmodified value.
	docsA, _, err := drv.Search(ctxA, "rule", condition.Equals("name", "shared"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsA, 1, "alpha must still see exactly its own row")
	require.Equal(t, "a", docsA[0]["val"], "alpha's value must be untouched")
	require.Equal(t, "alpha", docsA[0]["tenant_id"])

	// B sees its own freshly-created row.
	docsB, _, err := drv.Search(ctxB, "rule", condition.Equals("name", "shared"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsB, 1, "beta must see its own row")
	require.Equal(t, "b", docsB[0]["val"])
	require.Equal(t, "beta", docsB[0]["tenant_id"])
}

// testReplaceOnePreservesTenant proves that replacing A's row under tenant A
// keeps it visible to A — the stored tenant_id must not be dropped by the
// replace (a full-document overwrite).
func testReplaceOnePreservesTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")

	opts := db.WriteOptions{Primary: []string{"name"}, UpdateTime: false}
	resA, err := drv.Write(ctxA, "rule", []db.Document{{"name": "keep", "val": "old"}}, opts)
	require.NoError(t, err)
	require.Len(t, resA.Added, 1)
	uid := resA.Added[0]

	// Replace by uid under tenant A. The replacement doc carries no tenant_id;
	// the driver must stamp it so the row stays visible to A.
	matched, err := drv.ReplaceOne(ctxA, "rule", db.Document{"uid": uid}, db.Document{"name": "keep", "val": "new"}, false)
	require.NoError(t, err)
	require.Equal(t, 1, matched)

	docsA, _, err := drv.Search(ctxA, "rule", condition.Equals("name", "keep"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsA, 1, "replaced row must remain visible to alpha")
	require.Equal(t, "new", docsA[0]["val"])
	require.Equal(t, "alpha", docsA[0]["tenant_id"], "tenant_id must survive the replace")
}

// testUpdateOneTenantFenced proves that UpdateOne by a uid belonging to tenant A,
// executed under tenant B, does NOT modify A's document. B's scoped find misses
// A's uid, so the call upserts a B-scoped row (or no-ops); A's row is unchanged.
func testUpdateOneTenantFenced(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	opts := db.WriteOptions{Primary: []string{"name"}, UpdateTime: false}
	resA, err := drv.Write(ctxA, "rule", []db.Document{{"name": "owned", "val": "a"}}, opts)
	require.NoError(t, err)
	require.Len(t, resA.Added, 1)
	uidA := resA.Added[0]

	// Tenant B updates A's uid. Must not touch A's row.
	err = drv.UpdateOne(ctxB, "rule", uidA, db.Document{"val": "hijacked"}, false)
	require.NoError(t, err)

	// A's row is unchanged and still belongs to alpha.
	docsA, _, err := drv.Search(ctxA, "rule", condition.Equals("name", "owned"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsA, 1, "alpha must still see its row")
	require.Equal(t, "a", docsA[0]["val"], "alpha's value must be untouched by beta's UpdateOne")
	require.Equal(t, "alpha", docsA[0]["tenant_id"])

	// Whatever B created (if anything) must not be the hijacked alpha row: A
	// owns exactly one row with val=="a".
	plat := snoozetypes.WithPlatformScope(context.Background())
	all, _, err := drv.Search(plat, "rule", condition.Equals("name", "owned"), db.Page{})
	require.NoError(t, err)
	for _, d := range all {
		if d["tenant_id"] == "alpha" {
			require.Equal(t, "a", d["val"], "alpha's row must never carry beta's patch")
		}
	}
}

// testReplaceUpdateNakedContextErrors proves that ReplaceOne and UpdateOne on a
// scoped collection fail closed (ErrNoTenant) when the context carries no tenant
// and no platform scope.
func testReplaceUpdateNakedContextErrors(t *testing.T, drv db.Driver) {
	naked := context.Background()

	_, err := drv.ReplaceOne(naked, "record", db.Document{"uid": "x"}, db.Document{"a": "1"}, false)
	require.Error(t, err)
	require.True(t, errors.Is(err, snoozetypes.ErrNoTenant),
		"ReplaceOne on naked ctx: error must wrap ErrNoTenant, got: %v", err)

	err = drv.UpdateOne(naked, "record", "x", db.Document{"a": "1"}, false)
	require.Error(t, err)
	require.True(t, errors.Is(err, snoozetypes.ErrNoTenant),
		"UpdateOne on naked ctx: error must wrap ErrNoTenant, got: %v", err)
}

func mustWriteCtx(ctx context.Context, t *testing.T, drv db.Driver, collection string, docs ...db.Document) db.WriteResult {
	t.Helper()
	res, err := drv.Write(ctx, collection, docs, db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)
	return res
}

// testGetOneCrossTenant proves that a GetOne issued under tenant B cannot read a
// document owned by tenant A, even when B supplies the exact match A used. The
// driver must fence GetOne by tenant_id and return ErrNotFound for B. [C3]
func testGetOneCrossTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	// A writes a doc keyed by name; capture its uid.
	resA := mustWriteCtx(ctxA, t, drv, "rule", db.Document{"name": "secret", "val": "a"})
	require.Len(t, resA.Added, 1)
	uidA := resA.Added[0]

	// A can read its own doc by name and by uid.
	got, err := drv.GetOne(ctxA, "rule", db.Document{"name": "secret"})
	require.NoError(t, err)
	require.Equal(t, "a", got["val"])

	// B GetOne by the SAME name match must MISS — not leak A's row.
	_, err = drv.GetOne(ctxB, "rule", db.Document{"name": "secret"})
	require.ErrorIs(t, err, db.ErrNotFound,
		"GetOne under beta by alpha's name match must return ErrNotFound, not alpha's doc")

	// B GetOne by A's uid must also MISS.
	_, err = drv.GetOne(ctxB, "rule", db.Document{"uid": uidA})
	require.ErrorIs(t, err, db.ErrNotFound,
		"GetOne under beta by alpha's uid must return ErrNotFound, not alpha's doc")
}

// testWriteForeignUIDCrossTenant proves that a Write under tenant B that carries
// a uid belonging to tenant A neither overwrites A's row nor re-stamps it to B.
// B's scoped find misses A's uid, so the write must land as a fresh B-scoped row
// (mirroring SQLite) and A's row stays byte-for-byte under tenant A. [C1/C2]
func testWriteForeignUIDCrossTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	// A writes a doc; capture its uid U.
	resA := mustWriteCtx(ctxA, t, drv, "rule", db.Document{"name": "owned", "val": "a"})
	require.Len(t, resA.Added, 1)
	uidA := resA.Added[0]

	// B writes {uid: U, ...} — a by-uid write under beta.
	_, err := drv.Write(ctxB, "rule",
		[]db.Document{{"uid": uidA, "name": "owned", "val": "hijacked"}},
		db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	// A's row is UNCHANGED: still val=="a", still tenant alpha.
	docsA, _, err := drv.Search(ctxA, "rule", condition.Equals("name", "owned"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docsA, 1, "alpha must still see exactly its own row")
	require.Equal(t, "a", docsA[0]["val"], "alpha's value must NOT be overwritten by beta's by-uid write")
	require.Equal(t, "alpha", docsA[0]["tenant_id"], "alpha's row must NOT be re-stamped to beta")
	require.Equal(t, uidA, docsA[0]["uid"])

	// Platform scope: alpha's uid must still be owned by alpha, never re-stamped
	// to beta. (Whatever beta landed, if anything, is its own row.)
	plat := snoozetypes.WithPlatformScope(context.Background())
	all, _, err := drv.Search(plat, "rule", condition.Equals("uid", uidA), db.Page{})
	require.NoError(t, err)
	require.Len(t, all, 1, "uid U must resolve to exactly one row globally")
	require.Equal(t, "alpha", all[0]["tenant_id"], "uid U must still belong to alpha")
	require.Equal(t, "a", all[0]["val"])
}

// testWriteSameTenantUIDCollision is the positive counterpart to
// testWriteForeignUIDCrossTenant: a by-uid write that targets the SAME tenant's
// existing row must still merge into it. On Postgres this exercises the
// `ON CONFLICT (uid) DO UPDATE ... WHERE data->>'tenant_id' = fence` clause —
// the fence must match the row's own tenant, so a legitimate same-tenant update
// is applied rather than silently dropped as a no-op. [C1 positive path]
func testWriteSameTenantUIDCollision(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")

	// Insert a row (uid auto-assigned) and capture its uid U.
	resA := mustWriteCtx(ctxA, t, drv, "rule",
		db.Document{"name": "owned", "val": "a", "keep": "orig"})
	require.Len(t, resA.Added, 1)
	uidA := resA.Added[0]

	// Same tenant, same uid U: a by-uid merge. `val` is overwritten, `keep`
	// is preserved (merge, not replace), `added` is new.
	res, err := drv.Write(ctxA, "rule",
		[]db.Document{{"uid": uidA, "val": "b", "added": "new"}},
		db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)
	require.Equal(t, []string{uidA}, res.Updated, "same-tenant by-uid write must merge, not reject")
	require.Empty(t, res.Rejected, "same-tenant by-uid write must not be rejected")

	// Exactly one row, carrying the merged payload, still stamped to alpha.
	docsA, total, err := drv.Search(ctxA, "rule", condition.Equals("uid", uidA), db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, docsA, 1)
	require.Equal(t, "b", docsA[0]["val"], "touched key must be overwritten by the same-tenant merge")
	require.Equal(t, "orig", docsA[0]["keep"], "untouched key must be preserved by the merge")
	require.Equal(t, "new", docsA[0]["added"], "new key must be added by the merge")
	require.Equal(t, "owned", docsA[0]["name"], "pre-existing key must be preserved")
	require.Equal(t, "alpha", docsA[0]["tenant_id"], "row must stay stamped to its own tenant")
}

// testDeleteCrossTenant proves that a Delete issued under tenant B with a
// condition matching tenant A's doc deletes nothing and leaves A intact.
func testDeleteCrossTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	mustWriteCtx(ctxA, t, drv, "record", db.Document{"host": "srv", "msg": "from-a"})

	deleted, err := drv.Delete(ctxB, "record", condition.Equals("host", "srv"), false)
	require.NoError(t, err)
	require.Equal(t, 0, deleted, "Delete under beta must not touch alpha's row")

	docsA, totalA, err := drv.Search(ctxA, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, totalA, "alpha's row must survive beta's Delete")
	require.Equal(t, "from-a", docsA[0]["msg"])
}

// testMutatorsCrossTenant proves that every in-place mutator (IncMany, SetFields,
// UnsetFields, AppendList, PrependList, RemoveList) issued under tenant B with a
// condition matching tenant A's doc matches zero rows and leaves A untouched.
func testMutatorsCrossTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	// Each subtest shares the `record` collection (dropped only between top-level
	// cases, not subtests), so each uses a distinct host value to scope its own
	// row and avoid cross-subtest accumulation.
	t.Run("IncMany", func(t *testing.T) {
		cond := condition.Equals("host", "inc")
		mustWriteCtx(ctxA, t, drv, "record", db.Document{"host": "inc", "count": int64(1)})
		matched, err := drv.IncMany(ctxB, "record", "count", cond, 100)
		require.NoError(t, err)
		require.Equal(t, 0, matched, "IncMany under beta must match 0 of alpha's rows")
		docsA, _, err := drv.Search(ctxA, "record", cond, db.Page{})
		require.NoError(t, err)
		require.Len(t, docsA, 1)
		require.EqualValues(t, 1, toNumber(docsA[0]["count"]), "alpha's count must be untouched")
	})

	t.Run("SetFields", func(t *testing.T) {
		cond := condition.Equals("host", "set")
		mustWriteCtx(ctxA, t, drv, "record", db.Document{"host": "set", "val": "a"})
		matched, err := drv.SetFields(ctxB, "record", db.Document{"val": "hijacked"}, cond)
		require.NoError(t, err)
		require.Equal(t, 0, matched, "SetFields under beta must match 0 of alpha's rows")
		docsA, _, err := drv.Search(ctxA, "record", cond, db.Page{})
		require.NoError(t, err)
		require.Len(t, docsA, 1)
		require.Equal(t, "a", docsA[0]["val"], "alpha's val must be untouched")
	})

	t.Run("UnsetFields", func(t *testing.T) {
		cond := condition.Equals("host", "unset")
		mustWriteCtx(ctxA, t, drv, "record", db.Document{"host": "unset", "val": "a"})
		matched, err := drv.UnsetFields(ctxB, "record", []string{"val"}, cond)
		require.NoError(t, err)
		require.Equal(t, 0, matched, "UnsetFields under beta must match 0 of alpha's rows")
		docsA, _, err := drv.Search(ctxA, "record", cond, db.Page{})
		require.NoError(t, err)
		require.Len(t, docsA, 1)
		require.Equal(t, "a", docsA[0]["val"], "alpha's val must still be present")
	})

	t.Run("AppendList", func(t *testing.T) {
		cond := condition.Equals("host", "append")
		mustWriteCtx(ctxA, t, drv, "record", db.Document{"host": "append", "tags": []any{"a"}})
		matched, err := drv.AppendList(ctxB, "record", map[string][]any{"tags": {"x"}}, cond)
		require.NoError(t, err)
		require.Equal(t, 0, matched, "AppendList under beta must match 0 of alpha's rows")
		docsA, _, err := drv.Search(ctxA, "record", cond, db.Page{})
		require.NoError(t, err)
		require.Len(t, docsA, 1)
		require.Len(t, docsA[0]["tags"], 1, "alpha's tags must be untouched")
	})

	t.Run("PrependList", func(t *testing.T) {
		cond := condition.Equals("host", "prepend")
		mustWriteCtx(ctxA, t, drv, "record", db.Document{"host": "prepend", "tags": []any{"a"}})
		matched, err := drv.PrependList(ctxB, "record", map[string][]any{"tags": {"x"}}, cond)
		require.NoError(t, err)
		require.Equal(t, 0, matched, "PrependList under beta must match 0 of alpha's rows")
		docsA, _, err := drv.Search(ctxA, "record", cond, db.Page{})
		require.NoError(t, err)
		require.Len(t, docsA, 1)
		require.Len(t, docsA[0]["tags"], 1, "alpha's tags must be untouched")
	})

	t.Run("RemoveList", func(t *testing.T) {
		cond := condition.Equals("host", "remove")
		mustWriteCtx(ctxA, t, drv, "record", db.Document{"host": "remove", "tags": []any{"a", "b"}})
		matched, err := drv.RemoveList(ctxB, "record", map[string][]any{"tags": {"a"}}, cond)
		require.NoError(t, err)
		require.Equal(t, 0, matched, "RemoveList under beta must match 0 of alpha's rows")
		docsA, _, err := drv.Search(ctxA, "record", cond, db.Page{})
		require.NoError(t, err)
		require.Len(t, docsA, 1)
		require.Len(t, docsA[0]["tags"], 2, "alpha's tags must be untouched")
	})
}

// testCleanupPerTenant covers CleanupOrphans and CleanupSnooze under WithTenant:
// each must remove only the calling tenant's eligible rows and leave the other
// tenant's rows intact. [H3]
func testCleanupPerTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")
	plat := snoozetypes.WithPlatformScope(context.Background())

	t.Run("Snooze", func(t *testing.T) {
		now := time.Now().UTC()
		past := now.Add(-24 * time.Hour).Format(time.RFC3339)
		expired := func(name string) db.Document {
			return db.Document{
				"name": name,
				"time_constraints": map[string]any{
					"datetime": []any{map[string]any{"from": past, "until": past}},
				},
			}
		}
		mustWriteCtx(ctxA, t, drv, "snooze", expired("a-expired"))
		mustWriteCtx(ctxB, t, drv, "snooze", expired("b-expired"))

		deleted, err := drv.CleanupSnooze(ctxA)
		require.NoError(t, err)
		require.Equal(t, 1, deleted, "CleanupSnooze under alpha must remove only alpha's expired row")

		// Alpha's expired row is gone.
		_, totalA, err := drv.Search(ctxA, "snooze", condition.Cond{}, db.Page{})
		require.NoError(t, err)
		require.Equal(t, 0, totalA, "alpha's expired snooze must be deleted")

		// Beta's expired row survives — alpha's cleanup must not cross the boundary.
		docsB, totalB, err := drv.Search(ctxB, "snooze", condition.Cond{}, db.Page{})
		require.NoError(t, err)
		require.Equal(t, 1, totalB, "beta's snooze must survive alpha's cleanup")
		require.Equal(t, "b-expired", docsB[0]["name"])

		// Platform scope: exactly beta's row remains globally.
		_, totalAll, err := drv.Search(plat, "snooze", condition.Cond{}, db.Page{})
		require.NoError(t, err)
		require.Equal(t, 1, totalAll)
	})

	t.Run("Orphans", func(t *testing.T) {
		// Each tenant has a child row pointing at a missing parent uid.
		mustWriteCtx(ctxA, t, drv, "node", db.Document{"name": "a-child", "parents": []any{"missing-a"}})
		mustWriteCtx(ctxB, t, drv, "node", db.Document{"name": "b-child", "parents": []any{"missing-b"}})

		deleted, err := drv.CleanupOrphans(ctxA, "node")
		require.NoError(t, err)
		require.Equal(t, 1, deleted, "CleanupOrphans under alpha must remove only alpha's orphan")

		_, totalA, err := drv.Search(ctxA, "node", condition.Cond{}, db.Page{})
		require.NoError(t, err)
		require.Equal(t, 0, totalA, "alpha's orphan must be deleted")

		docsB, totalB, err := drv.Search(ctxB, "node", condition.Cond{}, db.Page{})
		require.NoError(t, err)
		require.Equal(t, 1, totalB, "beta's orphan must survive alpha's cleanup")
		require.Equal(t, "b-child", docsB[0]["name"])
	})
}

// testCleanupTimeoutPerTenant proves CleanupTimeout under WithTenant(A) removes
// only A's timed-out rows. [H3]
func testCleanupTimeoutPerTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	timedOut := db.Document{"host": "h", "ttl": int64(0), "date_epoch": float64(0)}
	mustWriteCtx(ctxA, t, drv, "record", timedOut)
	mustWriteCtx(ctxB, t, drv, "record", timedOut)

	deleted, err := drv.CleanupTimeout(ctxA, "record")
	require.NoError(t, err)
	require.Equal(t, 1, deleted, "CleanupTimeout under alpha must remove only alpha's timed-out row")

	_, totalA, err := drv.Search(ctxA, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 0, totalA, "alpha's timed-out record must be deleted")

	_, totalB, err := drv.Search(ctxB, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, totalB, "beta's timed-out record must survive alpha's cleanup")
}

// testCleanupCommentsPerTenant proves CleanupComments under WithTenant(A) only
// prunes A's orphaned comments. A comment whose record_uid belongs to B's still
// existing record (visible only under B) must NOT be pruned by A's run, and B's
// own orphaned comment must survive A's run entirely. [H3]
func testCleanupCommentsPerTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	// A: one live record + a comment on it, plus one orphaned comment.
	recA := mustWriteCtx(ctxA, t, drv, "record", db.Document{"msg": "a-record"})
	require.Len(t, recA.Added, 1)
	mustWriteCtx(ctxA, t, drv, "comment", db.Document{"record_uid": recA.Added[0], "text": "a-live"})
	mustWriteCtx(ctxA, t, drv, "comment", db.Document{"record_uid": "gone-a", "text": "a-orphan"})

	// B: one orphaned comment.
	mustWriteCtx(ctxB, t, drv, "comment", db.Document{"record_uid": "gone-b", "text": "b-orphan"})

	deleted, err := drv.CleanupComments(ctxA)
	require.NoError(t, err)
	require.Equal(t, 1, deleted, "CleanupComments under alpha must prune only alpha's orphan")

	// A keeps its live comment, drops its orphan.
	docsA, totalA, err := drv.Search(ctxA, "comment", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, totalA, "alpha must keep its live comment, drop its orphan")
	require.Equal(t, "a-live", docsA[0]["text"])

	// B's orphan survives alpha's run.
	docsB, totalB, err := drv.Search(ctxB, "comment", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, totalB, "beta's comment must survive alpha's cleanup")
	require.Equal(t, "b-orphan", docsB[0]["text"])
}

// testCleanupAuditLogsPerTenant proves CleanupAuditLogs under WithTenant(A)
// prunes only A's eligible audit rows. [H3]
func testCleanupAuditLogsPerTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	// Both tenants own an object whose latest event is a delete in the deep past.
	mustWriteCtx(ctxA, t, drv, "audit",
		db.Document{"object_id": "obj", "action": "create", "date_epoch": float64(100)},
		db.Document{"object_id": "obj", "action": "delete", "date_epoch": float64(200)},
	)
	mustWriteCtx(ctxB, t, drv, "audit",
		db.Document{"object_id": "obj", "action": "create", "date_epoch": float64(100)},
		db.Document{"object_id": "obj", "action": "delete", "date_epoch": float64(200)},
	)

	deleted, err := drv.CleanupAuditLogs(ctxA, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, deleted, "CleanupAuditLogs under alpha must prune only alpha's 2 rows")

	_, totalA, err := drv.Search(ctxA, "audit", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 0, totalA, "alpha's audit rows must be pruned")

	_, totalB, err := drv.Search(ctxB, "audit", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 2, totalB, "beta's audit rows must survive alpha's cleanup")
}

// testCleanupNotificationPerTenant proves CleanupNotification under
// WithTenant(A) removes only A's expired notification rows. [H3]
func testCleanupNotificationPerTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")

	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour).Format(time.RFC3339)
	expired := func(name string) db.Document {
		return db.Document{
			"name": name,
			"time_constraints": map[string]any{
				"datetime": []any{map[string]any{"from": past, "until": past}},
			},
		}
	}
	mustWriteCtx(ctxA, t, drv, "notification", expired("a"))
	mustWriteCtx(ctxB, t, drv, "notification", expired("b"))

	deleted, err := drv.CleanupNotification(ctxA)
	require.NoError(t, err)
	require.Equal(t, 1, deleted, "CleanupNotification under alpha must remove only alpha's row")

	_, totalA, err := drv.Search(ctxA, "notification", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 0, totalA)

	_, totalB, err := drv.Search(ctxB, "notification", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, totalB, "beta's notification must survive alpha's cleanup")
}

// testComputeStatsPerTenant proves ComputeStats under WithTenant(A) aggregates
// only A's stats rows, excluding B's. Alpha and beta write rows on DISTINCT days
// (hence distinct buckets), so alpha's result must contain alpha's bucket and
// must NOT contain beta's bucket — a robust isolation check independent of the
// per-backend series-decode shape. Platform scope must see both buckets. [H4]
func testComputeStatsPerTenant(t *testing.T, drv db.Driver) {
	ctxA := snoozetypes.WithTenant(context.Background(), "alpha")
	ctxB := snoozetypes.WithTenant(context.Background(), "beta")
	plat := snoozetypes.WithPlatformScope(context.Background())

	now := time.Now().UTC()
	dayA := now.Add(-48 * time.Hour)
	dayB := now.Add(-120 * time.Hour) // 5 days ago — a different day bucket
	mustWriteCtx(ctxA, t, drv, "stats", db.Document{"date": dayA, "key": "qty", "value": int64(3)})
	mustWriteCtx(ctxB, t, drv, "stats", db.Document{"date": dayB, "key": "qty", "value": int64(7)})

	bucketsOf := func(buckets []db.StatsBucket) map[string]struct{} {
		set := map[string]struct{}{}
		for _, b := range buckets {
			set[b.Bucket] = struct{}{}
		}
		return set
	}

	// Platform scope: both days present.
	bAll, err := drv.ComputeStats(plat, "stats", time.Unix(0, 0), time.Now(), "day")
	require.NoError(t, err)
	allSet := bucketsOf(bAll)
	require.Len(t, allSet, 2, "platform scope must see both tenants' day buckets")

	// Identify alpha's and beta's bucket labels from the platform run.
	bucketA, err := drv.ComputeStats(ctxA, "stats", time.Unix(0, 0), time.Now(), "day")
	require.NoError(t, err)
	aSet := bucketsOf(bucketA)
	require.Len(t, aSet, 1, "ComputeStats under alpha must see exactly alpha's single day bucket, not beta's")

	bucketB, err := drv.ComputeStats(ctxB, "stats", time.Unix(0, 0), time.Now(), "day")
	require.NoError(t, err)
	bSet := bucketsOf(bucketB)
	require.Len(t, bSet, 1, "ComputeStats under beta must see exactly beta's single day bucket, not alpha's")

	// Alpha's bucket and beta's bucket must be disjoint — alpha never sees beta's.
	for label := range aSet {
		_, leak := bSet[label]
		require.False(t, leak, "alpha's stats bucket %q must not also appear under beta", label)
	}
}

// testReadWriteNakedContextFailClosed proves GetOne, Delete, the in-place
// mutators, and a by-uid Write all fail closed (ErrNoTenant) on a scoped
// collection when ctx carries neither a tenant nor platform scope.
func testReadWriteNakedContextFailClosed(t *testing.T, drv db.Driver) {
	naked := context.Background()

	_, err := drv.GetOne(naked, "record", db.Document{"uid": "x"})
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "GetOne on naked ctx must fail closed, got: %v", err)

	_, err = drv.Delete(naked, "record", condition.Equals("a", "1"), false)
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "Delete on naked ctx must fail closed, got: %v", err)

	_, err = drv.IncMany(naked, "record", "n", condition.Equals("a", "1"), 1)
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "IncMany on naked ctx must fail closed, got: %v", err)

	_, err = drv.SetFields(naked, "record", db.Document{"n": "1"}, condition.Equals("a", "1"))
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "SetFields on naked ctx must fail closed, got: %v", err)

	_, err = drv.UnsetFields(naked, "record", []string{"n"}, condition.Equals("a", "1"))
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "UnsetFields on naked ctx must fail closed, got: %v", err)

	_, err = drv.AppendList(naked, "record", map[string][]any{"l": {"x"}}, condition.Equals("a", "1"))
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "AppendList on naked ctx must fail closed, got: %v", err)

	_, err = drv.PrependList(naked, "record", map[string][]any{"l": {"x"}}, condition.Equals("a", "1"))
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "PrependList on naked ctx must fail closed, got: %v", err)

	_, err = drv.RemoveList(naked, "record", map[string][]any{"l": {"x"}}, condition.Equals("a", "1"))
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "RemoveList on naked ctx must fail closed, got: %v", err)

	// Write carrying a uid on a scoped collection must also fail closed.
	_, err = drv.Write(naked, "record",
		[]db.Document{{"uid": "x", "msg": "y"}}, db.WriteOptions{UpdateTime: false})
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "by-uid Write on naked ctx must fail closed, got: %v", err)
}

// testCleanupNakedContextFailClosed proves every Cleanup* and ComputeStats fails
// closed (ErrNoTenant) on a scoped collection with a naked context. The
// housekeeper always invokes these under a per-tenant ctx, so a naked-ctx call is
// a programming error that must never silently operate cross-tenant. [H3/H4]
func testCleanupNakedContextFailClosed(t *testing.T, drv db.Driver) {
	naked := context.Background()
	// Seed via platform scope so the collections exist (a non-existent collection
	// short-circuits to a no-op before the fail-closed check on some backends).
	plat := snoozetypes.WithPlatformScope(context.Background())
	mustWriteCtx(plat, t, drv, "record", db.Document{"host": "h", "ttl": int64(0), "date_epoch": float64(0)})
	mustWriteCtx(plat, t, drv, "comment", db.Document{"record_uid": "gone"})
	mustWriteCtx(plat, t, drv, "audit",
		db.Document{"object_id": "o", "action": "delete", "date_epoch": float64(1)})
	mustWriteCtx(plat, t, drv, "snooze", db.Document{"name": "s",
		"time_constraints": map[string]any{"datetime": []any{map[string]any{"until": "2000-01-01T00:00:00Z"}}}})
	mustWriteCtx(plat, t, drv, "notification", db.Document{"name": "n",
		"time_constraints": map[string]any{"datetime": []any{map[string]any{"until": "2000-01-01T00:00:00Z"}}}})
	mustWriteCtx(plat, t, drv, "node", db.Document{"name": "c", "parents": []any{"missing"}})
	mustWriteCtx(plat, t, drv, "stats",
		db.Document{"date": time.Now().Add(-time.Hour).UTC(), "key": "k", "value": int64(1)})

	_, err := drv.CleanupTimeout(naked, "record")
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "CleanupTimeout on naked ctx must fail closed, got: %v", err)

	_, err = drv.CleanupComments(naked)
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "CleanupComments on naked ctx must fail closed, got: %v", err)

	_, err = drv.CleanupOrphans(naked, "node")
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "CleanupOrphans on naked ctx must fail closed, got: %v", err)

	_, err = drv.CleanupAuditLogs(naked, time.Minute)
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "CleanupAuditLogs on naked ctx must fail closed, got: %v", err)

	_, err = drv.CleanupSnooze(naked)
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "CleanupSnooze on naked ctx must fail closed, got: %v", err)

	_, err = drv.CleanupNotification(naked)
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "CleanupNotification on naked ctx must fail closed, got: %v", err)

	_, err = drv.ComputeStats(naked, "stats", time.Unix(0, 0), time.Now(), "day")
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant, "ComputeStats on naked ctx must fail closed, got: %v", err)
}
