// routes_agentic.go installs the only sanctioned writer of a record's
// protected `agentic` subtree — the AI-authored root-cause / remediation
// analysis:
//
//	GET    /api/v1/record/{uid}/agentic   — read the analysis
//	PUT    /api/v1/record/{uid}/agentic   — store (replace) it, schema-checked
//	DELETE /api/v1/record/{uid}/agentic   — drop it
//
// Like the bulk and retro-apply endpoints, these are mounted as siblings of
// the generic plugin CRUD surface and BEFORE it, so the record plugin's
// generic /{uid} handlers never shadow the sub-path.
//
// # Why an endpoint instead of a PATCH
//
// The generic CRUD layer accepts any well-formed map on a record (the record
// plugin validates nothing, deliberately, so the pipeline can write partial
// documents). That is the wrong contract for an analysis: a consumer branches
// on `confidence` and `risk`, so a typo'd enum or a missing step list has to
// fail loudly at write time rather than surface as a silently-absent key at
// read time. This handler is the schema gate, and `agentic` being a protected
// field (internal/protected) is what stops every other write path from going
// around it.
//
// # Auth
//
// Reads follow the collection: ro_record (or rw_record, or the rw_all
// wildcard). Writes require the LITERAL protected.WritePermission — rw_all
// does not grant it, so an existing admin role cannot forge or wipe an
// analysis without being explicitly entrusted with it.
//
//nolint:revive
package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/protected"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// agenticMaxBody caps the request body so a runaway agent cannot stream
// megabytes into the decoder before validation rejects it. It is NOT a second
// schema: a payload the schema accepts must never be refused here, or the
// caller gets a size error with no field path to fix — something the CLI's
// local validation cannot predict and the docs promise cannot happen.
//
// The cap therefore has to clear the largest schema-valid payload. The maxima
// in pkg/snoozetypes count CHARACTERS (runes), and they sum to:
//
//	MaxSummaryLen                            500
//	MaxScopeLen                              200
//	MaxEvidenceItems * MaxEvidenceLen      5,000
//	MaxSteps    * (MaxActionLen+MaxCommandLen)  30,000
//	MaxSteps    * … again, for Rollback     30,000
//	MaxSourceLen                              64
//	                                      -------
//	                                      65,764 characters
//
// In bytes that is far more: a rune costs up to 4 bytes in UTF-8, and a
// control character costs 6 as a JSON \u escape — so the worst case is
// 65,764 * 6 ≈ 386 KiB of string content, before the keys, quotes, braces and
// whitespace of the enclosing JSON. 64 KiB did not even cover the ASCII case
// (67,542 bytes), which is how this was found.
//
// 512 KiB clears the 6-bytes-per-character worst case with room for the
// envelope, and is still three orders of magnitude below "megabytes".
const agenticMaxBody = 512 << 10

// agenticField is the protected record field this endpoint owns.
const agenticField = protected.AgenticField

func (rt *Router) mountAgentic(r chi.Router) {
	if rt.DB == nil {
		return
	}
	// Reading an analysis is an ordinary record read: the two record grants
	// are enough. The read catch-all `ro_all` needs no entry of its own —
	// auth.HasPermission treats it as covering any `ro_*` want, the same way
	// the plugin CRUD authorizer does, so a read-only auditor who can list the
	// record can also read its analysis.
	r.Group(func(g chi.Router) {
		g.Use(middleware.RequirePerm("ro_"+recordCollection, "rw_"+recordCollection))
		g.Get("/api/v1/record/{uid}/agentic", rt.handleAgenticGet)
	})
	// Writing one requires the literal protected-write permission.
	r.Group(func(g chi.Router) {
		g.Use(middleware.RequireLiteralPerm(protected.WritePermission))
		g.Put("/api/v1/record/{uid}/agentic", rt.handleAgenticPut)
		g.Delete("/api/v1/record/{uid}/agentic", rt.handleAgenticDelete)
	})
}

// agenticResponse is what PUT and GET return: the stored subtree, so a client
// sees the provenance the server stamped without a second round-trip.
type agenticResponse struct {
	UID     string         `json:"uid"`
	Agentic map[string]any `json:"agentic"`
}

func (rt *Router) handleAgenticGet(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("missing record uid"))
		return
	}
	doc, err := rt.DB.GetOne(r.Context(), recordCollection, db.Document{"uid": uid})
	if err != nil {
		rt.writeRecordLookupError(w, r, uid, err)
		return
	}
	sub, ok := doc[agenticField].(map[string]any)
	if !ok {
		WriteError(w, r, ErrNotFound.WithMessage("record "+uid+" carries no agentic analysis"))
		return
	}
	WriteJSON(w, http.StatusOK, agenticResponse{UID: uid, Agentic: sub})
}

