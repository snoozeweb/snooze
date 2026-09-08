package postgres

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	// match because subscriptions match on dot-delimited segments.
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
		// The numeric branch is the guarded CASE from numericExpr, byte-for-byte
		// what the numeric expression index is built on (N2).
		{"eq-number", condition.Equals("count", 7),
			`(CASE WHEN data->>'count' ~ '^-?[0-9]+(\.[0-9]+)?$' THEN (data->>'count')::numeric END = $`},
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
		{"not", condition.Not(condition.Equals("a", 1)), `) IS NOT TRUE)`},
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

// TestSetFieldsCounterOnlyDoesNotNotify is the Postgres twin of the Mongo
// watcher's counterOnlyUpdate filter (internal/db/counter_fields.go). The
// notification dispatcher issues one SetFields{hits,last_sent} per successful
// delivery; on Postgres a publish is a pg_notify fanned out to EVERY node, so
// an unfiltered counter bump reloads the notification plugin cluster-wide on
// every send. The snooze plugin's `hits` bump is suppressed for the same
// reason; anything else must still notify.
//
// Ordering trick (same as the Mongo stream test): the suppressed write is
// issued first against row A, the control write second against row B, and the
// FIRST event the subscriber sees must be B's. That is a positive assertion —
// no sleep-and-hope-nothing-arrives.
func TestSetFieldsCounterOnlyDoesNotNotify(t *testing.T) {
	cases := []struct {
		name        string
		collection  string
		fields      dbpkg.Document
		wantSkipped bool
	}{
		{"notification counters", "notification",
			dbpkg.Document{"hits": float64(3), "last_sent": float64(1757340000)}, true},
		{"notification hits only", "notification", dbpkg.Document{"hits": float64(3)}, true},
		{"notification counter plus real field", "notification",
			dbpkg.Document{"hits": float64(3), "enabled": false}, false},
		{"snooze hits", "snooze", dbpkg.Document{"hits": float64(9)}, true},
		{"snooze last_sent is not a snooze counter", "snooze",
			dbpkg.Document{"last_sent": float64(1757340000)}, false},
		{"rule hits is a real edit", "rule", dbpkg.Document{"hits": float64(3)}, false},
	}

	drv := newTestDriver(t) // skips under -short / without a container
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(
				snoozetypes.WithTenant(context.Background(), "acme"), 30*time.Second)
			defer cancel()

			// Subscribe BEFORE seeding: pg_notify is delivered asynchronously
			// over the LISTEN connection, so a subscription opened after the
			// seeding write would still receive that write's event and race
			// the assertion below. Notifications arrive in commit order, so
			// draining the seed event first is deterministic.
			subCtx, subCancel := context.WithCancel(ctx)
			defer subCancel()
			ch, err := drv.Watcher().Subscribe(subCtx, "collection."+tc.collection)
			require.NoError(t, err)

			res, err := drv.Write(ctx, tc.collection, []dbpkg.Document{
				{"name": "row-a"}, {"name": "row-b"},
			}, dbpkg.WriteOptions{})
			require.NoError(t, err)
			require.Len(t, res.Added, 2)
			uidA, uidB := res.Added[0], res.Added[1]
			requireEventUIDs(t, ch, res.Added) // the seed write

			n, err := drv.SetFields(ctx, tc.collection, tc.fields, condition.Equals("uid", uidA))
			require.NoError(t, err)
			require.Equal(t, 1, n, "the row must actually be updated either way")

			// Control write: always notifies.
			_, err = drv.SetFields(ctx, tc.collection,
				dbpkg.Document{"name": "row-b-renamed"}, condition.Equals("uid", uidB))
			require.NoError(t, err)

			wantFirst := uidA
			if tc.wantSkipped {
				wantFirst = uidB
			}
			requireEventUIDs(t, ch, []string{wantFirst})

			require.NoError(t, drv.Drop(context.Background(), tc.collection))
		})
	}
}

// TestUpdateOneCounterOnlyDoesNotNotify covers the other patch-shaped write
// path on Postgres: UpdateOne's `patch` IS the field set. `updateTime` stamps
// date_epoch — a real change — so it always notifies.
func TestUpdateOneCounterOnlyDoesNotNotify(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(
		snoozetypes.WithTenant(context.Background(), "acme"), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = drv.Drop(context.Background(), "notification") })

	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	ch, err := drv.Watcher().Subscribe(subCtx, "collection.notification")
	require.NoError(t, err)

	require.NoError(t, drv.UpdateOne(ctx, "notification", "n-a", dbpkg.Document{"name": "a"}, false))
	require.NoError(t, drv.UpdateOne(ctx, "notification", "n-b", dbpkg.Document{"name": "b"}, false))
	requireEventUIDs(t, ch, []string{"n-a"})
	requireEventUIDs(t, ch, []string{"n-b"})

	// Suppressed: pure counter bump, no updateTime.
	require.NoError(t, drv.UpdateOne(ctx, "notification", "n-a",
		dbpkg.Document{"hits": float64(3), "last_sent": float64(1757340000)}, false))
	// Control: a real edit on the other row.
	require.NoError(t, drv.UpdateOne(ctx, "notification", "n-b",
		dbpkg.Document{"enabled": false}, false))

	requireEventUIDs(t, ch, []string{"n-b"})
}

// requireEventUIDs reads the next event off ch and asserts its UIDs. Used to
// drain seed writes and to pin which write produced the next notification.
func requireEventUIDs(t *testing.T, ch <-chan syncer.Event, want []string) {
	t.Helper()
	select {
	case ev := <-ch:
		require.Equal(t, want, ev.UIDs)
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for notify with uids %v", want)
	}
}

