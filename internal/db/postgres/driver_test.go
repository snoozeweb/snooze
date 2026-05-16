package postgres

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// newTestDriver spins up a postgres container and returns a connected
// Driver. The container is cleaned up via t.Cleanup. Tests that hit this
// helper must run with `go test` (no -short).
func newTestDriver(t *testing.T) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("driver tests require a postgres container")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("snooze"),
		tcpostgres.WithUsername("snooze"),
		tcpostgres.WithPassword("snooze"),
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Skipf("postgres container unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	// Belt-and-braces: wait for the listening port too.
	if err := wait.ForListeningPort("5432/tcp").
		WaitUntilReady(ctx, container); err != nil {
		t.Fatalf("waiting for postgres: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	drv, err := New(ctx, Config{DSN: dsn, ApplicationName: "snooze-test"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return drv
}

// TestDriver_BasicCRUD exercises the round-trip Write→Search→GetOne→Delete
// path with a couple of edge cases. This is the placeholder until
// dbtest.RunDriverSuite (Phase 1A) lands; once it does the body can shrink
// to dbtest.RunDriverSuite(t, "postgres", ...).
func TestDriver_BasicCRUD(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	wr, err := drv.Write(ctx, "record", []dbpkg.Document{
		{"host": "h1", "severity": "err", "count": 1},
		{"host": "h2", "severity": "warn", "count": 2},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, wr.Added, 2)
	require.Empty(t, wr.Rejected)

	docs, total, err := drv.Search(ctx, "record", condition.Cond{}, dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, docs, 2)

	one, err := drv.GetOne(ctx, "record", dbpkg.Document{"host": "h1"})
	require.NoError(t, err)
	require.Equal(t, "h1", one["host"])

	n, err := drv.Delete(ctx, "record", condition.Equals("host", "h2"), false)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	docs, total, err = drv.Search(ctx, "record", condition.Cond{}, dbpkg.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, docs, 1)
}

func TestDriver_UpdateAndReplace(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	wr, err := drv.Write(ctx, "rule", []dbpkg.Document{
		{"name": "rule-1", "value": 1},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, wr.Added, 1)
	uid := wr.Added[0]

	require.NoError(t, drv.UpdateOne(ctx, "rule", uid, dbpkg.Document{"value": 42}, true))
	got, err := drv.GetOne(ctx, "rule", dbpkg.Document{"uid": uid})
	require.NoError(t, err)
	require.Equal(t, float64(42), got["value"])
	require.Equal(t, "rule-1", got["name"]) // merged, not replaced

	n, err := drv.ReplaceOne(ctx, "rule", dbpkg.Document{"uid": uid},
		dbpkg.Document{"name": "rule-2"}, true)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	got, err = drv.GetOne(ctx, "rule", dbpkg.Document{"uid": uid})
	require.NoError(t, err)
	require.Equal(t, "rule-2", got["name"])
	require.Nil(t, got["value"]) // replaced overwrites
}

func TestDriver_BulkIncrement(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	_, err := drv.Write(ctx, "stats", []dbpkg.Document{
		{"key": "k1", "value": 0},
	}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	require.NoError(t, drv.BulkIncrement(ctx, "stats", []dbpkg.IncrementOp{
		{Search: dbpkg.Document{"key": "k1"}, Deltas: map[string]int64{"value": 5}},
		{Search: dbpkg.Document{"key": "k2"}, Deltas: map[string]int64{"value": 3}},
	}, true))

	got, err := drv.GetOne(ctx, "stats", dbpkg.Document{"key": "k1"})
	require.NoError(t, err)
	require.EqualValues(t, 5, toInt64(got["value"]))

	got, err = drv.GetOne(ctx, "stats", dbpkg.Document{"key": "k2"})
	require.NoError(t, err)
	require.EqualValues(t, 3, toInt64(got["value"]))
}

func TestDriver_ListCollections(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()
	_, err := drv.Write(ctx, "a", []dbpkg.Document{{"x": 1}}, dbpkg.WriteOptions{})
	require.NoError(t, err)
	_, err = drv.Write(ctx, "b.c", []dbpkg.Document{{"x": 2}}, dbpkg.WriteOptions{})
	require.NoError(t, err)

	cols, err := drv.ListCollections(ctx)
	require.NoError(t, err)
	sort.Strings(cols)
	require.Equal(t, []string{"a", "b.c"}, cols)

	require.NoError(t, drv.Drop(ctx, "b.c"))
	cols, err = drv.ListCollections(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, cols)
}

func TestSanitizeCollection(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"plain", "record", "snooze_record", false},
		{"dotted", "a.b.c", "snooze_a__b__c", false},
		{"leading_digit", "1bad", "", true},
		{"empty", "", "", true},
		{"underscore", "_ok", "snooze__ok", false},
		{"forbidden", "drop;table", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sanitizeCollection(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestCollectionFromTable(t *testing.T) {
	require.Equal(t, "record", collectionFromTable("snooze_record"))
	require.Equal(t, "a.b", collectionFromTable("snooze_a__b"))
	require.Equal(t, "", collectionFromTable("other_table"))
}
