// Package dbtest provides a backend-agnostic conformance test pack that
// every db.Driver implementation must pass. Driver tests wire a factory and
// call RunDriverSuite.
package dbtest

import (
	"context"
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
