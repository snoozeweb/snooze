// Package dbtest provides a backend-agnostic conformance test pack that
// every db.Driver implementation must pass. Driver tests wire a factory and
// call RunDriverSuite.
package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
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
		{"SearchOnlyOne", testSearchOnlyOne},
		{"Update", testUpdate},
		{"Replace", testReplace},
		{"Delete", testDelete},
		{"DeleteAllRequiresForce", testDeleteAllRequiresForce},
		{"BulkIncrement", testBulkIncrement},
		{"BulkIncrementUpsert", testBulkIncrementUpsert},
		{"IncMany", testIncMany},
		{"SetFields", testSetFields},
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
		{"ComputeStats", testComputeStats},
	}
	for _, c := range cases {
		c := c
		t.Run(name+"/"+c.name, func(t *testing.T) {
			drv, teardown := factory(t)
			defer teardown()
			c.run(t, drv)
		})
	}
}

func ctx() context.Context { return context.Background() }

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
		c := c
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
	// Drivers may differ on ttl=1 / no date_epoch handling; we only assert
	// that the obvious-expired pair is removed.
	require.GreaterOrEqual(t, deleted, 2)
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
	mustWrite(t, drv, "audit",
		db.Document{"id": "a", "date_epoch": float64(100)},
		db.Document{"id": "b", "date_epoch": float64(200)},
	)
	_, err := drv.CleanupAuditLogs(ctx(), time.Minute)
	require.NoError(t, err)
}

func testComputeStats(t *testing.T, drv db.Driver) {
	mustWrite(t, drv, "stats", db.Document{"date": float64(0), "key": "a_qty", "value": int64(1)})
	_, err := drv.ComputeStats(ctx(), "stats", time.Unix(0, 0), time.Now(), "day")
	require.NoError(t, err)
}
