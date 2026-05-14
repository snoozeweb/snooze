package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// mountAlerts wires POST /api/v1/alerts.
//
// The handler accepts a single record or an array, calls Processor.ProcessRecord
// on each, then writes a small batch envelope: {data: [...], errors: [...]}.
func (rt *Router) mountAlerts(r chi.Router) {
	if rt.Processor == nil {
		return
	}
	r.Route("/api/v1/alerts", func(sub chi.Router) {
		sub.Post("/", rt.handleAlertPost)
	})
}

// handleAlertPost ingests one or many alert payloads.
func (rt *Router) handleAlertPost(w http.ResponseWriter, r *http.Request) {
	if rt.Processor == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("alert ingestion disabled"))
		return
	}
	records, err := ParseJSONOrArray(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	out := struct {
		Data   []map[string]any `json:"data"`
		Errors []string         `json:"errors,omitempty"`
	}{
		Data: make([]map[string]any, 0, len(records)),
	}
	for _, rec := range records {
		res, err := rt.Processor.ProcessRecord(r.Context(), rec)
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		if res != nil {
			out.Data = append(out.Data, res)
		}
	}
	WriteJSON(w, http.StatusOK, out)
}