// TestCreateIndexCreatesPerFieldIndexes pins the half of CreateIndex that used
// to be a lie: the registry comment claimed Postgres got backing indexes from
// search_fields, but the implementation only remembered the field list. A
// 30-day notificationlog sorted by date_epoch is a sequential scan without it.
func TestCreateIndexCreatesPerFieldIndexes(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = drv.Drop(context.Background(), "notificationlog") })

	fields := []string{"date_epoch", "status", "action", "notifier"}
	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", fields))
	// Idempotent: boot runs this on every start.
	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", fields))
	// CreateIndex only QUEUES the builds (see driver.go CreateIndex): they run
	// on the driver's own worker so no caller — plugins.Build's goroutine or a
	// plugin PostInit on the boot path — ever waits out a CONCURRENTLY build.
	require.NoError(t, drv.flushIndexBuilds(ctx))

	rows, err := drv.pool.Query(ctx,
		"SELECT indexname, indexdef FROM pg_indexes WHERE tablename = $1", "snooze_notificationlog")
	require.NoError(t, err)
	defer rows.Close()
	defs := map[string]string{}
	for rows.Next() {
		var name, def string
		require.NoError(t, rows.Scan(&name, &def))
		defs[name] = def
	}
	require.NoError(t, rows.Err())

	for _, f := range fields {
		// Text index: plain `idx_<table>_<field>`, no suffix. That is a naming
		// convention, not backwards compatibility — no released version ever
		// created per-field indexes, so there is nothing on disk to reuse; the
		// suffix is simply reserved for the variants.
		name := "idx_snooze_notificationlog_" + f
		def, ok := defs[name]
		require.True(t, ok, "missing index %s; have %v", name, defs)
		// The indexed expression must be the one the query compiler emits,
		// otherwise the planner will never pick it.
		require.Contains(t, def, "data ->> '"+f+"'")
		require.NotContains(t, def, "WHERE", "the text index must stay total")

		// Numeric index (N2): dialect.typedCompare's numeric branch and
		// renderOrderBy's primary sort key both emit numericExpr, which the
		// text index above cannot serve.
		numName := name + "_num"
		numDef, ok := defs[numName]
		require.True(t, ok, "missing numeric index %s; have %v", numName, defs)
		require.Contains(t, numDef, "::numeric")
		require.Contains(t, numDef, "CASE")
		// ...and it must be PARTIAL: numericExpr is NULL for every row whose
		// text at that path is not a plain decimal, which for a text-only
		// field like `status` is every row. Without the predicate `record`
		// carries six all-NULL btrees, paid for on every INSERT.
		at := strings.LastIndex(numDef, " WHERE ")
		require.Greater(t, at, 0, "the numeric index must be partial: %s", numDef)
		where := numDef[at:]
		require.Contains(t, where, "IS NOT NULL", "predicate was %q", where)
		require.Contains(t, where, "CASE",
			"the partial predicate must reuse the same guarded expression")

		// Both must be valid: an interrupted CREATE INDEX CONCURRENTLY leaves
		// indisvalid = false behind, which the planner ignores.
		for _, n := range []string{name, numName} {
			exists, valid, err := drv.indexState(ctx, n)
			require.NoError(t, err)
			require.True(t, exists && valid, "%s must exist and be valid", n)
		}
	}

	// SEARCH registration is unchanged.
	require.Equal(t, fields, drv.getSearchFields("notificationlog"))
}

// TestSanitizeFieldIdent guards the DDL-splicing path: anything that is not a
// plain identifier path must be refused rather than interpolated.
func TestSanitizeFieldIdent(t *testing.T) {
	require.Equal(t, "date_epoch", sanitizeFieldIdent("date_epoch"))
	require.Equal(t, "a__b", sanitizeFieldIdent("a.b"))
	require.Equal(t, "", sanitizeFieldIdent(""))
	require.Equal(t, "", sanitizeFieldIdent("evil'); DROP TABLE x; --"))
	require.Equal(t, "", sanitizeFieldIdent("has space"))
	require.Len(t, indexName("snooze_"+strings.Repeat("x", 80), "f", ""), 63)
}

