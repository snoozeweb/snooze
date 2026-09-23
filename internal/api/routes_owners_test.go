package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ownersHarness mounts /record/owners beside the record plugin's generic CRUD,
// in router.go's order, over a SQLite store seeded with owned, unowned and
// ghost-only records.
func ownersHarness(t *testing.T) chi.Router {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	d, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	_, err = d.Write(ctx, recordCollection, []db.Document{
		{"host": "h1", "state": "ack", "owner": "bob"},
		{"host": "h2", "state": "ack", "owner": "bob"},
		{"host": "h3", "state": "close", "owner": "alice"},
		{"host": "h4", "state": "ack", "owner": "alice"},
		{"host": "h5", "state": "ack", "owner": "carol"},
		{"host": "h6", "state": "open", "owner": ""},
		{"host": "h7", "state": "open"},
		// A ghost-only alert is unowned.
		{"host": "h8", "state": "esc", "owner": "", "previous_owner": "bob"},
	}, db.WriteOptions{})
	require.NoError(t, err)
	// Another tenant's rows never leak into the counts.
	_, err = d.Write(snoozetypes.WithTenant(context.Background(), "acme"), recordCollection,
		[]db.Document{{"host": "x", "state": "ack", "owner": "mallory"}}, db.WriteOptions{})
	require.NoError(t, err)

	plugs := map[string]plugins.Plugin{recordCollection: &bulkRecordPlugin{name: recordCollection}}
	host := &bulkTestHost{driver: d, plugs: plugs}
	rt := &Router{DB: d, Host: host, Plugins: plugs}
	r := chi.NewRouter()
	rt.mountOwners(r)
	for _, p := range plugs {
		plugins.MountCRUD(r, host, p)
	}
	return r
}

func getOwners(t *testing.T, r chi.Router, target string, perms ...string) (*httptest.ResponseRecorder, ownersResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodGet, target, nil, perms...))
	var out ownersResponse
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	}
	return rec, out
}

func TestRecordOwners_CountsAllRecords(t *testing.T) {
	t.Parallel()
	r := ownersHarness(t)
	rec, out := getOwners(t, r, "/api/v1/record/owners", "ro_record")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	// Count descending, then owner ascending; the unowned bucket is separate.
	require.Equal(t, []ownerCount{{"alice", 2}, {"bob", 2}, {"carol", 1}}, out.Data)
	require.Equal(t, 3, out.Unowned, "absent, empty and ghost-only records are unowned")
	require.Equal(t, 8, out.Total)
}

func TestRecordOwners_HonoursQuery(t *testing.T) {
	t.Parallel()
	r := ownersHarness(t)
	q := encodeQ(t, condition.Equals("state", "ack"))
	rec, out := getOwners(t, r, "/api/v1/record/owners?q="+q, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []ownerCount{{"bob", 2}, {"alice", 1}, {"carol", 1}}, out.Data)
	require.Equal(t, 0, out.Unowned)
	require.Equal(t, 4, out.Total)

	// Nothing matches: an empty array, never null.
	q = encodeQ(t, condition.Equals("state", "shelved"))
	rec, _ = getOwners(t, r, "/api/v1/record/owners?q="+q, "ro_record")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"data":[],"unowned":0,"total":0}`, rec.Body.String())
}

func TestRecordOwners_Permissions(t *testing.T) {
	t.Parallel()
	r := ownersHarness(t)
	rec, _ := getOwners(t, r, "/api/v1/record/owners", "ro_rule")
	require.Equal(t, http.StatusForbidden, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/record/owners", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRecordOwners_BadQuery(t *testing.T) {
	t.Parallel()
	r := ownersHarness(t)
	rec, _ := getOwners(t, r, "/api/v1/record/owners?q=not*base64", "ro_record")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