func (rt *Router) handleAgenticPut(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("missing record uid"))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, agenticMaxBody+1))
	if err != nil {
		WriteError(w, r, ErrBadRequest.WithMessage("read body").WithCause(err))
		return
	}
	if len(raw) > agenticMaxBody {
		WriteError(w, r, ErrBadRequest.WithMessage(
			fmt.Sprintf("body exceeds %d bytes", agenticMaxBody)))
		return
	}
	if len(raw) == 0 {
		WriteError(w, r, ErrBadRequest.WithMessage("empty body"))
		return
	}

	req, err := snoozetypes.DecodeAgenticRequest(raw)
	if err != nil {
		var verrs snoozetypes.ValidationErrors
		if errors.As(err, &verrs) {
			// One 422 listing every offending path, so the caller fixes the
			// whole payload in a single retry.
			WriteError(w, r, ErrValidation.
				WithMessage("agentic payload failed schema validation").
				WithDetails(verrs.Details()))
			return
		}
		WriteError(w, r, ErrBadRequest.WithMessage("malformed JSON body").WithCause(err))
		return
	}

	// The record must already exist: SetFields on a missing uid is a silent
	// no-op on every backend, and an agent that typo'd a uid deserves a 404
	// rather than a 200 it will never be able to read back.
	if _, err := rt.DB.GetOne(r.Context(), recordCollection, db.Document{"uid": uid}); err != nil {
		rt.writeRecordLookupError(w, r, uid, err)
		return
	}

	subject := ""
	if claims, ok := auth.ClaimsFrom(r.Context()); ok {
		subject = claims.Subject
	}
	sub, err := req.ToAgentic(subject, time.Now()).Document()
	if err != nil {
		WriteError(w, r, ErrInternal.WithMessage("encode agentic document").WithCause(err))
		return
	}

	// SetFields, not UpdateOne: a merge write would stamp date_epoch and make
	// a months-old alert look freshly seen just because someone analysed it.
	// $set on the one key also gives replace (not deep-merge) semantics, so a
	// re-analysis with fewer evidence items cannot leave the old ones behind.
	matched, err := rt.DB.SetFields(r.Context(), recordCollection,
		db.Document{agenticField: sub}, condition.Equals("uid", uid))
	if err != nil {
		WriteError(w, r, ErrInternal.WithMessage("store agentic analysis").WithCause(err))
		return
	}
	if matched == 0 {
		WriteError(w, r, ErrNotFound.WithMessage("record not found: "+uid))
		return
	}
	rt.auditAgentic(r, uid, "agentic_set", "set agentic analysis (confidence "+req.RootCause.Confidence+")")
	WriteJSON(w, http.StatusOK, agenticResponse{UID: uid, Agentic: sub})
}

func (rt *Router) handleAgenticDelete(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("missing record uid"))
		return
	}
	// Look the record up first so "no such record" and "record carries no
	// analysis" are distinguishable: UnsetFields reports 0 for both.
	doc, err := rt.DB.GetOne(r.Context(), recordCollection, db.Document{"uid": uid})
	if err != nil {
		rt.writeRecordLookupError(w, r, uid, err)
		return
	}
	if _, ok := doc[agenticField]; !ok {
		WriteError(w, r, ErrNotFound.WithMessage("record "+uid+" carries no agentic analysis"))
		return
	}
	if _, err := rt.DB.UnsetFields(r.Context(), recordCollection,
		[]string{agenticField}, condition.Equals("uid", uid)); err != nil {
		WriteError(w, r, ErrInternal.WithMessage("clear agentic analysis").WithCause(err))
		return
	}
	rt.auditAgentic(r, uid, "agentic_clear", "cleared agentic analysis")
	w.WriteHeader(http.StatusNoContent)
}

// writeRecordLookupError maps a GetOne failure onto the canonical envelope: a
// miss is a 404 naming the uid, anything else is a 500.
func (rt *Router) writeRecordLookupError(w http.ResponseWriter, r *http.Request, uid string, err error) {
	if errors.Is(err, db.ErrNotFound) {
		WriteError(w, r, ErrNotFound.WithMessage("record not found: "+uid))
		return
	}
	WriteError(w, r, ErrInternal.WithMessage("look up record "+uid).WithCause(err))
}

// auditAgentic emits one audit row for the mutation, reusing the same
// emitter the CRUD and bulk paths use so an analysis write is visible in the
// audit trail next to every other change to the record.
func (rt *Router) auditAgentic(r *http.Request, uid, action, summary string) {
	if rt.Host == nil {
		return
	}
	p := rt.plugin(recordCollection)
	if p == nil {
		return
	}
	meta := p.Metadata()
	meta.PluginName = recordCollection
	plugins.EmitBulkAudit(r.Context(), rt.Host, meta, recordCollection, action, []string{uid}, summary)
}