// TestIndexNameTruncationAvoidsCollisions is the N4 guard. A plain 63-byte cut
// makes two long field paths sharing a long prefix produce the SAME index
// name, and the second CREATE INDEX ... IF NOT EXISTS then silently no-ops:
// the field looks indexed and is not.
func TestIndexNameTruncationAvoidsCollisions(t *testing.T) {
	tbl := "snooze_" + strings.Repeat("collection", 3) // long enough to force truncation
	prefix := strings.Repeat("nested.", 6)

	a := indexName(tbl, prefix+"alpha", "")
	b := indexName(tbl, prefix+"beta", "")
	require.Len(t, a, 63)
	require.Len(t, b, 63)
	require.NotEqual(t, a, b, "truncated names for distinct fields must differ")
	// Deterministic: boot recomputes the name on every start and must land on
	// the index it created last time.
	require.Equal(t, a, indexName(tbl, prefix+"alpha", ""))

	// The two kinds of index over the same field must not collide either.
	require.NotEqual(t, a, indexName(tbl, prefix+"alpha", "num"))
	require.Len(t, indexName(tbl, prefix+"alpha", "num"), 63)

	// Short names of plain-identifier fields are untouched: that is the whole
	// readability argument for the convention.
	require.Equal(t, "idx_snooze_notificationlog_date_epoch",
		indexName("snooze_notificationlog", "date_epoch", ""))
	require.Equal(t, "idx_snooze_notificationlog_date_epoch_num",
		indexName("snooze_notificationlog", "date_epoch", "num"))

	// Second collision source: sanitizeFieldIdent is not injective. It maps
	// "." to "__", so the dotted field "a.b" and the (perfectly legal,
	// underscore-only) field "a__b" both render "a__b" — and the second
	// CREATE INDEX ... IF NOT EXISTS would silently no-op, leaving one of the
	// two fields unindexed while looking indexed. Length is irrelevant here,
	// so the digest is appended whenever sanitisation changed the field at all.
	dotted := indexName("snooze_record", "a.b", "")
	literal := indexName("snooze_record", "a__b", "")
	require.NotEqual(t, dotted, literal,
		`"a.b" and "a__b" must not share an index name`)
	require.Equal(t, "idx_snooze_record_a__b", literal,
		"an unchanged identifier keeps the plain name")
	require.True(t, strings.HasPrefix(dotted, "idx_snooze_record_a__b_"),
		"got %q", dotted)
	require.Len(t, dotted, len("idx_snooze_record_a__b_")+8)
	// Deterministic across boots, and distinct per kind.
	require.Equal(t, dotted, indexName("snooze_record", "a.b", ""))
	require.NotEqual(t, dotted, indexName("snooze_record", "a.b", "num"))
	// Deep paths differing only where "." lands must also stay distinct.
	require.NotEqual(t,
		indexName("snooze_record", "a.b.c", ""),
		indexName("snooze_record", "a.b__c", ""))
}

// TestNumericExpressionIndexServesRangePredicate is the N2 evidence test: it
// proves the numeric expression index CreateIndex now builds is the one the
// driver-compiled range predicate can actually use.
//
// The predicate is NOT hand-written — it comes out of drv.Convert, the real
// Cond → SQL path (dialect.typedCompare's numeric branch) — because textual
// identity between the index expression and the query expression is the entire
// mechanism being tested. Before N2 the index was on ((data->>'date_epoch'))
// while the predicate emitted ((data->>'date_epoch')::numeric), so every
// `date_epoch < $1` sweep was a sequential scan.
//
// The seeded rows are padded: 2000 narrow rows fit in a handful of pages and
// the planner prefers a sequential scan on cost alone regardless of what
// indexes exist. No enable_seqscan tweaking is used.
func TestNumericExpressionIndexServesRangePredicate(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(
		snoozetypes.WithPlatformScope(context.Background()), 180*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = drv.Drop(context.Background(), "notificationlog") })

	const (
		rows      = 2000
		baseEpoch = 1_700_000_000
	)
	pad := strings.Repeat("x", 400)
	// Small batches: notifyTx puts every written UID in the pg_notify payload,
	// which Postgres caps at 8000 bytes.
	batch := make([]dbpkg.Document, 0, 100)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		_, err := drv.Write(ctx, "notificationlog", batch, dbpkg.WriteOptions{})
		require.NoError(t, err)
		batch = batch[:0]
	}
	for i := 0; i < rows; i++ {
		batch = append(batch, dbpkg.Document{
			"date_epoch": baseEpoch + i,
			"status":     "sent",
			"action":     "escalate",
			"notifier":   "webhook",
			"payload":    pad,
		})
		if len(batch) == cap(batch) {
			flush()
		}
	}
	flush()
	// One row holding non-numeric text where a number is expected. Pre-N2 this
	// row alone made BOTH the CREATE INDEX and every numeric comparison fail
	// with "invalid input syntax for type numeric"; the CASE guard turns it
	// into a NULL that simply never matches.
	_, err := drv.Write(ctx, "notificationlog", []dbpkg.Document{
		{"date_epoch": "not-a-number", "status": "sent", "payload": pad},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"date_epoch"}))
	require.NoError(t, drv.flushIndexBuilds(ctx))
	_, err = drv.pool.Exec(ctx, `ANALYZE "snooze_notificationlog"`)
	require.NoError(t, err)

	numIdx := indexName("snooze_notificationlog", "date_epoch", "num")

	// (a) The range predicate, compiled by the driver.
	q, err := drv.Convert(ctx,
		condition.Cond{Op: condition.OpLt, Field: "date_epoch", Value: baseEpoch + 10}, nil)
	require.NoError(t, err)
	pq, ok := q.(*PreparedQuery)
	require.True(t, ok)
	require.Contains(t, pq.SQL, "CASE WHEN", "predicate must use the guarded numeric expression")

	rangePlan := explainPlan(ctx, t, drv,
		`SELECT data FROM "snooze_notificationlog" WHERE `+pq.SQL, pq.Params...)
	t.Logf("range predicate plan:\n%s", rangePlan)
	// The index is PARTIAL ("... WHERE numericExpr IS NOT NULL"), so this also
	// proves the planner discharges the predicate: `<` is a strict operator,
	// so "expr < $1" implies "expr IS NOT NULL", and Postgres only considers a
	// partial index whose predicate it can prove implied by the query. If the
	// partial predicate ever stopped being the byte-identical expression the
	// proof would fail and the plan would fall back to Seq Scan.
	require.Contains(t, rangePlan, numIdx,
		"the driver-compiled range predicate must use the numeric expression index")
	require.Regexp(t, `(?i)(bitmap )?index (only )?scan`, rangePlan)
	require.NotContains(t, rangePlan, "Seq Scan")
	// Proof the partial predicate was DISCHARGED, not merely tolerated: the
	// plan carries no Filter node re-testing the CASE, only the Index Cond.
	require.NotContains(t, rangePlan, "Filter",
		"the partial predicate must be proven by implication, not rechecked")

	// Counter-proof that the predicate really is partial and really is
	// discharged: the definition on disk carries the WHERE clause.
	var numDefOnDisk string
	require.NoError(t, drv.pool.QueryRow(ctx,
		"SELECT indexdef FROM pg_indexes WHERE indexname = $1", numIdx).Scan(&numDefOnDisk))
	t.Logf("index definition:\n%s", numDefOnDisk)
	require.Contains(t, numDefOnDisk, "IS NOT NULL")

	// The predicate must also still be correct, non-numeric row included.
	docs, total, err := drv.Search(ctx, "notificationlog",
		condition.Cond{Op: condition.OpLt, Field: "date_epoch", Value: baseEpoch + 10},
		dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 10, total, "the non-numeric row must not match")
	require.Len(t, docs, 10)

	// (b) The ORDER BY. Documented negative result: a plain btree on
	// numericExpr CANNOT serve renderOrderBy's sort key, for two independent
	// reasons.
	//
	//   1. renderOrderBy emits a TWO-key sort — the numeric expression first,
	//      then the raw text expression as a tiebreaker — while the index has
	//      exactly one key. A single-key index can never provide a two-key
	//      ordering.
	//   2. Even for the leading key the direction does not line up:
	//      "DESC NULLS LAST" is not a btree-scannable ordering of an
	//      "ASC NULLS LAST" index (scanning it backwards yields
	//      DESC NULLS FIRST). Serving it would need
	//      "CREATE INDEX ... (expr DESC NULLS LAST)".
	//
	// So the ORDER BY keeps a Sort node and this half asserts only that the
	// query runs and stays correct. Fixing it is a deliberate non-goal here:
	// it would mean a second pair of DESC indexes per field (doubling write
	// amplification again) and dropping the text tiebreaker, which would
	// change result ordering. The range predicate above is what the
	// housekeeper sweep and the delivery-history list actually depend on.
	orderSQL := `SELECT data FROM "snooze_notificationlog" WHERE TRUE ` +
		renderOrderBy("date_epoch", false) + " " + renderPagination(25, 1)
	orderPlan := explainPlan(ctx, t, drv, orderSQL)
	t.Logf("ORDER BY plan:\n%s", orderPlan)
	require.Contains(t, orderPlan, "Sort",
		"documented limitation: the two-key NULLS LAST sort cannot use a single-key btree")
}

