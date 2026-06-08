package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/migrate"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// slugRE is the canonical slug validator for tenant IDs.
var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*[a-z0-9]$|^[a-z0-9]$`)

// generateLoginKey returns a URL-safe 192-bit (24-byte) opaque discovery key.
func generateLoginKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// tenantGeneralCollection holds the per-tenant bootstrap marker (init_db),
// mirroring core.BootstrapDB's "general" sentinel collection.
const tenantGeneralCollection = "general"

// defaultTenantRoles are the RBAC roles seeded into every freshly-created
// tenant's scope. They mirror pluginimpl/tenant.defaultRoles so a tenant born
// through the platform-scoped control plane is identical to one born through
// the generic CRUD AfterCreate hook. Note these are deliberately tenant-local:
// none of them carries the reserved platform permissions (rw_tenant/ro_tenant).
var defaultTenantRoles = []db.Document{
	{
		"name":        "admin",
		"permissions": []string{auth.AllPermission},
		"description": "Full access within the tenant",
	},
	{
		"name":        "viewer",
		"permissions": []string{"ro_all"},
		"description": "Read-only access within the tenant",
	},
	{
		"name":        "notifications",
		"permissions": []string{"rw_notification", "ro_all"},
		"description": "Manage notifications within the tenant",
	},
}

// adminCredential is the one-time credential returned when a tenant's first
// local admin is provisioned (or its password is regenerated).
type adminCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Method   string `json:"method"`
	Created  bool   `json:"created"`
}

// tenantCreateResponse is the create response: the written tenant id(s) and,
// when an admin was provisioned, the one-time admin credential. We expose an
// explicit lowercase `added` rather than embedding db.WriteResult (whose fields
// have no json tags and would serialize PascalCase) so the JSON shape is
// consistent with the lowercase `admin` field and the web/CLI clients.
type tenantCreateResponse struct {
	Added []string         `json:"added,omitempty"`
	Admin *adminCredential `json:"admin,omitempty"`
}

// mountTenant wires the platform-permission-gated tenant registry under
// /api/v1/tenant. Every handler runs under auth.WithPlatformScope so the
// driver bypasses per-tenant injection for the global "tenant" collection.
//
// Routes:
//
//	POST   /api/v1/tenant           — create (rw_tenant)
//	GET    /api/v1/tenant           — list   (ro_tenant)
//	GET    /api/v1/tenant/{id}      — get one (ro_tenant)
//	POST   /api/v1/tenant/{id}/admin — ensure/reset the tenant's local admin (rw_tenant)
//	PATCH  /api/v1/tenant/{id}      — update display_name/status/ingest_token (rw_tenant)
//	DELETE /api/v1/tenant/{id}      — delete; "default" is undeletable (rw_tenant)
func (rt *Router) mountTenant(r chi.Router) {
	r.Route("/api/v1/tenant", func(sub chi.Router) {
		// Control-plane routes are platform-gated: RequirePlatformPerm does a
		// LITERAL permission check (rw_all is NOT honored) and requires the
		// caller to be authenticated against the default tenant (D5). This
		// closes C4 — a tenant admin seeded with rw_all can no longer touch the
		// global tenant registry.
		//
		// Read endpoints (ro_tenant or rw_tenant).
		sub.With(middleware.RequirePlatformPerm(auth.PermReadTenant, auth.PermWriteTenant)).
			Get("/", rt.handleTenantList)
		sub.With(middleware.RequirePlatformPerm(auth.PermReadTenant, auth.PermWriteTenant)).
			Get("/{id}", rt.handleTenantGet)
		// Write endpoints (rw_tenant only).
		sub.With(middleware.RequirePlatformPerm(auth.PermWriteTenant)).
			Post("/", rt.handleTenantCreate)
		sub.With(middleware.RequirePlatformPerm(auth.PermWriteTenant)).
			Post("/{id}/admin", rt.handleTenantAdminReset)
		sub.With(middleware.RequirePlatformPerm(auth.PermWriteTenant)).
			Patch("/{id}", rt.handleTenantUpdate)
		sub.With(middleware.RequirePlatformPerm(auth.PermWriteTenant)).
			Delete("/{id}", rt.handleTenantDelete)
	})
}

// platformCtx returns a context carrying platform scope so the driver skips
// tenant_id injection for the global tenant collection.
func (rt *Router) platformCtx(r *http.Request) *http.Request {
	ctx := auth.WithPlatformScope(r.Context())
	return r.WithContext(ctx)
}

// handleTenantCreate POST /api/v1/tenant
func (rt *Router) handleTenantCreate(w http.ResponseWriter, r *http.Request) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database not configured"))
		return
	}
	// Keep the pre-platform context as the seeding base: WithPlatformScope is
	// sticky (there is no un-set), and platform scope OUTRANKS a layered tenant
	// in the driver's TenantScope precedence — so seeding under
	// WithTenant(platformCtx, id) would silently skip tenant_id stamping and
	// write NULL-tenant rows. Seed from the bare request context instead.
	baseCtx := r.Context()
	r = rt.platformCtx(r)

	var doc db.Document
	if err := ParseJSONBody(r, &doc); err != nil {
		WriteError(w, r, err)
		return
	}

	id, _ := doc["id"].(string)
	if id == "" {
		WriteError(w, r, ErrValidation.WithMessage("tenant: id is required"))
		return
	}
	if !slugRE.MatchString(id) {
		WriteError(w, r, ErrValidation.WithMessage("tenant: id must be a lowercase URL-safe slug (letters, digits, hyphens)"))
		return
	}

	// Stamp defaults.
	if _, ok := doc["status"]; !ok {
		doc["status"] = "active"
	}
	if _, ok := doc["listed"]; !ok {
		doc["listed"] = true
	}
	key, err := generateLoginKey()
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	doc["login_key"] = key
	now := time.Now().Unix()
	doc["created_at"] = now
	doc["updated_at"] = now

	res, err := rt.DB.Write(r.Context(), "tenant", []db.Document{doc}, db.WriteOptions{
		Primary:         []string{"id"},
		DuplicatePolicy: "reject",
		UpdateTime:      false, // we stamped manually above
	})
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if len(res.Rejected) > 0 {
		WriteError(w, r, ErrConflict.WithMessage(res.Rejected[0].Reason))
		return
	}

	// Seed the new tenant under its OWN scope. The platform-scoped control
	// plane bypasses the tenant plugin's generic-CRUD AfterCreate hook (the
	// router skips that mount, see router.go), so the seeding that AfterCreate
	// would have done must happen here — otherwise a brand-new tenant comes up
	// with zero roles and no admin and is unusable (H5). A duplicate tenant id
	// is already rejected above (409), so this only runs for a genuinely new
	// tenant; seedTenant is itself idempotent for retry-after-partial-failure.
	if err := rt.seedTenant(baseCtx, id); err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}

	out := tenantCreateResponse{Added: res.Added}

	// Provision a first local admin unless explicitly suppressed. This is the
	// only isolation-preserving way to seed a brand-new tenant's first admin
	// (the platform admin cannot write users into another tenant). LDAP/SSO-only
	// tenants opt out with create_admin:false.
	createAdmin := true
	if v, ok := doc["create_admin"].(bool); ok {
		createAdmin = v
	}
	if createAdmin {
		username, _ := doc["admin_username"].(string)
		adm, err := auth.EnsureTenantAdmin(auth.WithTenant(baseCtx, id), rt.DB, username, false)
		if err != nil {
			// The tenant + roles are already written; surface the id so the
			// operator can recover via the reset endpoint rather than seeing an
			// opaque 500.
			WriteError(w, r, ErrInternal.WithMessage(
				"tenant "+id+" created but admin provisioning failed; recover via POST /api/v1/tenant/"+id+"/admin").WithCause(err))
			return
		}
		if adm.Created {
			out.Admin = &adminCredential{
				Username: adm.Username,
				Password: adm.Password,
				Method:   auth.LocalMethod,
				Created:  true,
			}
		}
	}

	WriteJSON(w, http.StatusCreated, out)
}

// handleTenantAdminReset POST /api/v1/tenant/{id}/admin
//
// Ensure a local admin for tenant {id}: create it if absent, otherwise reset
// its password. Returns the one-time credential. Body is optional:
// {"username":"admin"}. The recovery path for a lost create-time password and
// the later-provisioning path for a tenant created with create_admin:false.
func (rt *Router) handleTenantAdminReset(w http.ResponseWriter, r *http.Request) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database not configured"))
		return
	}
	baseCtx := r.Context()
	r = rt.platformCtx(r)

	id := chi.URLParam(r, "id")
	if id == "" {
		WriteError(w, r, ErrValidation.WithMessage("missing id"))
		return
	}

	// The tenant must exist in the registry. Mirror handleTenantGet: a real
	// driver returns db.ErrNotFound on miss, so treat (err != nil || nil-doc)
	// as 404 rather than 500.
	existing, err := rt.DB.GetOne(r.Context(), "tenant", db.Document{"id": id})
	if err != nil || existing == nil {
		WriteError(w, r, ErrNotFound.WithMessage("tenant not found: "+id))
		return
	}

	// Optional username override.
	var body struct {
		Username string `json:"username"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := ParseJSONBody(r, &body); err != nil {
			WriteError(w, r, err)
			return
		}
	}

	adm, err := auth.EnsureTenantAdmin(auth.WithTenant(baseCtx, id), rt.DB, body.Username, true)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, adminCredential{
		Username: adm.Username,
		Password: adm.Password,
		Method:   auth.LocalMethod,
		Created:  adm.Created,
	})
}

