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
// permission string contributed by registered plugins. The output is the
// sorted union of metadata.provides + the canonical {rw,ro}_all wildcards
// + a per-plugin {rw,ro}_<name> pair, matching the Python convention.
func (rt *Router) mountPermissions(r chi.Router) {
	r.Get("/api/v1/permissions", rt.handlePermissions)
}

func (rt *Router) handlePermissions(w http.ResponseWriter, _ *http.Request) {
	set := map[string]struct{}{
		"rw_all": {},
		"ro_all": {},
	}
	for name, p := range rt.Plugins {
		set["rw_"+name] = struct{}{}
		set["ro_"+name] = struct{}{}
		for _, perm := range p.Metadata().Provides {
			if perm != "" {
				set[perm] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}