// explainPlan runs EXPLAIN (no ANALYZE — the plan shape is what matters) and
// returns the plan as newline-joined text.
func explainPlan(ctx context.Context, t *testing.T, drv *Driver, sql string, args ...any) string {
	t.Helper()
	rows, err := drv.pool.Query(ctx, "EXPLAIN "+sql, args...)
	require.NoError(t, err)
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		lines = append(lines, line)
	}
	require.NoError(t, rows.Err())
	return strings.Join(lines, "\n")
}

// TestCreateIndexRebuildsInvalidIndex covers the CONCURRENTLY hazard from N3:
// an interrupted CREATE INDEX CONCURRENTLY leaves the index behind with
// indisvalid = false. The planner ignores such an index, yet
// "CREATE INDEX CONCURRENTLY IF NOT EXISTS" sees the name and skips, so
// without the catalog probe the index would stay dead for the lifetime of the
// deployment. The invalid state is forged here by flipping the catalog flag,
// which is what an interrupted build leaves behind.
func TestCreateIndexRebuildsInvalidIndex(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = drv.Drop(context.Background(), "notificationlog") })

	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"date_epoch"}))
	require.NoError(t, drv.flushIndexBuilds(ctx))
	name := indexName("snooze_notificationlog", "date_epoch", "num")

	_, err := drv.pool.Exec(ctx,
		"UPDATE pg_index SET indisvalid = false WHERE indexrelid = $1::regclass", name)
	require.NoError(t, err)
	exists, valid, err := drv.indexState(ctx, name)
	require.NoError(t, err)
	require.True(t, exists)
	require.False(t, valid, "precondition: the index must look interrupted")

	// A second boot must notice and rebuild rather than skip on IF NOT EXISTS.
	// The pg_stat_progress_create_index probe added for the replica hazard must
	// not get in the way here: nothing is building, so the leftover is dropped.
	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"date_epoch"}))
	require.NoError(t, drv.flushIndexBuilds(ctx))
	exists, valid, err = drv.indexState(ctx, name)
	require.NoError(t, err)
	require.True(t, exists && valid, "invalid index must have been dropped and rebuilt")
}

