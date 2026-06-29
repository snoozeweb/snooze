package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
)

// mountHousekeeping wires the on-demand housekeeping admin endpoints under
// /api/v1/housekeeping. Both require rw_all (auth.AllPermission); the run
// endpoint fires every registered cleanup job synchronously in the request.
func (rt *Router) mountHousekeeping(r chi.Router) {
	r.Route("/api/v1/housekeeping", func(sub chi.Router) {
		sub.Use(middleware.RequirePerm(auth.AllPermission))
		sub.Get("/status", rt.handleHousekeepingStatus)
		sub.Post("/run", rt.handleHousekeepingRun)
	})
}

// handleHousekeepingStatus reports whether the housekeeper is wired and, when
// it is, how many jobs are registered. Returns 503 when the housekeeper is not
// configured (rt.HK == nil) so operators can probe before triggering a run.
func (rt *Router) handleHousekeepingStatus(w http.ResponseWriter, r *http.Request) {
	if rt.HK == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("housekeeper not configured"))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"registered_jobs": rt.HK.RegisteredJobs(),
	})
}

// handleHousekeepingRun fires every registered housekeeping job once,
// synchronously, and returns the per-job results. The HTTP status is 200 even
// when individual jobs failed — failures are surfaced in each job's `error`
// field and the top-level `errors` count. Returns 503 when the housekeeper is
// not configured.
//
// The jobs run while the scheduler goroutine may also be firing them; they are
// idempotent DB sweeps, so overlapping runs are benign. Long-running jobs are
// bounded only by the HTTP server's WriteTimeout and ctx cancellation.
func (rt *Router) handleHousekeepingRun(w http.ResponseWriter, r *http.Request) {
	if rt.HK == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("housekeeper not configured"))
		return
	}
	results := rt.HK.RunAll(r.Context())
	errCount := 0
	for _, res := range results {
		if res.Error != "" {
			errCount++
		}
	}
	if rt.Logger != nil {
		rt.Logger.Info("housekeeping: on-demand run complete",
			"jobs", len(results), "errors", errCount)
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"jobs":   results,
		"errors": errCount,
	})
}