// seedTenant provisions the baseline data a freshly-created tenant needs to be
// usable: the default RBAC roles and the per-tenant init_db marker. All writes
// run under auth.WithTenant(ctx, tenantID) so the driver stamps tenant_id on
// every seeded document. The caller MUST pass a context that does NOT carry
// platform scope (platform scope outranks a layered tenant in TenantScope, so
// it would suppress the tenant_id stamp). Idempotent.
func (rt *Router) seedTenant(ctx context.Context, tenantID string) error {
	scoped := auth.WithTenant(ctx, tenantID)

	// Default roles (admin / viewer / notifications). Upsert by name so a
	// re-seed updates rather than duplicates.
	roles := make([]db.Document, len(defaultTenantRoles))
	for i, r := range defaultTenantRoles {
		cp := make(db.Document, len(r))
		for k, v := range r {
			cp[k] = v
		}
		roles[i] = cp
	}
	if _, err := rt.DB.Write(scoped, auth.RoleCollection, roles, db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	}); err != nil {
		return err
	}

	// Per-tenant init_db marker (mirrors core.BootstrapDB's bootstrap sentinel)
	// so housekeeping/bootstrap logic recognises the tenant as provisioned.
	if _, err := rt.DB.Write(scoped, tenantGeneralCollection, []db.Document{
		{"init_db": true},
	}, db.WriteOptions{
		Primary:    []string{"init_db"},
		UpdateTime: true,
	}); err != nil {
		return err
	}
	return nil
}