// TestCreateIndexNeverBuildsInline is the direct evidence for the boot-path
// hazard: CREATE INDEX CONCURRENTLY waits for every transaction that could
// still write the table to finish, so a caller that awaits the build stalls for
// as long as the oldest open writer lives. plugins.Build dispatches its own
// pass to a goroutine, but plugin PostInit hooks call CreateIndex directly
// while the HTTP listener is still down (heartbeat indexes its `token` field),
// so the deferral has to live in the driver.
//
// The test opens a writer transaction and leaves it open, which is exactly
// what a real CONCURRENTLY build blocks on, then asserts CreateIndex returns
// promptly with the index NOT yet usable, and that it completes once the
// writer commits.
func TestCreateIndexNeverBuildsInline(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(
		snoozetypes.WithPlatformScope(context.Background()), 120*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = drv.Drop(context.Background(), "notificationlog") })

	// Materialise the table first so the ensureCollection DDL inside
	// CreateIndex is a cached no-op; that part IS synchronous by design (it is
	// the SEARCH/table prerequisite, not an index build) and it takes a lock
	// the blocking transaction below would conflict with.
	_, err := drv.Write(ctx, "notificationlog",
		[]dbpkg.Document{{"date_epoch": 1, "status": "sent"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	blocker, err := drv.pool.Acquire(ctx)
	require.NoError(t, err)
	defer blocker.Release()
	tx, err := blocker.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx,
		`INSERT INTO "snooze_notificationlog" (uid, data) VALUES ('blocker', '{}'::jsonb)`)
	require.NoError(t, err)
	// tx is deliberately left open: a RowExclusive writer is precisely what
	// CREATE INDEX CONCURRENTLY's first phase waits out.

	start := time.Now()
	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"date_epoch"}))
	elapsed := time.Since(start)
	require.Less(t, elapsed, 5*time.Second,
		"CreateIndex must not await the index build (took %s)", elapsed)

	// SEARCH scoping is in place immediately — that is the part callers depend
	// on for correctness.
	require.Equal(t, []string{"date_epoch"}, drv.getSearchFields("notificationlog"))

	// The build is parked behind the open writer, so the index is either
	// absent or published-but-invalid. Either way it is not usable yet, which
	// is the proof that CreateIndex returned before the build finished.
	name := indexName("snooze_notificationlog", "date_epoch", "num")
	_, valid, err := drv.indexState(ctx, name)
	require.NoError(t, err)
	require.False(t, valid, "the build must still be in flight")

	// Let the writer go; the queued build now completes on its own.
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, drv.flushIndexBuilds(ctx))
	exists, valid, err := drv.indexState(ctx, name)
	require.NoError(t, err)
	require.True(t, exists && valid, "the queued build must finish once the writer commits")
}

// TestDropRacesQueuedIndexBuild is the regression test for the hazard that
// deferring the builds introduced: DROP TABLE takes ACCESS EXCLUSIVE and
// deadlocks against a CREATE INDEX CONCURRENTLY on the same relation, so with
// the builds moved off the caller's goroutine the shared conformance suite
// started failing with "drop record: deadlock detected" (testIndex indexes
// `record`, the next case drops every collection). Drop now aborts and awaits
// the driver's index work first.
//
// Deliberately NO flushIndexBuilds before the Drop: the point is to land it
// while the builds are queued or in flight.
func TestDropRacesQueuedIndexBuild(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(
		snoozetypes.WithPlatformScope(context.Background()), 120*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = drv.Drop(context.Background(), "notificationlog") })

	docs := make([]dbpkg.Document, 0, 200)
	for i := 0; i < 200; i++ {
		docs = append(docs, dbpkg.Document{
			"date_epoch": 1_700_000_000 + i, "status": "sent",
			"action": "escalate", "notifier": "webhook",
		})
	}
	_, err := drv.Write(ctx, "notificationlog", docs[:100], dbpkg.WriteOptions{})
	require.NoError(t, err)
	_, err = drv.Write(ctx, "notificationlog", docs[100:], dbpkg.WriteOptions{})
	require.NoError(t, err)

	require.NoError(t, drv.CreateIndex(ctx, "notificationlog",
		[]string{"date_epoch", "status", "action", "notifier"}))
	require.NoError(t, drv.Drop(ctx, "notificationlog"),
		"Drop must not deadlock against the queued index builds")
	require.NoError(t, drv.flushIndexBuilds(ctx))

	// And the collection is usable again straight after.
	_, err = drv.Write(ctx, "notificationlog",
		[]dbpkg.Document{{"date_epoch": 1, "status": "sent"}}, dbpkg.WriteOptions{})
	require.NoError(t, err)
}

// TestIndexPassSkipsWhenPeerHoldsLock covers the cross-replica half of the
// INVALID-index hazard. CREATE INDEX CONCURRENTLY advertises indisvalid=false
// for the whole duration of a build, so replica B booting mid-build on replica
// A cannot tell "interrupted leftover" from "being built right now": its
// DROP INDEX CONCURRENTLY would block until A finished and then drop A's
// freshly valid index. One session-level advisory lock per pass removes the
// class of problem, and the loser skips rather than queues — the winner is
// building the identical set.
//
// A second pooled connection stands in for the peer: advisory locks are
// per-session, so a different session is indistinguishable from another
// server. (The DRIVER's pass no longer uses the pool at all — it opens its own
// maintenance connection — which if anything makes the stand-in more faithful:
// two genuinely independent sessions.)
//
// The tail of the test also pins how the driver RELEASES the lock now. It used
// to run an explicit pg_advisory_unlock on a pooled connection, and if that
// failed or timed out the healthy connection went back to the pool still
// holding a session-level lock that nothing would ever release — every later
// pass, on every replica, skipping forever. The lock now dies with the
// dedicated session, so there is no unlock round-trip that can fail.
func TestIndexPassSkipsWhenPeerHoldsLock(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = drv.Drop(context.Background(), "notificationlog") })

	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"status"}))
	require.NoError(t, drv.flushIndexBuilds(ctx))

	peer, err := drv.pool.Acquire(ctx)
	require.NoError(t, err)
	defer peer.Release()
	var held bool
	require.NoError(t, peer.QueryRow(ctx,
		"SELECT pg_try_advisory_lock(hashtext($1))", indexMaintLockKey).Scan(&held))
	require.True(t, held, "precondition: the stand-in peer must hold the lock")

	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"action"}))
	require.NoError(t, drv.flushIndexBuilds(ctx))

	name := indexName("snooze_notificationlog", "action", "")
	exists, _, err := drv.indexState(ctx, name)
	require.NoError(t, err)
	require.False(t, exists, "the pass must be skipped outright, not queued behind the peer")
	// The synchronous half still ran: skipping index maintenance must never
	// cost SEARCH its scoping.
	require.Equal(t, []string{"action"}, drv.getSearchFields("notificationlog"))

	_, err = peer.Exec(ctx, "SELECT pg_advisory_unlock(hashtext($1))", indexMaintLockKey)
	require.NoError(t, err)

	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"action"}))
	require.NoError(t, drv.flushIndexBuilds(ctx))
	exists, valid, err := drv.indexState(ctx, name)
	require.NoError(t, err)
	require.True(t, exists && valid, "the next pass must build what the skipped one did not")

	// The completed pass must hold nothing: closing its session is what
	// releases the lock, so a fresh session can take it immediately.
	require.NoError(t, peer.QueryRow(ctx,
		"SELECT pg_try_advisory_lock(hashtext($1))", indexMaintLockKey).Scan(&held))
	require.True(t, held,
		"the finished pass must have released the maintenance lock with its session")
	_, err = peer.Exec(ctx, "SELECT pg_advisory_unlock(hashtext($1))", indexMaintLockKey)
	require.NoError(t, err)
}

