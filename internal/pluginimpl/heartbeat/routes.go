package heartbeat

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

// RegisterRoutes implements plugins.RouteProvider. The heartbeat plugin mounts
// its own GET / and GET /{uid} handlers so it can project a read-time `status`
// field onto every heartbeat document, then delegates the entire write surface
// (POST/PUT/PATCH/DELETE/search) to the canonical CRUD constructors via
// plugins.MountCRUDWriteRoutes — so writes are byte-for-byte identical to every
// other plugin. The webhook ping (/api/v1/webhook/heartbeat) is mounted
// separately by the router's mountWebhooks and is unaffected by this.
func (p *Plugin) RegisterRoutes(r chi.Router, host plugins.Host) {
	r.Get("/", p.handleListHeartbeats(host))
	r.Get("/{uid}", p.handleGetOneHeartbeat(host))
	plugins.MountCRUDWriteRoutes(r, host, p, collection)
}

// handleListHeartbeats serves GET /api/v1/heartbeat. It runs the same list
// query the generic CRUD list handler would (honouring ?q/?limit/?offset/
// ?orderby/?asc), then projects a computed `status` onto every parseable
// document. An optional ?status= filter keeps only documents whose computed
// status matches (the heartbeat collection is small by nature, so an in-memory
// filter after the fetch is the pragmatic choice — no DB change, no new index).
//
// Malformed documents (parseHeartbeat ok=false) are omitted from the response
// rather than crashing the handler — fail-closed, consistent with the scanner.
func (p *Plugin) handleListHeartbeats(host plugins.Host) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, cond, err := plugins.DecodeListParams(r)
		if err != nil {
			plugins.WriteJSON(w, http.StatusBadRequest, errorEnvelope("bad_request", err.Error()))
			return
		}
		driver := host.DB()
		if driver == nil {
			plugins.WriteJSON(w, http.StatusInternalServerError, errorEnvelope("db_error", "no database available"))
			return
		}
		docs, total, err := driver.Search(r.Context(), collection, cond, page)
		if err != nil {
			plugins.WriteJSON(w, http.StatusInternalServerError, errorEnvelope("db_error", err.Error()))
			return
		}

		// Optional ?status= filter (comma-separated values, e.g. ?status=ok,overdue).
		wanted := wantedStatuses(r)

		now := p.now()
		out := make([]db.Document, 0, len(docs))
		for _, doc := range docs {
			hb, ok := parseHeartbeat(doc)
			if !ok {
				// Malformed row: omit rather than crash.
				continue
			}
			status := computeStatus(hb, now)
			doc["status"] = status
			if wanted != nil && !wanted[status] {
				continue
			}
			out = append(out, doc)
		}

		// When a ?status= filter trims the page, the envelope count reflects the
		// returned slice; total stays the unfiltered driver total so paging math
		// upstream is unaffected.
		count := len(out)
		reportedTotal := total
		if wanted != nil {
			reportedTotal = count
		}

		plugins.WriteJSON(w, http.StatusOK, map[string]any{
			"data": out,
			"meta": map[string]any{
				"count":  count,
				"limit":  page.PerPage,
				"offset": offsetFromPage(page),
				"total":  reportedTotal,
			},
		})
	}
}

// handleGetOneHeartbeat serves GET /api/v1/heartbeat/{uid}. It fetches the one
// document, projects `status`, and returns 200. A missing document is 404 and a
// malformed (but present) document is returned with no status rather than
// failing the read.
func (p *Plugin) handleGetOneHeartbeat(host plugins.Host) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		if uid == "" {
			plugins.WriteJSON(w, http.StatusBadRequest, errorEnvelope("bad_request", "missing uid"))
			return
		}
		driver := host.DB()
		if driver == nil {
			plugins.WriteJSON(w, http.StatusInternalServerError, errorEnvelope("db_error", "no database available"))
			return
		}
		doc, err := driver.GetOne(r.Context(), collection, db.Document{"uid": uid})
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				plugins.WriteJSON(w, http.StatusNotFound, errorEnvelope("not_found", "uid "+uid))
				return
			}
			plugins.WriteJSON(w, http.StatusNotFound, errorEnvelope("not_found", err.Error()))
			return
		}
		if hb, ok := parseHeartbeat(doc); ok {
			doc["status"] = computeStatus(hb, p.now())
		}
		plugins.WriteJSON(w, http.StatusOK, doc)
	}
}

// wantedStatuses parses the optional ?status= query into a set of accepted
// status values. A comma-separated list (?status=ok,overdue) is supported;
// blank entries are ignored. Returns nil when no ?status= param is present,
// signalling "no filter".
func wantedStatuses(r *http.Request) map[HeartbeatStatus]bool {
	raw, present := r.URL.Query()["status"]
	if !present {
		return nil
	}
	set := map[HeartbeatStatus]bool{}
	for _, v := range raw {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				set[part] = true
			}
		}
	}
	return set
}

// offsetFromPage reverses Page.PageNb back into an offset for the response
// envelope, mirroring the generic CRUD list handler so clients see what they
// sent.
func offsetFromPage(p db.Page) int {
	if p.PerPage == 0 || p.PageNb == 0 {
		return 0
	}
	return (p.PageNb - 1) * p.PerPage
}

// errorEnvelope builds the minimal {error:{code,message}} body the generic CRUD
// layer emits, so heartbeat read errors look identical to every other endpoint.
func errorEnvelope(code, msg string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	}
}