// handleTenantList GET /api/v1/tenant
func (rt *Router) handleTenantList(w http.ResponseWriter, r *http.Request) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database not configured"))
		return
	}
	r = rt.platformCtx(r)

	docs, total, err := rt.DB.Search(r.Context(), "tenant",
		condition.Cond{Op: condition.OpAlwaysTrue}, db.Page{})
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if docs == nil {
		docs = []db.Document{}
	}
	type listResp struct {
		Data []db.Document `json:"data"`
		Meta struct {
			Count int `json:"count"`
			Total int `json:"total"`
		} `json:"meta"`
	}
	resp := listResp{Data: docs}
	resp.Meta.Count = len(docs)
	resp.Meta.Total = total
	WriteJSON(w, http.StatusOK, resp)
}

// handleTenantGet GET /api/v1/tenant/{id}
func (rt *Router) handleTenantGet(w http.ResponseWriter, r *http.Request) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database not configured"))
		return
	}
	r = rt.platformCtx(r)

	id := chi.URLParam(r, "id")
	if id == "" {
		WriteError(w, r, ErrValidation.WithMessage("missing id"))
		return
	}
	doc, err := rt.DB.GetOne(r.Context(), "tenant", db.Document{"id": id})
	if err != nil || doc == nil {
		WriteError(w, r, ErrNotFound.WithMessage("tenant not found: "+id))
		return
	}
	WriteJSON(w, http.StatusOK, doc)
}

