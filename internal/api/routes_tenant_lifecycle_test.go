package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// lifecycleDB is a tenant- and collection-aware in-memory driver used by the
// H5 lifecycle tests. Unlike the trivial tenantDB stub, it faithfully models
// the driver's tenant_id enforcement: writes to scoped collections stamp
// tenant_id from the ctx, reads/deletes fence by it, and platform scope (or a
// global collection) bypasses the fence. This lets the tests prove genuine
// cross-tenant isolation rather than rubber-stamping the handler.
type lifecycleDB struct {
	mu          sync.Mutex
	collections map[string][]db.Document
}

func newLifecycleDB() *lifecycleDB {
	return &lifecycleDB{collections: map[string][]db.Document{}}
}

// seedScoped inserts a doc into a scoped collection already stamped with
// tenant_id, simulating prior tenant data.
func (f *lifecycleDB) seedScoped(collection, tenantID string, doc db.Document) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make(db.Document, len(doc)+2)
	for k, v := range doc {
		cp[k] = v
	}
	cp["tenant_id"] = tenantID
	if _, ok := cp["uid"]; !ok {
		cp["uid"] = uuid.NewString()
	}
	f.collections[collection] = append(f.collections[collection], cp)
}

func (f *lifecycleDB) count(collection, tenantID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.collections[collection] {
		if d["tenant_id"] == tenantID {
			n++
		}
	}
	return n
}

// scope mirrors db.TenantScope for the fake: returns (tenantID, inject, err).
func scope(ctx context.Context, collection string) (string, bool, error) {
	if db.IsGlobalCollection(collection) || snoozetypes.IsPlatformScope(ctx) {
		return "", false, nil
	}
	t, ok := snoozetypes.TenantFrom(ctx)
	if !ok || t == "" {
		return "", false, snoozetypes.ErrNoTenant
	}
	return t, true, nil
}

func (f *lifecycleDB) Search(ctx context.Context, collection string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	tenantID, inject, err := scope(ctx, collection)
	if err != nil {
		return nil, 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []db.Document
	for _, d := range f.collections[collection] {
		if inject && d["tenant_id"] != tenantID {
			continue
		}
		out = append(out, d)
	}
	return out, len(out), nil
}

func (f *lifecycleDB) GetOne(ctx context.Context, collection string, match db.Document) (db.Document, error) {
	tenantID, inject, err := scope(ctx, collection)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.collections[collection] {
		if inject && d["tenant_id"] != tenantID {
			continue
		}
		ok := true
		for k, want := range match {
			if d[k] != want {
				ok = false
				break
			}
		}
		if ok {
			cp := make(db.Document, len(d))
			for k, v := range d {
				cp[k] = v
			}
			return cp, nil
		}
	}
	return nil, nil
}

func (f *lifecycleDB) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, nil
}

func (f *lifecycleDB) Write(ctx context.Context, collection string, docs []db.Document, opts db.WriteOptions) (db.WriteResult, error) {
	tenantID, inject, err := scope(ctx, collection)
	if err != nil {
		return db.WriteResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var res db.WriteResult
	for _, doc := range docs {
		cp := make(db.Document, len(doc)+2)
		for k, v := range doc {
			cp[k] = v
		}
		if inject {
			cp["tenant_id"] = tenantID
		}
		var match db.Document
		if len(opts.Primary) > 0 {
			match = make(db.Document, len(opts.Primary)+1)
			for _, k := range opts.Primary {
				match[k] = cp[k]
			}
			if inject {
				match["tenant_id"] = tenantID
			}
		}
		idx := -1
		if match != nil {
			for i, existing := range f.collections[collection] {
				eq := true
				for k, want := range match {
					if existing[k] != want {
						eq = false
						break
					}
				}
				if eq {
					idx = i
					break
				}
			}
		}
		if idx >= 0 {
			if opts.DuplicatePolicy == "reject" {
				res.Rejected = append(res.Rejected, db.Rejection{Reason: "duplicate", Payload: doc})
				continue
			}
			for k, v := range cp {
				f.collections[collection][idx][k] = v
			}
			res.Updated = append(res.Updated, f.collections[collection][idx]["uid"].(string))
		} else {
			if _, ok := cp["uid"]; !ok {
				cp["uid"] = uuid.NewString()
			}
			f.collections[collection] = append(f.collections[collection], cp)
			res.Added = append(res.Added, cp["uid"].(string))
		}
	}
	return res, nil
}

func (f *lifecycleDB) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, nil
}

func (f *lifecycleDB) UpdateOne(ctx context.Context, collection, uid string, patch db.Document, _ bool) error {
	tenantID, inject, err := scope(ctx, collection)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.collections[collection] {
		if d["uid"] != uid && d["id"] != uid {
			continue
		}
		if inject && d["tenant_id"] != tenantID {
			continue
		}
		for k, v := range patch {
			d[k] = v
		}
		return nil
	}
	return nil
}

