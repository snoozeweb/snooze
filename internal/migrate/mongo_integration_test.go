// internal/migrate/mongo_integration_test.go
//
// Real-MongoDB coverage for the multitenancy migration. The unit suite
// (migrate_test.go) runs against a fake driver and sqlite_integration_test.go
// against SQLite; this file exercises the in-place tenant_id backfill against a
// live mongod, which is what the production deployment actually runs. The
// backfill must never duplicate rows, so the assertions check row counts
// explicitly rather than just the stamped field — including the uid-less case
// (stats counters, Python-era users) that caused a production incident.
package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/mongo"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// mongoTestDB is the database name used by the migration integration tests.
const mongoTestDB = "snoozetest"

// newMongoDriver spins up a single-node replica-set MongoDB via testcontainers
// and returns a connected driver plus the connection URI (so a raw client can
// insert documents the driver's own Write would never produce, e.g. uid-less
// rows). Mirrors internal/db/mongo's startMongo helper (unexported in that
// package's _test.go, so duplicated here).
func newMongoDriver(t *testing.T) (*mongo.Driver, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: skipping under -short")
	}
	ctx := context.Background()
	container, err := tcmongo.Run(ctx, "mongo:7", tcmongo.WithReplicaSet("rs0"))
	if err != nil {
		t.Skipf("testcontainers mongo unavailable: %v", err)
	}
	uri, err := container.ConnectionString(ctx)
	require.NoError(t, err)
	drv, err := mongo.New(ctx, mongo.Config{
		URI:                    uri,
		Database:               mongoTestDB,
		ServerSelectionTimeout: 15 * time.Second,
	})
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = drv.Close()
		_ = testcontainers.TerminateContainer(container)
	})
	return drv, uri
}