// handleTenantUpdate PATCH /api/v1/tenant/{id}
// Allowed fields: display_name, status, ingest_token. The id slug is immutable.
func (rt *Router) handleTenantUpdate(w http.ResponseWriter, r *http.Request) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database not configured"))
		return
	}
	r = rt.platformCtx(r)

	id := chi.URLParam(r, "id")
	if id == "" {
		WriteError(w, r, ErrValidation.WithMessage("missing id"))
		return
	}

	var patch db.Document
	if err := ParseJSONBody(r, &patch); err != nil {
		WriteError(w, r, err)
		return
	}

	// id is immutable: reject any attempt to change it.
	if newID, ok := patch["id"]; ok {
		if s, _ := newID.(string); s != id {
			WriteError(w, r, ErrValidation.WithMessage("tenant: id is immutable and cannot be changed"))
			return
		}
		delete(patch, "id")
	}

	// Only allow mutable fields.
	allowed := map[string]struct{}{
		"display_name": {},
		"status":       {},
		"ingest_token": {},
	}
	for k := range patch {
		if _, ok := allowed[k]; !ok {
			delete(patch, k)
		}
	}
	if len(patch) == 0 {
		WriteError(w, r, ErrValidation.WithMessage("no mutable fields provided"))
		return
	}
	patch["updated_at"] = time.Now().Unix()

	// Fetch doc to get uid for UpdateOne.
	existing, err := rt.DB.GetOne(r.Context(), "tenant", db.Document{"id": id})
	if err != nil || existing == nil {
		WriteError(w, r, ErrNotFound.WithMessage("tenant not found: "+id))
		return
	}
	uid, _ := existing["uid"].(string)
	if uid == "" {
		// Fallback: use id as uid if driver does not stamp uid on global docs.
		uid = id
	}

	if err := rt.DB.UpdateOne(r.Context(), "tenant", uid, patch, true); err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"id": id})
}

// handleTenantDelete DELETE /api/v1/tenant/{id}
//
// Deletion is destructive and irreversible, so it follows suspend-first
// semantics: the tenant is first flipped to status=suspended (closing logins
// and ingestion for that org), then its data is cascade-purged across every
// tenant-scoped collection, its refresh tokens are revoked (the refresh_token
// collection is itself tenant-scoped and is purged in the same sweep), and
// finally the registry doc is removed. The reserved "default" tenant can never
// be deleted (its purge would wipe the platform's own roles/users).
func (rt *Router) handleTenantDelete(w http.ResponseWriter, r *http.Request) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database not configured"))
		return
	}
	r = rt.platformCtx(r)

	id := chi.URLParam(r, "id")
	if id == "" {
		WriteError(w, r, ErrValidation.WithMessage("missing id"))
		return
	}
	if id == snoozetypes.DefaultTenant {
		WriteError(w, r, ErrConflict.WithMessage("the \"default\" tenant cannot be deleted"))
		return
	}

	// The tenant must exist before we touch anything.
	existing, err := rt.DB.GetOne(r.Context(), "tenant", db.Document{"id": id})
	if err != nil || existing == nil {
		WriteError(w, r, ErrNotFound.WithMessage("tenant not found: "+id))
		return
	}

	// 1. Suspend first (idempotent): close the org before purging so no new
	//    rows can be written into a collection we are about to wipe.
	if status, _ := existing["status"].(string); status != "suspended" {
		uid, _ := existing["uid"].(string)
		if uid == "" {
			uid = id
		}
		if err := rt.DB.UpdateOne(r.Context(), "tenant", uid, db.Document{
			"status":     "suspended",
			"updated_at": time.Now().Unix(),
		}, true); err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
	}

	// 2. Cascade-purge every tenant-scoped collection. We run under platform
	//    scope (the request ctx) but AND an explicit tenant_id predicate so the
	//    delete is fenced to this tenant only — never a naked global wipe.
	tenantCond := condition.Equals("tenant_id", id)
	purged := 0
	for _, col := range migrate.TenantScopedCollections {
		n, derr := rt.DB.Delete(r.Context(), col, tenantCond, true)
		if derr != nil {
			WriteError(w, r, ErrInternal.WithCause(derr))
			return
		}
		if n > 0 {
			purged += n
		}
	}

	// 3. Finally remove the registry doc (global collection, by id).
	deleted, err := rt.DB.Delete(r.Context(), "tenant",
		condition.Equals("id", id), false)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if deleted == 0 {
		WriteError(w, r, ErrNotFound.WithMessage("tenant not found: "+id))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "purged": purged})
}
