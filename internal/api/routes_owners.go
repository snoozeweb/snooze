// routes_owners.go installs GET /api/v1/record/owners?q=<cond>, the per-owner
// record counts behind the alert list's owner filter. It is mounted before
// the record plugin's CRUD (like mountBulk) so the static /owners segment wins
// over the generic /{uid} handlers.
//
// The count is one Driver.CountBy over `owner`, so the tenant fence is the
// driver's and nothing is loaded into memory per record. Records with an
// absent or empty `owner` — including ghost-only ones that just carry a
// `previous_owner` — make up the `unowned` bucket, kept out of `data`.

package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
)

// ownerField is the record key ownership is grouped by.
const ownerField = "owner"

// ownerCount is one `data` entry of the owners response.
type ownerCount struct {
	Owner string `json:"owner"`
	Count int    `json:"count"`
}

// ownersResponse is the GET /api/v1/record/owners envelope.
type ownersResponse struct {
	Data    []ownerCount `json:"data"`
	Unowned int          `json:"unowned"`
	Total   int          `json:"total"`
}

// mountOwners wires the owners count behind ro_record (rw_record also reads,
// as on the agentic GET).
func (rt *Router) mountOwners(r chi.Router) {
	if rt.DB == nil {
		return
	}
	r.Group(func(g chi.Router) {
		g.Use(middleware.RequirePerm("ro_"+recordCollection, "rw_"+recordCollection))
		g.Get("/api/v1/record/owners", rt.handleRecordOwners)
	})
}

// handleRecordOwners counts the records matching ?q per owner, sorted by count
// descending then owner ascending.
func (rt *Router) handleRecordOwners(w http.ResponseWriter, r *http.Request) {
	cond, err := decodeQueryCond(r)
	if err != nil {
		WriteError(w, r, ErrBadRequest.WithMessage("bad q: "+err.Error()))
		return
	}
	counts, err := rt.DB.CountBy(r.Context(), recordCollection, cond, ownerField)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	out := ownersResponse{Data: make([]ownerCount, 0, len(counts))}
	for owner, n := range counts {
		out.Total += n
		if owner == "" {
			out.Unowned += n
			continue
		}
		out.Data = append(out.Data, ownerCount{Owner: owner, Count: n})
	}
	sort.Slice(out.Data, func(i, j int) bool {
		if out.Data[i].Count != out.Data[j].Count {
			return out.Data[i].Count > out.Data[j].Count
		}
		return out.Data[i].Owner < out.Data[j].Owner
	})
	WriteJSON(w, http.StatusOK, out)
}
