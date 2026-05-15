package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
)

// clusterMemberFreshOK is the max age of a `last_seen` timestamp for a node
// to count as "ok". Beyond this but within `clusterMemberFreshDegraded` the
// member is "degraded"; older still is "down". The thresholds are scaled
// from the syncer's 5s heartbeat default — 4× and 12× respectively.
const (
	clusterMemberFreshOK       = 20 * time.Second
	clusterMemberFreshDegraded = 60 * time.Second
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
func (rt *Router) handleLive(w http.ResponseWriter, _ *http.Request) {
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

// handleClusterStatus serves the /web/admin/status page. Members come from
// the `nodes` collection, which each Snooze instance updates via the syncer
// NodeHeartbeat (every ~5s). Freshness thresholds map last_seen to one of
// ok/degraded/down. When the collection is empty (true single-node deploy)
// we fall back to a hardcoded standalone payload so the page is still
// informative.
func (rt *Router) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
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

	members, leader := rt.discoverMembers(r.Context())

	WriteJSON(w, http.StatusOK, map[string]any{
		"cluster": map[string]any{
			"members": members,
			"leader":  leader,
		},
		"plugins": pluginRows,
	})
}

// discoverMembers reads the `nodes` heartbeat collection and grades each
// row by last_seen freshness. Falls back to a synthetic standalone member
// when the collection is empty or unreachable. The "leader" today is the
// alphabetically-first ok member — Snooze has no real leader election,
// every node serves writes against the shared DB.
//
// Returns the same `member` shape inlined here as the caller (must be a
// fresh declaration here so the slice element type is exported via JSON).
func (rt *Router) discoverMembers(ctx context.Context) (members []map[string]string, leader string) {
	standalone := []map[string]string{{"name": "standalone", "status": "ok"}}
	if rt.DB == nil {
		return standalone, "standalone"
	}
	queryCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()
	docs, _, err := rt.DB.Search(queryCtx, "nodes", condition.Cond{}, db.Page{PerPage: 100})
	if err != nil || len(docs) == 0 {
		return standalone, "standalone"
	}

	now := time.Now().UTC()
	out := make([]map[string]string, 0, len(docs))
	for _, d := range docs {
		name, _ := d["node"].(string)
		if name == "" {
			continue
		}
		status := "down"
		if lastSeenStr, ok := d["last_seen"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, lastSeenStr); err == nil {
				age := now.Sub(t)
				switch {
				case age <= clusterMemberFreshOK:
					status = "ok"
				case age <= clusterMemberFreshDegraded:
					status = "degraded"
				}
			}
		}
		out = append(out, map[string]string{"name": name, "status": status})
	}
	if len(out) == 0 {
		return standalone, "standalone"
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })

	leader = ""
	for _, m := range out {
		if m["status"] == "ok" {
			leader = m["name"]
			break
		}
	}
	return out, leader
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
		if p != nil {
			pluginNames = append(pluginNames, name)
		}
	}
	resp["subsystems"] = subsystems
	resp["plugins"] = pluginNames
	WriteJSON(w, http.StatusOK, resp)
}
