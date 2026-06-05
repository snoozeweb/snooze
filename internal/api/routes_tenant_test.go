package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// tenantDB is a minimal db.Driver stub for tenant route tests. It records
// writes and returns configurable documents for reads.
type tenantDB struct {
	fakeDB
	written  []db.Document
	docs     []db.Document
	writeErr error
}

func (t *tenantDB) Write(_ context.Context, _ string, docs []db.Document, _ db.WriteOptions) (db.WriteResult, error) {
	if t.writeErr != nil {
		return db.WriteResult{}, t.writeErr
	}
	t.written = append(t.written, docs...)
	uids := make([]string, 0, len(docs))
	for _, d := range docs {
		if id, ok := d["id"].(string); ok {
			uids = append(uids, id)
		}
	}
	return db.WriteResult{Added: uids}, nil
}

func (t *tenantDB) Search(_ context.Context, _ string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	return t.docs, len(t.docs), nil
}

func (t *tenantDB) GetOne(_ context.Context, _ string, match db.Document) (db.Document, error) {
	if id, ok := match["id"].(string); ok {
		for _, d := range t.docs {
			if d["id"] == id {
				return d, nil
			}
		}
	}
	return nil, nil
}

func (t *tenantDB) UpdateOne(_ context.Context, _, _ string, patch db.Document, _ bool) error {
	t.written = append(t.written, patch)
	return nil
}

func (t *tenantDB) Delete(_ context.Context, _ string, _ condition.Cond, _ bool) (int, error) {
	return 1, nil
}

// tenantRouter sets up a chi router with mountTenant wired and returns it
// with the injected DB and a helper to build authorized requests.
func tenantRouter(t *testing.T, tdb *tenantDB, perms ...string) (chi.Router, *Router) {
	t.Helper()
	rt := &Router{
		Auth: testTokenEngine(t),
		DB:   tdb,
	}
	r := chi.NewRouter()
	// Inject authenticated claims with the given permissions directly (skip the
	// full auth middleware). Claims are always present so a request lacking the
	// required permission is rejected as 403 Forbidden (insufficient perms),
	// not 401 Unauthorized (no identity) — matching middleware.RequirePerm's
	// established semantics.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithClaims(req.Context(), snoozetypes.Claims{
				Subject: "admin",
				Method:  "local",
				// Platform admins live in the default tenant (D5). The tenant
				// registry is platform-gated, so these tests model an operator
				// authenticated against the default tenant.
				TenantID:    snoozetypes.DefaultTenant,
				Permissions: perms,
			})
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})
	rt.mountTenant(r)
	return r, rt
}

