package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/japannext/snooze/internal/plugins"
)

// healthCheckTimeout caps the per-subsystem readiness probes so /readyz
// never blocks the load balancer.
const healthCheckTimeout = 2 * time.Second

// mountHealth wires the liveness/readiness/verbose health endpoints.
func (rt *Router) mountHealth(r chi.Router) {
	r.Get("/healthz", rt.handleLive)
	r.Get("/readyz", rt.handleReady)
	r.Route("/api/v1/health", func(sub chi.Router) {
		sub.Get("/", rt.handleHealthVerbose)
	})
}

// handleLive is the liveness probe: cheap, returns 200 as long as the
// process is up.
func (rt *Router) handleLive(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleReady is the readiness probe. We poll the database driver via
// ListCollections (cheap on every backend) with a tight timeout.
func (rt *Router) handleReady(w http.ResponseWriter, r *http.Request) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("db not configured"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()
	if _, err := rt.DB.ListCollections(ctx); err != nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database unreachable").WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleHealthVerbose returns the per-subsystem health: db status, registered
// plugins, and (when wired) the cluster node set surfaced by the syncer.
func (rt *Router) handleHealthVerbose(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status": "ok",
	}
	subsystems := map[string]string{}
	if rt.DB != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
		defer cancel()
		if _, err := rt.DB.ListCollections(ctx); err != nil {
			subsystems["db"] = "degraded: " + err.Error()
			resp["status"] = "degraded"
		} else {
			subsystems["db"] = "ok"
		}
	} else {
		subsystems["db"] = "absent"
	}

	pluginNames := make([]string, 0, len(rt.Plugins))
	for name, p := range rt.Plugins {
		if _, ok := p.(plugins.Plugin); ok {
			pluginNames = append(pluginNames, name)
		}
	}
	resp["subsystems"] = subsystems
	resp["plugins"] = pluginNames
	WriteJSON(w, http.StatusOK, resp)
}