func TestRunMultitenancyMigration_Mongo_FullRun(t *testing.T) {
	drv, _ := newMongoDriver(t)
	ctx := context.Background()
	pctx := auth.WithPlatformScope(ctx)

	// Seed pre-migration data (no tenant_id on any doc), mirroring a real
	// pre-multitenancy database.
	_, err := drv.Write(pctx, "record",
		[]db.Document{{"host": "h1", "message": "pre-migration"}},
		db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	_, err = drv.Write(pctx, "user",
		[]db.Document{{"name": auth.RootUsername, "method": auth.LocalMethod, "roles": []any{"admin"}}},
		db.WriteOptions{Primary: []string{"name", "method"}, UpdateTime: false})
	require.NoError(t, err)

	_, err = drv.Write(pctx, "role",
		[]db.Document{{"name": "admin", "permissions": []any{"rw_all"}}},
		db.WriteOptions{Primary: []string{"name"}, UpdateTime: false})
	require.NoError(t, err)

	// Run migration.
	require.NoError(t, RunMultitenancyMigration(ctx, drv))

	// Records gain tenant_id = default (and are not duplicated).
	records, _, err := drv.Search(pctx, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, snoozetypes.DefaultTenant, records[0]["tenant_id"])

	// The tenant registry doc exists.
	tenants, _, err := drv.Search(pctx, "tenant", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, tenants, 1)
	require.Equal(t, snoozetypes.DefaultTenant, tenants[0]["id"])

	// The user gains tenant_id and the platform_admin role — and the PK
	// rewrite must NOT duplicate the row (the Mongo-specific risk).
	users, _, err := drv.Search(pctx, "user", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, users, 1, "user PK rewrite must update in place, not duplicate")
	require.Equal(t, snoozetypes.DefaultTenant, users[0]["tenant_id"])
	roleSet := make(map[string]struct{})
	if rs, ok := users[0]["roles"].([]any); ok {
		for _, r := range rs {
			if s, ok := r.(string); ok {
				roleSet[s] = struct{}{}
			}
		}
	}
	require.Contains(t, roleSet, auth.PlatformAdminRole)

	// The pre-existing "admin" role gains tenant_id without duplicating; the
	// platform_admin role is seeded alongside it.
	roles, _, err := drv.Search(pctx, "role", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	roleNames := make(map[string]int)
	for _, r := range roles {
		if n, ok := r["name"].(string); ok {
			roleNames[n]++
			if n == "admin" {
				require.Equal(t, snoozetypes.DefaultTenant, r["tenant_id"],
					"pre-existing admin role must be stamped with the default tenant")
			}
		}
	}
	require.Equal(t, 1, roleNames["admin"], "admin role PK rewrite must not duplicate")
	require.Equal(t, 1, roleNames[auth.PlatformAdminRole], "platform_admin role must be seeded exactly once")

	// Second run is idempotent: no duplicate tenant / user / role rows.
	require.NoError(t, RunMultitenancyMigration(ctx, drv))
	tenants2, _, err := drv.Search(pctx, "tenant", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, tenants2, 1, "idempotent: must not duplicate tenant doc")
	users2, _, err := drv.Search(pctx, "user", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, users2, 1, "idempotent: must not duplicate user doc")
}

func TestRunMultitenancyMigration_Mongo_EmptyDB(t *testing.T) {
	drv, _ := newMongoDriver(t)
	require.NoError(t, RunMultitenancyMigration(context.Background(), drv))

	pctx := auth.WithPlatformScope(context.Background())
	done, err := isAlreadyMigrated(pctx, drv)
	require.NoError(t, err)
	require.True(t, done)
}

// TestBackfillTenantID_Mongo_NoUidDocsNotDuplicated is the regression test for
// the production incident: documents WITHOUT a uid field (stats counters,
// Python-era user rows) were DUPLICATED by the backfill on every run. The old
// implementation read each doc and re-wrote it upserting on ["uid"]; a uid-less
// doc gets a fresh uid on write, so the upsert filter never matched an existing
// row and a copy was inserted instead. On the live host stats went 286k → 860k
// across runs. The backfill must instead stamp tenant_id in place.
func TestBackfillTenantID_Mongo_NoUidDocsNotDuplicated(t *testing.T) {
	drv, uri := newMongoDriver(t)
	ctx := context.Background()
	pctx := auth.WithPlatformScope(ctx)

	// Insert uid-less documents via a raw client — the driver's own Write always
	// assigns a uid, so it cannot reproduce the production shape.
	cli, err := mongodriver.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Disconnect(ctx) })
	mdb := cli.Database(mongoTestDB)

	var statsDocs []any
	for i := 0; i < 5; i++ {
		statsDocs = append(statsDocs, bson.M{
			"metric": "alert_hit", "dim": "severity", "key": "warning",
			"bucket": int64(1000 + i), "value": int64(i),
		})
	}
	_, err = mdb.Collection("stats").InsertMany(ctx, statsDocs)
	require.NoError(t, err)
	_, err = mdb.Collection("user").InsertOne(ctx, bson.M{"name": "svc-bot", "method": "local"})
	require.NoError(t, err)

	// Backfill twice: the first run stamps, the second must be a pure no-op.
	// The old uid-upsert inserted a fresh copy of every uid-less doc on each
	// call; the in-place path must leave the counts unchanged.
	_, err = backfillTenantID(pctx, drv, snoozetypes.DefaultTenant)
	require.NoError(t, err)
	_, err = backfillTenantID(pctx, drv, snoozetypes.DefaultTenant)
	require.NoError(t, err)

	statsTotal, err := mdb.Collection("stats").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	require.Equal(t, int64(5), statsTotal, "uid-less stats must not be duplicated by the backfill")
	statsStamped, err := mdb.Collection("stats").CountDocuments(ctx, bson.M{"tenant_id": snoozetypes.DefaultTenant})
	require.NoError(t, err)
	require.Equal(t, int64(5), statsStamped, "every stats doc must be stamped tenant_id=default")

	botCount, err := mdb.Collection("user").CountDocuments(ctx, bson.M{"name": "svc-bot"})
	require.NoError(t, err)
	require.Equal(t, int64(1), botCount, "uid-less user must not be duplicated")
	botStamped, err := mdb.Collection("user").CountDocuments(ctx, bson.M{"name": "svc-bot", "tenant_id": snoozetypes.DefaultTenant})
	require.NoError(t, err)
	require.Equal(t, int64(1), botStamped, "uid-less user must be stamped tenant_id=default")
}
