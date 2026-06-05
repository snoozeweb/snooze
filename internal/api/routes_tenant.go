package api

import (
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// slugRE is the canonical slug validator for tenant IDs.
var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*[a-z0-9]$|^[a-z0-9]$`)

// mountTenant wires the platform-permission-gated tenant registry under
// /api/v1/tenant. Every handler runs under auth.WithPlatformScope so the
// driver bypasses per-tenant injection for the global "tenant" collection.
//
// Routes:
//
//	POST   /api/v1/tenant           — create (rw_tenant)
//	GET    /api/v1/tenant           — list   (ro_tenant)
//	GET    /api/v1/tenant/{id}      — get one (ro_tenant)
//	PATCH  /api/v1/tenant/{id}      — update display_name/status/ingest_token (rw_tenant)
//	DELETE /api/v1/tenant/{id}      — delete; "default" is undeletable (rw_tenant)
func (rt *Router) mountTenant(r chi.Router) {
	r.Route("/api/v1/tenant", func(sub chi.Router) {
		// Read endpoints (ro_tenant or rw_tenant).
		sub.With(middleware.RequirePerm(auth.PermReadTenant, auth.PermWriteTenant)).
			Get("/", rt.handleTenantList)
		sub.With(middleware.RequirePerm(auth.PermReadTenant, auth.PermWriteTenant)).
			Get("/{id}", rt.handleTenantGet)
		// Write endpoints (rw_tenant only).
		sub.With(middleware.RequirePerm(auth.PermWriteTenant)).
			Post("/", rt.handleTenantCreate)
		sub.With(middleware.RequirePerm(auth.PermWriteTenant)).
			Patch("/{id}", rt.handleTenantUpdate)
		sub.With(middleware.RequirePerm(auth.PermWriteTenant)).
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
	WriteJSON(w, http.StatusCreated, res)
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
// The reserved "default" tenant cannot be deleted.
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
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}