func (f *lifecycleDB) Delete(ctx context.Context, collection string, cond condition.Cond, _ bool) (int, error) {
	tenantID, inject, err := scope(ctx, collection)
	if err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.collections[collection]
	kept := rows[:0:0]
	deleted := 0
	for _, d := range rows {
		// Tenant fence: a scoped delete may only touch its own tenant's rows.
		if inject && d["tenant_id"] != tenantID {
			kept = append(kept, d)
			continue
		}
		if condMatches(cond, d) {
			deleted++
			continue
		}
		kept = append(kept, d)
	}
	f.collections[collection] = kept
	return deleted, nil
}

// condMatches is a tiny evaluator covering the conditions the tenant handlers
// build: equality, conjunction, and always-true.
func condMatches(cond condition.Cond, d db.Document) bool {
	switch cond.Op {
	case condition.OpAlwaysTrue: // "" — always-true / zero cond
		return true
	case condition.OpEq:
		return d[cond.Field] == cond.Value
	case condition.OpAnd:
		for _, c := range cond.Children {
			if !condMatches(c, d) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func (f *lifecycleDB) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error {
	return nil
}
func (f *lifecycleDB) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, nil
}
func (f *lifecycleDB) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, nil
}
func (f *lifecycleDB) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
	return 0, nil
}
func (f *lifecycleDB) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (f *lifecycleDB) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (f *lifecycleDB) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (f *lifecycleDB) CreateIndex(context.Context, string, []string) error      { return nil }
func (f *lifecycleDB) ListCollections(context.Context) ([]string, error)        { return nil, nil }
func (f *lifecycleDB) Drop(context.Context, string) error                       { return nil }
func (f *lifecycleDB) Backup(context.Context, string, []string) error           { return nil }
func (f *lifecycleDB) CleanupTimeout(context.Context, string) (int, error)      { return 0, nil }
func (f *lifecycleDB) CleanupComments(context.Context) (int, error)             { return 0, nil }
func (f *lifecycleDB) CleanupOrphans(context.Context, string) (int, error)      { return 0, nil }
func (f *lifecycleDB) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (f *lifecycleDB) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (f *lifecycleDB) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (f *lifecycleDB) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (f *lifecycleDB) Watcher() syncer.Bus { return nil }
func (f *lifecycleDB) Close() error        { return nil }

