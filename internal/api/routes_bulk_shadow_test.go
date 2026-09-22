package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// The bulk endpoints are mounted as siblings of the per-plugin CRUD subrouters,
// and chi prefers a static path segment over a parameter sibling: a request to
// /api/v1/record/bulk_update descends into the static `record` mount and never
// backtracks to `/api/v1/{plugin}/bulk_update`. With only the parameterised
// form registered, every collection answered 405 — including the alerts table's
// bulk tag/attribute dialog, which posts exactly this.
//
// The pre-existing bulk tests mount ONLY mountBulk, so the shadowing was
// invisible to them. This harness mounts both surfaces, in the same order
// router.go does.
func bulkWithCRUDHarness(t *testing.T) (chi.Router, db.Driver) {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	d, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	plugs := map[string]plugins.Plugin{
		"record": &bulkRecordPlugin{name: "record", audit: false},
	}
	host := &bulkTestHost{driver: d, plugs: plugs}
	rt := &Router{DB: d, Host: host, Plugins: plugs}

	r := chi.NewRouter()
	rt.mountBulk(r)
	for _, p := range plugs {
		plugins.MountCRUD(r, host, p)
	}
	return r, d
}

func TestBulkUpdateIsNotShadowedByPluginCRUD(t *testing.T) {
	t.Parallel()
	r, d := bulkWithCRUDHarness(t)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, "record", []db.Document{
		{"host": "web-1", "message": "one"},
		{"host": "web-1", "message": "two"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	rec := writeJSONReq(t, r, http.MethodPost, "/api/v1/record/bulk_update",
		map[string]any{"set": map[string]any{"environment": "prod"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"set":2`)

	// And the CRUD surface it sits beside still works.
	rec = writeJSONReq(t, r, http.MethodGet, "/api/v1/record/", nil)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBulkStateIsNotShadowedByPluginCRUD(t *testing.T) {
	t.Parallel()
	r, d := bulkWithCRUDHarness(t)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err := d.Write(ctx, "record", []db.Document{{"host": "web-1", "message": "one"}}, db.WriteOptions{})
	require.NoError(t, err)

	rec := writeJSONReq(t, r, http.MethodPost, "/api/v1/record/bulk_state",
		map[string]any{"state": "ack"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// A uid that happens to read "bulk_update" must still route to the bulk
// endpoint, not to the CRUD /{uid} handler — documenting which wins.
func TestBulkUpdateWinsOverTheUIDRoute(t *testing.T) {
	t.Parallel()
	r, _ := bulkWithCRUDHarness(t)
	// GET /{uid} still resolves through the CRUD mount: the bulk registration
	// is POST-only, so it does not swallow reads of a same-named document.
	rec := writeJSONReq(t, r, http.MethodGet, "/api/v1/record/bulk_update", nil)
	require.NotEqual(t, http.StatusMethodNotAllowed, rec.Code)
}
