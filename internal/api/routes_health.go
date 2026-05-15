package api

import (
	"context"
	"net/http"
	"sort"
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
	r.Route("/api/v1/cluster", func(sub chi.Router) {
		sub.Get("/status", rt.handleClusterStatus)
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

// handleClusterStatus serves the /web/admin/status page. Today a Go snooze
// runs as a single node (the syncer is a peer-to-peer fan-out for write
// invalidation, not a Raft cluster); we surface the local node + every
// loaded plugin so the page is informative even in standalone mode.
func (rt *Router) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	type member struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	type pluginInfo struct {
		Name   string `json:"name"`
		Loaded bool   `json:"loaded"`
	}

	pluginNames := make([]string, 0, len(rt.Plugins))
	for name := range rt.Plugins {
		pluginNames = append(pluginNames, name)
	}
	sort.Strings(pluginNames)
	pluginRows := make([]pluginInfo, 0, len(pluginNames))
	for _, n := range pluginNames {
		pluginRows = append(pluginRows, pluginInfo{Name: n, Loaded: true})
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"cluster": map[string]any{
			"members": []member{{Name: "standalone", Status: "ok"}},
			"leader":  "standalone",
		},
		"plugins": pluginRows,
	})
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
