package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// mountSchema wires GET /api/v1/schema/{plugin}.
//
// The plugin only contributes a schema when it satisfies plugins.DataModel.
func (rt *Router) mountSchema(r chi.Router) {
	r.Route("/api/v1/schema", func(sub chi.Router) {
		sub.Get("/{plugin}", rt.handleSchema)
	})
}

// handleSchema returns the JSON schema attached to a DataModel plugin.
func (rt *Router) handleSchema(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "plugin")
	if name == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("missing plugin name"))
		return
	}
	p, ok := rt.Plugins[name]
	if !ok {
		WriteError(w, r, ErrNotFound.WithMessage("unknown plugin"))
		return
	}
	dm, ok := p.(plugins.DataModel)
	if !ok {
		WriteError(w, r, ErrNotFound.WithMessage("plugin has no schema"))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"data": dm.Schema()})
}

// mountPermissions wires GET /api/v1/permissions which enumerates every
// permission string an authorizer can actually honour. The output is the
// sorted union of the canonical {rw,ro}_all wildcards, a per-plugin
// {rw,ro}_<name> pair, and every permission named in a route's
// authorization_policy (read+write). It deliberately does NOT surface raw
// metadata.provides entries: `provides` advertises plugin capabilities
// (e.g. `notifier` / `receiver`) that the authorizer never checks, so listing
// them as assignable permissions would be misleading — a role could "grant"
// them yet nothing would change. A functional custom permission such as
// `can_comment` still appears because the comment plugin references it in its
// authorization_policy, which the loop below picks up.
func (rt *Router) mountPermissions(r chi.Router) {
	r.Get("/api/v1/permissions", rt.handlePermissions)
}

func (rt *Router) handlePermissions(w http.ResponseWriter, _ *http.Request) {
	set := map[string]struct{}{
		"rw_all": {},
		"ro_all": {},
	}
	add := func(perm string) {
		// Skip the empty string and the `any` sentinel: `any` is an implicit
		// grant the authorizer adds to every authenticated caller, never an
		// assignable permission (see plugins.AuthorizationPolicy).
		if perm == "" || perm == "any" {
			return
		}
		set[perm] = struct{}{}
	}
	// addPolicy walks the Read+Write lists of a route's AuthorizationPolicy.
	// Nil-safe: a route may carry no policy at all.
	addPolicy := func(pol *plugins.AuthorizationPolicy) {
		if pol == nil {
			return
		}
		for _, perm := range pol.Read {
			add(perm)
		}
		for _, perm := range pol.Write {
			add(perm)
		}
	}
	for name, p := range rt.Plugins {
		set["rw_"+name] = struct{}{}
		set["ro_"+name] = struct{}{}
		meta := p.Metadata()
		// Assignable custom permissions are declared in an AuthorizationPolicy
		// (on the plugin-level RouteDefaults or on a per-path Routes override).
		// Walk both so the catalog never silently omits one. metadata.provides
		// is intentionally NOT consulted here — see mountPermissions.
		addPolicy(meta.RouteDefaults.AuthorizationPolicy)
		for _, route := range meta.Routes {
			addPolicy(route.AuthorizationPolicy)
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}
