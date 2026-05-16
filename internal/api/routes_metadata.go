package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// mountMetadata wires:
//
//	GET /api/v1/metadata           — every registered plugin's metadata
//	GET /api/v1/metadata/{plugin}  — one plugin's metadata (404 if missing)
//
// The React frontend consumes this to render plugin-defined typed forms
// (e.g. Action subtypes like Mail / Webhook / Patlite) instead of free-form
// JSON textareas. The payload is the parsed metadata.yaml — see
// plugins.Metadata for the field set.
func (rt *Router) mountMetadata(r chi.Router) {
	r.Route("/api/v1/metadata", func(sub chi.Router) {
		sub.Get("/", rt.handleMetadataList)
		sub.Get("/{plugin}", rt.handleMetadataOne)
	})
}

// handleMetadataList returns every registered plugin's metadata, sorted by
// plugin name so the response is deterministic across runs.
//
// Every entry's PluginName is stamped with the registry key before
// serialisation. We can't rely on the YAML `name:` field (most action plugin
// metadata.yamls use it as a human display label, e.g. "Send email") so the
// frontend needs a separate machine-readable handle to match `action_type`
// against.
func (rt *Router) handleMetadataList(w http.ResponseWriter, _ *http.Request) {
	names := make([]string, 0, len(rt.Plugins))
	for name := range rt.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]plugins.Metadata, 0, len(names))
	for _, name := range names {
		meta := rt.Plugins[name].Metadata()
		meta.PluginName = name
		out = append(out, meta)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

// handleMetadataOne returns the metadata of a single plugin by name. 404 if
// the plugin isn't registered. As in the list handler, the response's
// PluginName is stamped with the registry key (URL param) so the frontend
// can match it regardless of the YAML `name:` value.
func (rt *Router) handleMetadataOne(w http.ResponseWriter, r *http.Request) {
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
	meta := p.Metadata()
	meta.PluginName = name
	WriteJSON(w, http.StatusOK, map[string]any{"data": meta})
}
