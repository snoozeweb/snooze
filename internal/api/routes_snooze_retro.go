// routes_snooze_retro.go installs the legacy /retro_apply endpoint on the
// snooze plugin. The Python era exposed this as `PUT /snooze_apply` and it
// is responsible for replaying a stored snooze rule against records that
// already exist in the database. The endpoint is mounted as a sibling to
// the standard plugin CRUD surface so MountCRUD's generic handlers don't
// need to know about it.
//
// Semantics:
//   - Snooze.discard == true  → delete every record matching the snooze's
//     condition (the legacy plugin's behaviour).
//   - Snooze.discard == false → set the snooze plugin's attribution field to
//     the snooze's name on every matching record so dashboards / filters can
//     hide them. The field name comes from the plugin
//     (plugins.SuppressionOwner), which owns it — see that interface for why
//     server code outside the owner reads the name instead of hard-coding it.
//
// The handler is gated on the `rw_record` permission because it mutates
// the alert collection. We also bump the snooze's hit counter by the
// number of records touched so the Hits column in the snoozes table
// reflects retro-applied activity.
//
//nolint:revive
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

func (rt *Router) mountSnoozeRetro(r chi.Router) {
	if rt.DB == nil {
		return
	}
	// Retro-apply mutates the alert collection (records get tagged or
	// deleted), so the caller must hold rw_record (or the rw_all wildcard
	// for admins). Operators who can only read records cannot replay a
	// snooze against them.
	r.Group(func(g chi.Router) {
		g.Use(middleware.RequirePerm("rw_record"))
		g.Post("/api/v1/snooze/{uid}/retro_apply", rt.handleSnoozeRetroApply)
	})
}

// retroApplyResponse is the JSON envelope the legacy UI expects on success.
// Counts are surfaced so the toast can say "47 alerts updated" without an
// extra round-trip.
type retroApplyResponse struct {
	Matched int    `json:"matched"`
	Deleted int    `json:"deleted,omitempty"`
	Tagged  int    `json:"tagged,omitempty"`
	Snooze  string `json:"snooze"`
}

func (rt *Router) handleSnoozeRetroApply(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("missing snooze uid"))
		return
	}
	ctx := r.Context()

	// Resolve the snooze. The condition is stored on the wire as either the
	// canonical object form or the legacy nested list form; both decode into
	// condition.Cond via the same UnmarshalJSON path.
	doc, err := rt.DB.GetOne(ctx, "snooze", db.Document{"uid": uid})
	if err != nil {
		WriteError(w, r, ErrNotFound.WithMessage("snooze not found").WithCause(err))
		return
	}
	name, _ := doc["name"].(string)
	discard, _ := doc["discard"].(bool)
	cond, err := decodeSnoozeCondition(doc["condition"])
	if err != nil {
		WriteError(w, r, ErrBadRequest.WithMessage("snooze has invalid condition").WithCause(err))
		return
	}
	// Apply to the record collection.
	resp := retroApplyResponse{Snooze: name}
	if discard {
		deleted, err := rt.DB.Delete(ctx, "record", cond, false)
		if err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
		resp.Matched = deleted
		resp.Deleted = deleted
	} else {
		// Whole-query write, so it goes through the same gate the bulk
		// endpoints use (routes_bulk.go guardBulk): one authorization for the
		// mutation as a whole, and an outright refusal for a collection whose
		// write hooks are per-document and cannot be honoured here. Without
		// this, retro-apply was the last ungated query-wide write left in the
		// api package — a hole in a chain every other path enforces.
		//
		// In production this is a pass-through: `record` implements neither
		// WriteGuard nor WriteTransformer, so guardBulk falls to its no-hook
		// case. The nil check keeps the tests that wire no plugin host at all
		// on the historical straight-to-driver path.
		// The attribution field belongs to the snooze plugin
		// (plugins.SuppressionOwner), so ask it for the name rather than
		// repeating the literal here: retro-apply and the pipeline must never
		// be able to disagree about which field means "silenced".
		owner, ok := rt.plugin("snooze").(plugins.SuppressionOwner)
		if !ok {
			WriteError(w, r, ErrInternal.WithMessage("snooze plugin does not own a suppression field"))
			return
		}
		set := db.Document{owner.SuppressionField(): name}
		if p := rt.plugin("record"); p != nil {
			if !rt.guardBulk(w, r, p, set, nil, nil) {
				return
			}
		}
		tagged, err := rt.DB.SetFields(ctx, "record", set, cond)
		if err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
		resp.Matched = tagged
		resp.Tagged = tagged
	}

	// Best-effort: bump the snooze's hit counter so the table reflects this.
	if resp.Matched > 0 {
		_ = bumpSnoozeHits(ctx, rt.DB, uid, int64(resp.Matched))
	}

	WriteJSON(w, http.StatusOK, resp)
}

// decodeSnoozeCondition normalises the wire shape — the field is stored as
// `any` in the DB (mongo, sqlite-jsonb, postgres-jsonb all hand it back as
// either a list or a map). Re-marshal then Unmarshal through condition.Cond
// so the same code path used at boot in the snooze plugin handles it.
func decodeSnoozeCondition(raw any) (condition.Cond, error) {
	if raw == nil {
		return condition.Cond{}, nil
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return condition.Cond{}, err
	}
	var c condition.Cond
	if err := json.Unmarshal(blob, &c); err != nil {
		return condition.Cond{}, err
	}
	return c, nil
}

// bumpSnoozeHits increments the snooze record's `hits` counter by n. We use
// the generic Write path so the increment lands on the same async-writer
// batch as the pipeline's per-match bumps. Failures are non-fatal — the
// caller already returned 200.
func bumpSnoozeHits(ctx context.Context, drv db.Driver, uid string, n int64) error {
	doc, err := drv.GetOne(ctx, "snooze", db.Document{"uid": uid})
	if err != nil {
		return err
	}
	prev, _ := doc["hits"].(int64)
	if prev == 0 {
		if f, ok := doc["hits"].(float64); ok {
			prev = int64(f)
		}
	}
	return drv.UpdateOne(ctx, "snooze", uid, db.Document{"hits": prev + n}, false)
}