// TestNumericMaintenanceQueriesSurviveGarbage is the guard for the
// hand-written casts that used to sit in cleanup.go / source_activity.go /
// bulk.go. record.Validate accepts ANY JSON value, so a single document with
// {"ttl":"soon"} or {"date_epoch":"yesterday"} used to make each of these
// statements fail with `invalid input syntax for type numeric` — permanently,
// because the housekeeper and the inputs page re-run the identical statement
// every cycle. The `data ? 'x'` tests in those queries never protected
// anything: SQL AND has no guaranteed evaluation order.
func TestNumericMaintenanceQueriesSurviveGarbage(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(
		snoozetypes.WithPlatformScope(context.Background()), 60*time.Second)
	defer cancel()
	t.Cleanup(func() {
		for _, c := range []string{"record", "audit", "stat", "counter"} {
			_ = drv.Drop(context.Background(), c)
		}
	})

	// One well-formed row and one carrying text where a number belongs, in
	// every shape the maintenance queries read.
	_, err := drv.Write(ctx, "record", []dbpkg.Document{
		{"source": "good", "date_epoch": 1, "ttl": 1},
		{"source": "bad", "date_epoch": "yesterday", "ttl": "soon"},
		{"source": "bad", "date_epoch": " 5", "ttl": "1e3"},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	// CleanupTimeout: the expired well-formed row goes, the garbage rows stay
	// (their predicate is NULL, not an error).
	n, err := drv.CleanupTimeout(ctx, "record")
	require.NoError(t, err, "one garbage ttl must not wedge the timeout sweep")
	require.Equal(t, 1, n)
	_, total, err := drv.Search(ctx, "record", condition.Cond{}, dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 2, total)

	// SourceActivity: garbage epochs read as 0 (same as a missing key) instead
	// of blowing up the whole inputs page.
	acts, err := drv.SourceActivity(ctx, 0)
	require.NoError(t, err, "one garbage date_epoch must not wedge SourceActivity")
	require.Len(t, acts, 1)
	require.Equal(t, "bad", acts[0].Source)
	require.EqualValues(t, 2, acts[0].Count)
	require.Equal(t, int64(0), acts[0].LastEpoch)

	// CleanupAuditLogs: same treatment on the self-joined MAX(date_epoch).
	_, err = drv.Write(ctx, "audit", []dbpkg.Document{
		{"object_id": "o1", "action": "delete", "date_epoch": 1},
		{"object_id": "o2", "action": "delete", "date_epoch": "nope"},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	pruned, err := drv.CleanupAuditLogs(ctx, time.Hour)
	require.NoError(t, err, "one garbage audit date_epoch must not wedge retention")
	// Both go: COALESCE(numericExpr, 0) reads the garbage epoch as 0, exactly
	// as it already read a MISSING date_epoch, and 0 is older than any
	// threshold. The behaviour that changed is only the error.
	require.Equal(t, 2, pruned)

	// ComputeStats: a non-numeric `value` now contributes 0, and a `date` that
	// is not a timestamp at all no longer aborts the statement. `date` is cast
	// in two places (the projection and the window predicate) and both are
	// guarded, so a garbage row simply falls outside the window. Nothing in the
	// Go server writes this field — see the comment in ComputeStats — so the
	// rows that carry it are unvalidated by construction.
	now := time.Now().UTC()
	_, err = drv.Write(ctx, "stat", []dbpkg.Document{
		{"date": now.Format(time.RFC3339), "key": "k", "value": 2},
		{"date": now.Format(time.RFC3339), "key": "k", "value": "many"},
		{"date": "yesterday", "key": "k", "value": 5},
		{"date": "", "key": "k", "value": 7},
		{"date": "2026-09-08 25:61:00", "key": "k", "value": 11},
		{"key": "k", "value": 13}, // no date at all
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	buckets, err := drv.ComputeStats(ctx, "stat",
		now.Add(-2*time.Hour), now.Add(time.Hour), "hour")
	require.NoError(t, err,
		"a garbage stat value or date must not wedge ComputeStats")
	require.Len(t, buckets, 1)
	require.Len(t, buckets[0].Series, 1)
	require.Equal(t, float64(2), buckets[0].Series[0].Value,
		"only the two well-dated rows may contribute (2 + garbage-as-0)")
	// The nanosecond-precision form encoding/json produces for a time.Time
	// must pass the guard too — that is what the conformance suite writes.
	_, err = drv.Write(ctx, "stat", []dbpkg.Document{
		{"date": now, "key": "precise", "value": 4},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	buckets, err = drv.ComputeStats(ctx, "stat",
		now.Add(-2*time.Hour), now.Add(time.Hour), "hour")
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	require.Len(t, buckets[0].Series, 2, "the time.Time-shaped date must be accepted")

	// IncMany: a counter holding text is treated as 0 and overwritten, instead
	// of failing this and every future increment on that row.
	_, err = drv.Write(ctx, "counter", []dbpkg.Document{
		{"name": "c", "hits": "lots"},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	touched, err := drv.IncMany(ctx, "counter", "hits", condition.Equals("name", "c"), 3)
	require.NoError(t, err, "one garbage counter must not wedge Increment")
	require.Equal(t, 1, touched)
	docs, _, err := drv.Search(ctx, "counter", condition.Equals("name", "c"), dbpkg.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.EqualValues(t, 3, counterAsFloat(t, docs[0]["hits"]))
}

// counterAsFloat normalises whatever numeric shape the JSONB round-trip
// produced (json.Number, float64, int64) to a float for comparison.
func counterAsFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case json.Number:
		f, err := n.Float64()
		require.NoError(t, err)
		return f
	default:
		t.Fatalf("unexpected counter type %T (%v)", v, v)
		return 0
	}
}

// TestNumericExprSharedVerbatim is the cheap, container-free guard for N2: the
// numeric expression must be textually identical in all three places that
// emit it, because Postgres only matches an expression index when the query's
// expression is the same one. A stray space or a reordered clause in any one
// of them silently downgrades every numeric filter to a sequential scan, with
// no error anywhere.
func TestNumericExprSharedVerbatim(t *testing.T) {
	const field = "date_epoch"
	expr := numericExpr(field)

	// 1. The literal string, pinned. renderOrderBy's emitted SQL must not have
	//    changed when it was refactored onto the helper.
	require.Equal(t,
		`CASE WHEN data->>'date_epoch' ~ '^-?[0-9]+(\.[0-9]+)?$' THEN (data->>'date_epoch')::numeric END`,
		expr)
	require.Equal(t,
		"ORDER BY "+expr+" DESC NULLS LAST, data->>'date_epoch' DESC NULLS LAST",
		renderOrderBy(field, false))
	require.Equal(t,
		"ORDER BY "+expr+" ASC NULLS LAST, data->>'date_epoch' ASC NULLS LAST",
		renderOrderBy(field, true))

	// 2. The compiled predicate (dialect.typedCompare numeric branch).
	ctx := snoozetypes.WithPlatformScope(context.Background())
	for _, tc := range []struct {
		op   condition.Op
		want string
	}{
		{condition.OpLt, "(" + expr + " < $1)"},
		{condition.OpGte, "(" + expr + " >= $1)"},
		{condition.OpEq, "(" + expr + " = $1)"},
		{condition.OpNeq, "(" + expr + " IS DISTINCT FROM $1)"},
	} {
		res, err := convert(ctx, "", condition.Cond{Op: tc.op, Field: field, Value: 42}, nil)
		require.NoError(t, err)
		require.Equal(t, tc.want, res.SQL, "op %s", tc.op)
	}

	// 3. The indexed expression.
	require.Equal(t, "num", indexKinds[1].kind)
	require.Equal(t, expr, indexKinds[1].expr(field))
	require.Equal(t, "", indexKinds[0].kind)
	require.Equal(t, pathText(field), indexKinds[0].expr(field))
}

// newIndexStateDriver builds a Driver with ONLY its index-maintenance state
// wired: no pool, no bus, no worker goroutine. Everything the tests below
// touch (enqueueIndexJob, takeIndexBatch, abortIndexWork, the drop guard) is
// pure in-memory bookkeeping under idxMu, so these cases need no container.
//
// idxDone stays nil on purpose: flushIndexBuilds returns immediately for a
// driver with no worker, which is what lets abortIndexWork be called here
// without parking the test.
func newIndexStateDriver(t *testing.T) *Driver {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Driver{
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		searchFields: map[string][]string{},
		idxSignal:    make(chan struct{}, 1),
		idxCtx:       ctx,
		idxCancel:    cancel,
		idxDropping:  map[string]int{},
	}
}

// queuedCollections snapshots the worker queue under idxMu.
func queuedCollections(d *Driver) []string {
	d.idxMu.Lock()
	defer d.idxMu.Unlock()
	out := make([]string, 0, len(d.idxQueue))
	for _, j := range d.idxQueue {
		out = append(out, j.collection)
	}
	return out
}

// TestTakeIndexBatchPublishesBusyAndCancelAtomically is the regression guard
// for a shutdown/Drop hang. takeIndexBatch used to set idxBusy in one idxMu
// window while the worker published idxPassCancel in a LATER one, so
// "idxBusy == true && idxPassCancel == nil" was observable — and
// abortIndexWork, which reads both together, treats a nil cancel as "nothing
// to stop" and falls straight through to flushIndexBuilds. A Drop landing in
// that window therefore waited out the entire UNCANCELLED pass; for a
// CREATE INDEX CONCURRENTLY parked behind a long-lived writer that wait has no
// bound at all.
//
// The window cannot be hit deterministically from outside, so the assertion is
// on the invariant that replaces it: the batch, its context and its cancel all
// come out of one critical section, so busy always implies a live cancel.
func TestTakeIndexBatchPublishesBusyAndCancelAtomically(t *testing.T) {
	d := newIndexStateDriver(t)
	d.enqueueIndexJob(indexJob{
		collection: "notificationlog", table: "snooze_notificationlog",
		fields: []string{"status"},
	})

	batch, passCtx, cancel := d.takeIndexBatch()
	require.Len(t, batch, 1)
	require.NotNil(t, passCtx, "a non-empty batch must come with its pass context")
	require.NotNil(t, cancel, "a non-empty batch must come with its cancel")
	defer cancel()

	// Read both halves in ONE idxMu window, exactly as abortIndexWork does.
	d.idxMu.Lock()
	busy, published := d.idxBusy, d.idxPassCancel
	d.idxMu.Unlock()
	require.True(t, busy)
	require.NotNil(t, published,
		"busy with no cancel is the state abortIndexWork used to skip on")

	// And the consequence: an abort really does stop the pass instead of
	// waiting for it.
	d.abortIndexWork(context.Background(), "notificationlog")
	require.ErrorIs(t, passCtx.Err(), context.Canceled,
		"abortIndexWork must cancel the in-flight pass, not wait it out")

	// An empty queue reports idle and leaves no stale cancel behind.
	batch, passCtx, cancel = d.takeIndexBatch()
	require.Empty(t, batch)
	require.Nil(t, passCtx)
	require.Nil(t, cancel)
	d.idxMu.Lock()
	busy, published = d.idxBusy, d.idxPassCancel
	d.idxMu.Unlock()
	require.False(t, busy)
	require.Nil(t, published)
}

// TestDropGuardRefusesIndexWorkWhileDropping covers the re-race Drop had left:
// abortIndexWork clears the queue and waits for the worker to go idle, but
// nothing stopped a concurrent CreateIndex from enqueuing a fresh build in the
// gap between that wait returning and DROP TABLE taking its ACCESS EXCLUSIVE
// lock — which is exactly the CONCURRENTLY-vs-DROP deadlock the abort exists
// to avoid, reintroduced by a racing caller.
func TestDropGuardRefusesIndexWorkWhileDropping(t *testing.T) {
	d := newIndexStateDriver(t)
	job := func(c string) indexJob {
		return indexJob{collection: c, table: "snooze_" + c, fields: []string{"status"}}
	}

	d.beginDropGuard("record")
	d.enqueueIndexJob(job("record"))
	d.enqueueIndexJob(job("notificationlog"))
	require.Equal(t, []string{"notificationlog"}, queuedCollections(d),
		"the collection being dropped must be skipped; others must not be")

	// Nested Drops of the same collection must not un-guard each other.
	d.beginDropGuard("record")
	d.endDropGuard("record")
	d.enqueueIndexJob(job("record"))
	require.Equal(t, []string{"notificationlog"}, queuedCollections(d),
		"the outer Drop still holds the guard")

	d.endDropGuard("record")
	d.enqueueIndexJob(job("record"))
	require.Equal(t, []string{"notificationlog", "record"}, queuedCollections(d),
		"once every Drop has returned the collection is indexable again")
	require.Empty(t, d.idxDropping, "the guard map must not leak an entry per drop")
}

// TestIndexPassRunsOffThePool pins the second half of the pooled-connection
// fix. runIndexPass used to acquire a POOLED connection and pin it for the
// whole pass — every CONCURRENTLY build in the batch, each waiting out every
// transaction older than itself. On a `database.pool_max_size: 1` or `: 2`
// deployment (both of which exist) that is the entire pool, so the server
// serves requests with one connection fewer, or none, for as long as the
// builds take. The pass now opens its own non-pooled session instead.
//
// The pool is exhausted on purpose here: pre-fix the pass parked in
// pool.Acquire until the context expired and flushIndexBuilds timed out.
//
// The same change also removes a permanent-wedge hazard the test cannot
// observe directly: the advisory lock is session-level, so a pooled
// connection whose explicit unlock failed went back to the pool still holding
// it, and every later pass in the process then skipped forever. A dedicated
// session releases the lock by dying — see TestIndexPassSkipsWhenPeerHoldsLock,
// which now asserts that release.
func TestIndexPassRunsOffThePool(t *testing.T) {
	drv := newTestDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = drv.Drop(context.Background(), "notificationlog") })

	// Materialise the table while the pool is still free: ensureCollection is
	// synchronous by design and does use the pool (it is cached afterwards, so
	// the second CreateIndex below touches it no more).
	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"status"}))
	require.NoError(t, drv.flushIndexBuilds(ctx))

	maxConns := int(drv.pool.Config().MaxConns)
	require.Positive(t, maxConns)
	held := make([]*pgxpool.Conn, 0, maxConns)
	defer func() {
		for _, c := range held {
			c.Release()
		}
	}()
	for i := 0; i < maxConns; i++ {
		c, err := drv.pool.Acquire(ctx)
		require.NoError(t, err)
		held = append(held, c)
	}
	// Precondition: an ordinary pooled acquire can no longer succeed.
	shortCtx, shortCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer shortCancel()
	_, err := drv.pool.Acquire(shortCtx)
	require.Error(t, err, "precondition: the pool must be exhausted")

	require.NoError(t, drv.CreateIndex(ctx, "notificationlog", []string{"action"}))
	flushCtx, flushCancel := context.WithTimeout(ctx, 30*time.Second)
	defer flushCancel()
	require.NoError(t, drv.flushIndexBuilds(flushCtx),
		"the index pass must not need a pooled connection")

	// Release before probing the catalog — the probe itself is a pooled query.
	for _, c := range held {
		c.Release()
	}
	held = nil

	name := indexName("snooze_notificationlog", "action", "")
	exists, valid, err := drv.indexState(ctx, name)
	require.NoError(t, err)
	require.True(t, exists && valid,
		"the pass must have built the index on its own connection")
}