// lifecycleRouter wires mountTenant with a platform-admin identity (default
// tenant + literal rw_tenant) so the platform gate admits the request.
func lifecycleRouter(t *testing.T, ldb *lifecycleDB) chi.Router {
	t.Helper()
	rt := &Router{Auth: testTokenEngine(t), DB: ldb}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithClaims(req.Context(), snoozetypes.Claims{
				Subject:     "root",
				Method:      "local",
				TenantID:    snoozetypes.DefaultTenant,
				Permissions: []string{auth.PermWriteTenant, auth.PermReadTenant},
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	rt.mountTenant(r)
	return r
}

func doJSON(t *testing.T, r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Buffer
	if body != "" {
		rdr = bytes.NewBufferString(body)
	} else {
		rdr = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestTenantCreate_SeedsRolesAndMarker reproduces the seeding half of H5: a
// runtime-created tenant must come up with the three default roles AND the
// init_db marker in its own scope. Before the fix the platform-scoped create
// handler did a bare registry write and seeded nothing, so the new tenant has
// zero roles → unusable. This test FAILS on the unpatched handler.
func TestTenantCreate_SeedsRolesAndMarker(t *testing.T) {
	ldb := newLifecycleDB()
	r := lifecycleRouter(t, ldb)

	rec := doJSON(t, r, http.MethodPost, "/api/v1/tenant", `{"id":"acme","display_name":"Acme"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Registry doc written to the global tenant collection.
	require.Equal(t, 1, len(ldb.collections[auth.TenantCollection]))

	// Default roles seeded under the NEW tenant's scope.
	require.Equal(t, 3, ldb.count(auth.RoleCollection, "acme"),
		"create must seed the three default roles in the new tenant's scope")
	seeded := map[string]bool{}
	for _, d := range ldb.collections[auth.RoleCollection] {
		if d["tenant_id"] == "acme" {
			seeded[d["name"].(string)] = true
		}
	}
	require.True(t, seeded["admin"], "admin role must be seeded")
	require.True(t, seeded["viewer"], "viewer role must be seeded")
	require.True(t, seeded["notifications"], "notifications role must be seeded")

	// init_db marker written under the new tenant's scope.
	require.GreaterOrEqual(t, ldb.count("general", "acme"), 1,
		"create must write the per-tenant init_db marker")
	var markerOK bool
	for _, d := range ldb.collections["general"] {
		if d["tenant_id"] == "acme" {
			if b, _ := d["init_db"].(bool); b {
				markerOK = true
			}
		}
	}
	require.True(t, markerOK, "init_db marker must be true under the new tenant")
}

// TestTenantCreate_Idempotent ensures a re-create (same slug) does not blow up
// and does not duplicate the seeded roles.
func TestTenantCreate_Idempotent(t *testing.T) {
	ldb := newLifecycleDB()
	r := lifecycleRouter(t, ldb)

	// First create succeeds.
	rec := doJSON(t, r, http.MethodPost, "/api/v1/tenant", `{"id":"acme","display_name":"Acme"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, 3, ldb.count(auth.RoleCollection, "acme"))

	// Second create with same slug is rejected (registry duplicate) but the
	// seed must not have multiplied.
	rec = doJSON(t, r, http.MethodPost, "/api/v1/tenant", `{"id":"acme","display_name":"Acme"}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Equal(t, 3, ldb.count(auth.RoleCollection, "acme"),
		"a rejected re-create must not double-seed roles")
}

// TestTenantDelete_PurgesAndIsolates reproduces the cascade half of H5: delete
// must purge the target tenant's data across every scoped collection AND must
// leave other tenants' data intact. Before the fix delete only removed the
// registry doc, orphaning record/rule/user/role/refresh_token rows that a
// same-slug re-create would silently inherit. This test FAILS on the unpatched
// handler (acme's scoped rows survive).
func TestTenantDelete_PurgesAndIsolates(t *testing.T) {
	ldb := newLifecycleDB()
	r := lifecycleRouter(t, ldb)

	// Two tenants with data across several scoped collections.
	ldb.collections[auth.TenantCollection] = []db.Document{
		{"id": "acme", "status": "active", "uid": "t-acme"},
		{"id": "beta", "status": "active", "uid": "t-beta"},
	}
	for _, ten := range []string{"acme", "beta"} {
		ldb.seedScoped("record", ten, db.Document{"host": "h1"})
		ldb.seedScoped("record", ten, db.Document{"host": "h2"})
		ldb.seedScoped("rule", ten, db.Document{"name": "r1"})
		ldb.seedScoped(auth.RoleCollection, ten, db.Document{"name": "admin"})
		ldb.seedScoped("user", ten, db.Document{"name": "alice"})
		ldb.seedScoped(auth.RefreshCollection, ten, db.Document{"token_hash": "deadbeef"})
	}

	rec := doJSON(t, r, http.MethodDelete, "/api/v1/tenant/acme", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// acme's data must be gone from every scoped collection.
	for _, col := range []string{"record", "rule", auth.RoleCollection, "user", auth.RefreshCollection} {
		require.Equal(t, 0, ldb.count(col, "acme"),
			"delete must purge acme's %q rows", col)
	}
	// acme's registry doc must be gone.
	require.Equal(t, 0, ldb.count(auth.TenantCollection, "")+len(filterByID(ldb, auth.TenantCollection, "acme")))
	require.Empty(t, filterByID(ldb, auth.TenantCollection, "acme"),
		"delete must remove acme's registry doc")

	// beta's data must be untouched.
	require.Equal(t, 2, ldb.count("record", "beta"), "beta records must survive")
	require.Equal(t, 1, ldb.count("rule", "beta"), "beta rule must survive")
	require.Equal(t, 1, ldb.count(auth.RoleCollection, "beta"), "beta role must survive")
	require.Equal(t, 1, ldb.count("user", "beta"), "beta user must survive")
	require.Equal(t, 1, ldb.count(auth.RefreshCollection, "beta"), "beta refresh token must survive")
	require.NotEmpty(t, filterByID(ldb, auth.TenantCollection, "beta"), "beta registry doc must survive")
}

func filterByID(ldb *lifecycleDB, collection, id string) []db.Document {
	ldb.mu.Lock()
	defer ldb.mu.Unlock()
	var out []db.Document
	for _, d := range ldb.collections[collection] {
		if d["id"] == id {
			out = append(out, d)
		}
	}
	return out
}

// TestTenantDelete_DefaultRefused ensures the reserved default tenant can never
// be deleted, and that its data is never purged.
func TestTenantDelete_DefaultRefused(t *testing.T) {
	ldb := newLifecycleDB()
	r := lifecycleRouter(t, ldb)
	ldb.collections[auth.TenantCollection] = []db.Document{
		{"id": snoozetypes.DefaultTenant, "status": "active", "uid": "t-default"},
	}
	ldb.seedScoped("record", snoozetypes.DefaultTenant, db.Document{"host": "h1"})

	rec := doJSON(t, r, http.MethodDelete, "/api/v1/tenant/"+snoozetypes.DefaultTenant, "")
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Equal(t, 1, ldb.count("record", snoozetypes.DefaultTenant),
		"default tenant data must never be purged")
	require.NotEmpty(t, filterByID(ldb, auth.TenantCollection, snoozetypes.DefaultTenant),
		"default registry doc must survive")
}
