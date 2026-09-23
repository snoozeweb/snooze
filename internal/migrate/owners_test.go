package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ownersFixture seeds one tenant's records, comments and users and returns the
// record uids by label.
func ownersFixture(t *testing.T, drv *sqlite.Driver, tenant string) map[string]string {
	t.Helper()
	tctx := auth.WithTenant(context.Background(), tenant)

	_, err := drv.Write(tctx, "user", []db.Document{
		{"name": "alice", "method": "ldap"},
		{"name": "bob", "method": "local"},
		{"name": "bob", "method": "oidc"}, // ambiguous login
		{"name": "carol", "method": "local"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	labels := []string{"acked", "acked-nocomment", "escalated", "closed", "closed-auto", "open", "already"}
	docs := []db.Document{
		{"state": "ack", "acked_by": "alice", "date_epoch": int64(100)},
		{"state": "ack", "acked_by": "bob", "date_epoch": int64(200)},
		{"state": "esc", "acked_by": "alice", "date_epoch": int64(300)},
		{"state": "close", "date_epoch": int64(400)},
		{"state": "close", "date_epoch": int64(500)},
		{"state": "open", "acked_by": "alice", "date_epoch": int64(600)},
		{"state": "ack", "acked_by": "alice", "owner": "carol", "owner_method": "local", "date_epoch": int64(700)},
	}
	res, err := drv.Write(tctx, "record", docs, db.WriteOptions{})
	require.NoError(t, err)
	uids := map[string]string{}
	for i, l := range labels {
		uids[l] = res.Added[i]
	}

	_, err = drv.Write(tctx, "comment", []db.Document{
		// Two human acks: the latest wins for owner_since.
		{"record_uid": uids["acked"], "type": "ack", "user": "alice", "date_epoch": int64(110)},
		{"record_uid": uids["acked"], "type": "ack", "user": "alice", "date_epoch": int64(150)},
		// An older close by alice, then the latest by carol, then an auto one.
		{"record_uid": uids["closed"], "type": "close", "user": "alice", "date_epoch": int64(410)},
		{"record_uid": uids["closed"], "type": "close", "user": "carol", "date_epoch": int64(420)},
		{"record_uid": uids["closed"], "type": "close", "auto": true, "date_epoch": int64(430)},
		// Only a system close: nobody to credit.
		{"record_uid": uids["closed-auto"], "type": "close", "auto": true, "date_epoch": int64(510)},
	}, db.WriteOptions{})
	require.NoError(t, err)
	return uids
}

func seedTenants(t *testing.T, drv *sqlite.Driver, ids ...string) {
	t.Helper()
	docs := make([]db.Document, len(ids))
	for i, id := range ids {
		docs[i] = db.Document{"id": id, "status": "active"}
	}
	_, err := drv.Write(auth.WithPlatformScope(context.Background()), auth.TenantCollection, docs, db.WriteOptions{})
	require.NoError(t, err)
}

func recordIn(t *testing.T, drv *sqlite.Driver, tenant, uid string) db.Document {
	t.Helper()
	doc, err := drv.GetOne(auth.WithTenant(context.Background(), tenant), "record", db.Document{"uid": uid})
	require.NoError(t, err)
	return doc
}

func TestOwnersMigration_Backfills(t *testing.T) {
	drv := newSQLiteDriver(t)
	seedTenants(t, drv, snoozetypes.DefaultTenant)
	uids := ownersFixture(t, drv, snoozetypes.DefaultTenant)

	require.NoError(t, RunOwnersMigration(context.Background(), drv))
	get := func(label string) db.Document { return recordIn(t, drv, snoozetypes.DefaultTenant, uids[label]) }

	acked := get("acked")
	require.Equal(t, "alice", acked["owner"])
	require.Equal(t, "ldap", acked["owner_method"], "method of the matching user")
	require.EqualValues(t, 150, acked["owner_since"], "latest human ack comment")
	require.Equal(t, "", acked["previous_owner"])

	noComment := get("acked-nocomment")
	require.Equal(t, "bob", noComment["owner"])
	require.Equal(t, "", noComment["owner_method"], "an ambiguous login gets no method")
	require.EqualValues(t, 200, noComment["owner_since"], "falls back to date_epoch")

	esc := get("escalated")
	require.Equal(t, "", esc["owner"], "an escalated alert is unowned")
	require.Equal(t, "alice", esc["previous_owner"])
	require.Equal(t, "ldap", esc["previous_owner_method"])

	closed := get("closed")
	require.Equal(t, "carol", closed["owner"], "the latest human close")
	require.Equal(t, "local", closed["owner_method"])
	require.EqualValues(t, 420, closed["owner_since"])

	for _, label := range []string{"closed-auto", "open"} {
		_, has := get(label)["owner"]
		require.False(t, has, "%s: nothing to backfill", label)
	}

	already := get("already")
	require.Equal(t, "carol", already["owner"], "a record that already has an owner key is left alone")
	_, has := already["owner_since"]
	require.False(t, has)
}

// Running it again changes nothing, and a record that has been handled since
// the first run is not rewritten from stale acked_by data.
func TestOwnersMigration_Idempotent(t *testing.T) {
	drv := newSQLiteDriver(t)
	seedTenants(t, drv, snoozetypes.DefaultTenant)
	uids := ownersFixture(t, drv, snoozetypes.DefaultTenant)
	require.NoError(t, RunOwnersMigration(context.Background(), drv))

	before := map[string]db.Document{}
	for l, uid := range uids {
		before[l] = recordIn(t, drv, snoozetypes.DefaultTenant, uid)
	}
	require.NoError(t, RunOwnersMigration(context.Background(), drv))
	for l, uid := range uids {
		after := recordIn(t, drv, snoozetypes.DefaultTenant, uid)
		for _, k := range []string{"owner", "owner_method", "owner_since", "previous_owner", "previous_owner_method"} {
			require.Equal(t, before[l][k], after[k], "%s.%s", l, k)
		}
	}
}

// Each tenant is migrated under its own scope: users and comments resolve
// within the tenant only.
func TestOwnersMigration_PerTenant(t *testing.T) {
	drv := newSQLiteDriver(t)
	seedTenants(t, drv, snoozetypes.DefaultTenant, "acme")
	tctx := auth.WithTenant(context.Background(), "acme")

	// alice exists only in the default tenant; acme's alice is oidc.
	_, err := drv.Write(auth.WithTenant(context.Background(), snoozetypes.DefaultTenant), "user",
		[]db.Document{{"name": "alice", "method": "ldap"}}, db.WriteOptions{})
	require.NoError(t, err)
	_, err = drv.Write(tctx, "user", []db.Document{{"name": "alice", "method": "oidc"}}, db.WriteOptions{})
	require.NoError(t, err)
	res, err := drv.Write(tctx, "record",
		[]db.Document{{"state": "ack", "acked_by": "alice", "date_epoch": int64(9)}}, db.WriteOptions{})
	require.NoError(t, err)

	require.NoError(t, RunOwnersMigration(context.Background(), drv))
	doc := recordIn(t, drv, "acme", res.Added[0])
	require.Equal(t, "alice", doc["owner"])
	require.Equal(t, "oidc", doc["owner_method"], "resolved against acme's directory")
}

func TestOwnersMigration_NilDriver(t *testing.T) {
	require.Error(t, RunOwnersMigration(context.Background(), nil))
}
