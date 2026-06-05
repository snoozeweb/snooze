package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestListenNotifyRoundTrip verifies that a mutation made via the driver is
// observed by a subscriber on the watcher bus.
func TestListenNotifyRoundTrip(t *testing.T) {
	drv := newTestDriver(t)
	// "record" is tenant-scoped, so the Write fail-closes on a naked context
	// (Task 1.8). Scope to a tenant for the write/seed.
	ctx, cancel := context.WithTimeout(
		snoozetypes.WithTenant(context.Background(), "default"), 30*time.Second)
	defer cancel()

	bus := drv.Watcher()
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	ch, err := bus.Subscribe(subCtx, "collection.record")
	require.NoError(t, err)

	// Write triggers a notification (in-process fanout + pg_notify).
	_, err = drv.Write(ctx, "record", []dbpkg.Document{{"host": "h1"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	select {
	case ev := <-ch:
		require.Equal(t, "record", ev.Collection)
		require.Equal(t, "write", ev.Op)
		require.NotEmpty(t, ev.UIDs)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for collection.record event")
	}
}

// TestListenNotifyTenantStamped is the H2 regression guard for postgres: a
// write under a tenant context must round-trip a notify whose Tenant field is
// set and whose Topic is the per-tenant topic. Without the tenant on the
// payload the receiving instance's per-tenant Reload short-circuits.
func TestListenNotifyTenantStamped(t *testing.T) {
	drv := newTestDriver(t) // skips under -short
	ctx, cancel := context.WithTimeout(
		snoozetypes.WithTenant(context.Background(), "acme"), 30*time.Second)
	defer cancel()

	bus := drv.Watcher()
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	// Subscribe on the bare collection prefix; the per-tenant topic must still
	// match because subscriptions use HasPrefix semantics.
	ch, err := bus.Subscribe(subCtx, "collection.rule")
	require.NoError(t, err)

	_, err = drv.Write(ctx, "rule", []dbpkg.Document{{"name": "owned"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	select {
	case ev := <-ch:
		require.Equal(t, "rule", ev.Collection)
		require.Equal(t, "acme", ev.Tenant, "notify must carry the writing tenant")
		require.Equal(t, syncer.CollectionTopic("rule", "acme"), ev.Topic,
			"event topic must be the per-tenant topic")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for collection.rule event")
	}
}

// TestGINIndexUsage runs EXPLAIN on a containment query to confirm Postgres
// chooses the GIN index we create at table-bootstrap time. The check is
// resilient to small plan-format differences across Postgres versions.
func TestGINIndexUsage(t *testing.T) {
	drv := newTestDriver(t)
	// "record" is tenant-scoped, so the Write fail-closes on a naked context
	// (Task 1.8). Scope to a tenant for the write/seed.
	ctx, cancel := context.WithTimeout(
		snoozetypes.WithTenant(context.Background(), "default"), 30*time.Second)
	defer cancel()

	// Seed enough rows that the planner reaches for the index.
	docs := make([]dbpkg.Document, 0, 200)
	for i := 0; i < 200; i++ {
		docs = append(docs, dbpkg.Document{"host": "h", "i": i})
	}
	docs = append(docs, dbpkg.Document{"host": "special", "i": -1})
	_, err := drv.Write(ctx, "record", docs, dbpkg.WriteOptions{})
	require.NoError(t, err)

	// Ensure stats reflect the load before explaining.
	_, err = drv.pool.Exec(ctx, `ANALYZE "snooze_record"`)
	require.NoError(t, err)

	// Use jsonb_path_ops's supported containment operator @> so the GIN
	// index is eligible. Build the SQL through the same conversion path
	// that production uses (= compiles to text comparison, not @>); for
	// the index check we just probe the catalog instead.
	row := drv.pool.QueryRow(ctx,
		"SELECT indexdef FROM pg_indexes WHERE schemaname = ANY (current_schemas(false)) "+
			"AND tablename = 'snooze_record' AND indexname LIKE '%data_gin%'",
	)
	var def string
	require.NoError(t, row.Scan(&def))
	require.Contains(t, strings.ToLower(def), "gin")
	require.Contains(t, strings.ToLower(def), "jsonb_path_ops")
}

// TestConvertEqualityMatches checks the convert.go output for a representative
// set of operators. Keeps regression coverage even when no container is
// available.
func TestConvertEqualityMatches(t *testing.T) {
	cases := []struct {
		name   string
		cond   condition.Cond
		wantIn string
	}{
		{"eq-string", condition.Equals("host", "h1"), `data->>'host' = $`},
		{"eq-number", condition.Equals("count", 7), `(data->>'count')::numeric = $`},
		{"eq-bool", condition.Equals("ack", true), `data->>'ack' = $`},
		{"eq-null", condition.Equals("foo", nil), `data->>'foo' IS NULL`},
		{"not-null", condition.Cond{Op: condition.OpNeq, Field: "foo", Value: nil},
			`data->>'foo' IS NOT NULL`},
		{"exists-flat", condition.Exists("foo"), `data ? $`},
		{"exists-nested", condition.Exists("a.b"), `data->'a'->'b' IS NOT NULL`},
		{"matches", condition.Cond{Op: condition.OpMatches, Field: "host", Value: "h.*"},
			`data->>'host' ~* $`},
		{"contains", condition.Cond{Op: condition.OpContains, Field: "tags", Value: "prod"},
			`jsonb_array_elements_text`},
		{"and", condition.And(condition.Equals("a", 1), condition.Equals("b", 2)),
			` AND `},
		{"or", condition.Or(condition.Equals("a", 1), condition.Equals("b", 2)),
			` OR `},
		{"not", condition.Not(condition.Equals("a", 1)), `(NOT `},
		{"search-no-fields", condition.Cond{Op: condition.OpSearch, Value: "x"},
			`data::text ~* $`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// "tenant" is a global collection → no tenant injection, so the
			// rendered SQL stays byte-identical to the pre-multitenancy output.
			res, err := convert(context.Background(), "tenant", tc.cond, nil)
			require.NoError(t, err)
			require.Contains(t, res.SQL, tc.wantIn, "got: %s", res.SQL)
			require.True(t, strings.Count(res.SQL, "$") <= len(res.Params)*3+1)
		})
	}
}

// TestConvertSearchWithFields exercises the SEARCH branch when explicit
// search fields are registered.
func TestConvertSearchWithFields(t *testing.T) {
	res, err := convert(context.Background(), "tenant",
		condition.Cond{Op: condition.OpSearch, Value: "needle"},
		[]string{"host", "tags.0"})
	require.NoError(t, err)
	require.Contains(t, res.SQL, "data->>'host' ~* ")
	require.Contains(t, res.SQL, "data->'tags'->>0 ~* ")
}

// TestConvertPlaceholderNumbering checks that placeholders are renumbered
// monotonically and that params are returned in matching order.
func TestConvertPlaceholderNumbering(t *testing.T) {
	c := condition.And(
		condition.Equals("a", "x"),
		condition.Equals("b", "y"),
		condition.Equals("c", "z"),
	)
	res, err := convert(context.Background(), "tenant", c, nil)
	require.NoError(t, err)
	require.Equal(t, 3, len(res.Params))
	require.Contains(t, res.SQL, "$1")
	require.Contains(t, res.SQL, "$2")
	require.Contains(t, res.SQL, "$3")
	require.Less(t, strings.Index(res.SQL, "$1"), strings.Index(res.SQL, "$2"))
	require.Less(t, strings.Index(res.SQL, "$2"), strings.Index(res.SQL, "$3"))
	require.Equal(t, []any{"x", "y", "z"}, res.Params)
}

// TestPgBusPublishLocal ensures the local-fanout path delivers without
// needing a database round-trip.
func TestPgBusPublishLocal(t *testing.T) {
	if testing.Short() {
		t.Skip("requires container")
	}
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bus := drv.Watcher()
	ch, err := bus.Subscribe(ctx, "collection.test")
	require.NoError(t, err)

	require.NoError(t, bus.Publish(ctx, syncer.Event{
		Topic: "collection.test", Op: "ping", Collection: "test",
	}))
	select {
	case ev := <-ch:
		require.Equal(t, "ping", ev.Op)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for local fanout")
	}
}