// tenantRouterClaims is like tenantRouter but injects a full Claims value so
// the test can control the TenantID origin (platform-permission gating).
func tenantRouterClaims(t *testing.T, tdb *tenantDB, claims snoozetypes.Claims) chi.Router {
	t.Helper()
	rt := &Router{
		Auth: testTokenEngine(t),
		DB:   tdb,
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithClaims(req.Context(), claims)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	rt.mountTenant(r)
	return r
}

// TestTenant_TenantAdminRwAllForbidden reproduces C4: a tenant admin seeded
// with the rw_all wildcard but living in a non-default tenant must be denied
// on every /api/v1/tenant verb. Before the fix, RequirePerm honored rw_all and
// let them list/create/delete every org on the global tenant collection.
func TestTenant_TenantAdminRwAllForbidden(t *testing.T) {
	claims := snoozetypes.Claims{
		Subject:     "acme-admin",
		TenantID:    "acme",
		Permissions: []string{auth.AllPermission},
	}

	t.Run("GET list", func(t *testing.T) {
		tdb := &tenantDB{docs: []db.Document{{"id": "acme"}}}
		r := tenantRouterClaims(t, tdb, claims)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil))
		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("POST create", func(t *testing.T) {
		tdb := &tenantDB{}
		r := tenantRouterClaims(t, tdb, claims)
		body := bytes.NewBufferString(`{"id":"evil","display_name":"Evil"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenant", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Empty(t, tdb.written, "no tenant doc may be written by a tenant admin")
	})

	t.Run("DELETE", func(t *testing.T) {
		tdb := &tenantDB{docs: []db.Document{{"id": "beta"}}}
		r := tenantRouterClaims(t, tdb, claims)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/tenant/beta", nil))
		require.Equal(t, http.StatusForbidden, rec.Code)
	})
}

// TestTenant_DefaultTenantPlatformAdminAllowed is path (b): a default-tenant
// admin carrying the literal platform permission may operate the registry.
func TestTenant_DefaultTenantPlatformAdminAllowed(t *testing.T) {
	claims := snoozetypes.Claims{
		Subject:     "root",
		TenantID:    snoozetypes.DefaultTenant,
		Permissions: []string{auth.PermWriteTenant},
	}

	t.Run("GET list", func(t *testing.T) {
		tdb := &tenantDB{docs: []db.Document{{"id": "acme"}}}
		r := tenantRouterClaims(t, tdb, claims)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil))
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("POST create", func(t *testing.T) {
		tdb := &tenantDB{}
		r := tenantRouterClaims(t, tdb, claims)
		body := bytes.NewBufferString(`{"id":"acme","display_name":"Acme"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenant", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code)
		// The registry doc is written first; seedTenant then adds the default
		// roles + init_db marker (H5), so written is >= 1 with the tenant doc
		// at index 0.
		require.NotEmpty(t, tdb.written)
		require.Equal(t, "acme", tdb.written[0]["id"])
	})
}

func TestTenantCreate_RequiresWritePerm(t *testing.T) {
	tdb := &tenantDB{}
	r, _ := tenantRouter(t, tdb /* no permissions */)
	body := bytes.NewBufferString(`{"id":"acme","display_name":"Acme"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenant", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, tdb.written)
}

func TestTenantCreate_Success(t *testing.T) {
	tdb := &tenantDB{}
	r, _ := tenantRouter(t, tdb, auth.PermWriteTenant)
	body := bytes.NewBufferString(`{"id":"acme","display_name":"Acme Corp"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenant", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	// The tenant registry doc is the first write; seedTenant then writes the
	// default roles + init_db marker (H5).
	require.NotEmpty(t, tdb.written)
	require.Equal(t, "acme", tdb.written[0]["id"])
	// status must be seeded as "active"
	require.Equal(t, "active", tdb.written[0]["status"])
}

func TestTenantCreate_InvalidSlug(t *testing.T) {
	tdb := &tenantDB{}
	r, _ := tenantRouter(t, tdb, auth.PermWriteTenant)
	body := bytes.NewBufferString(`{"id":"Bad Slug!","display_name":"Bad"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenant", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestTenantList_RequiresReadPerm(t *testing.T) {
	tdb := &tenantDB{docs: []db.Document{{"id": "acme"}}}
	r, _ := tenantRouter(t, tdb /* no permissions */)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTenantList_Success(t *testing.T) {
	tdb := &tenantDB{docs: []db.Document{
		{"id": "acme", "display_name": "Acme"},
		{"id": "beta", "display_name": "Beta"},
	}}
	r, _ := tenantRouter(t, tdb, auth.PermReadTenant)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
}

func TestTenantGetOne_Success(t *testing.T) {
	tdb := &tenantDB{docs: []db.Document{{"id": "acme", "display_name": "Acme"}}}
	r, _ := tenantRouter(t, tdb, auth.PermReadTenant)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenant/acme", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Equal(t, "acme", doc["id"])
}

func TestTenantGetOne_NotFound(t *testing.T) {
	tdb := &tenantDB{docs: nil}
	r, _ := tenantRouter(t, tdb, auth.PermReadTenant)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenant/nonexistent", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTenantUpdate_RequiresWritePerm(t *testing.T) {
	tdb := &tenantDB{docs: []db.Document{{"id": "acme"}}}
	r, _ := tenantRouter(t, tdb, auth.PermReadTenant /* read only */)
	body := bytes.NewBufferString(`{"display_name":"New Name"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenant/acme", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTenantUpdate_CannotChangeID(t *testing.T) {
	tdb := &tenantDB{docs: []db.Document{{"id": "acme", "display_name": "Acme"}}}
	r, _ := tenantRouter(t, tdb, auth.PermWriteTenant)
	// Attempting to change id must be rejected.
	body := bytes.NewBufferString(`{"id":"other","display_name":"Renamed"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenant/acme", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestTenantUpdate_Success(t *testing.T) {
	tdb := &tenantDB{docs: []db.Document{{"id": "acme", "display_name": "Acme"}}}
	r, _ := tenantRouter(t, tdb, auth.PermWriteTenant)
	body := bytes.NewBufferString(`{"display_name":"Acme Corp"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenant/acme", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestTenantDelete_DefaultUndeletable(t *testing.T) {
	tdb := &tenantDB{docs: []db.Document{{"id": "default"}}}
	r, _ := tenantRouter(t, tdb, auth.PermWriteTenant)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/tenant/default", nil))
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestTenantDelete_Success(t *testing.T) {
	tdb := &tenantDB{docs: []db.Document{{"id": "acme"}}}
	r, _ := tenantRouter(t, tdb, auth.PermWriteTenant)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/tenant/acme", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestLogin_SuspendedTenantBlocked(t *testing.T) {
	// A tenant with status=suspended must block login for that org.
	tdb := &tenantDB{
		docs: []db.Document{
			{"id": "acme", "status": "suspended"},
		},
	}
	localProvider := &fakeProvider{
		name:     "local",
		wantUser: "alice",
		wantPass: "secret",
		enabled:  true,
		identity: auth.Identity{Username: "alice", Method: "local", TenantID: "acme"},
	}
	reg := auth.NewRegistry()
	reg.Register(localProvider)

	rt := &Router{
		Auth:      testTokenEngine(t),
		Refresh:   &fakeRefresh{},
		Providers: reg,
		DB:        tdb,
	}
	r := chi.NewRouter()
	rt.mountLogin(r)

	body := bytes.NewBufferString(`{"username":"alice","password":"secret","org":"acme"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/local", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestLogin_ActiveTenantAllowed(t *testing.T) {
	tdb := &tenantDB{
		docs: []db.Document{
			{"id": "acme", "status": "active"},
		},
	}
	localProvider := &fakeProvider{
		name:     "local",
		wantUser: "alice",
		wantPass: "secret",
		enabled:  true,
		identity: auth.Identity{Username: "alice", Method: "local", TenantID: "acme"},
	}
	reg := auth.NewRegistry()
	reg.Register(localProvider)

	rt := &Router{
		Auth:      testTokenEngine(t),
		Refresh:   &fakeRefresh{},
		Providers: reg,
		DB:        tdb,
	}
	r := chi.NewRouter()
	rt.mountLogin(r)

	body := bytes.NewBufferString(`{"username":"alice","password":"secret","org":"acme"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/local", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestTenantRoutes_RegisteredInBuild(t *testing.T) {
	rt := &Router{Auth: testTokenEngine(t)}
	h := rt.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()
	// Without auth the endpoints exist but return 401/403 not 404.
	resp, err := http.Get(srv.URL + "/api/v1/tenant")
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
		"/api/v1/tenant must be mounted")
}
