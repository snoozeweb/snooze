// routes_inputs.go exposes db.SourceActivityAggregator over HTTP: per-source
// last-received activity, backing the admin Inputs page.
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/db"
)

// defaultInputsWindow bounds the source-activity scan when the client sends no
// explicit ?since. 30 days keeps the grouped query cheap; inputs idle longer
// surface as "no recent activity" (the frontend renders "never").
const defaultInputsWindow = 30 * 24 * time.Hour

// mountInputs registers GET /api/v1/inputs (per-source last-received activity),
// gated behind the stats read/write permission.
func (rt *Router) mountInputs(r chi.Router) {
	if rt.DB == nil {
		return
	}
	r.Group(func(g chi.Router) {
		g.Use(middleware.RequirePerm("ro_stats", "rw_stats"))
		g.Get("/api/v1/inputs", rt.handleInputs)
	})
}

func (rt *Router) handleInputs(w http.ResponseWriter, r *http.Request) {
	agg, ok := rt.DB.(db.SourceActivityAggregator)
	if !ok {
		WriteError(w, r, ErrUnavailable.WithMessage("source activity not supported by this backend"))
		return
	}
	since := time.Now().Add(-defaultInputsWindow).Unix()
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			WriteError(w, r, ErrBadRequest.WithMessage("bad since parameter"))
			return
		}
		since = n
	}
	rows, err := agg.SourceActivity(r.Context(), since)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"data": rows})
}
