// routes_bulk.go installs three synchronous bulk-mutation endpoints that apply
// a single mutation to every record matching a ?q condition in one HTTP call:
//
//	POST /api/v1/record/bulk_state?q=<cond>   — flip state on all matches
//	POST /api/v1/record/bulk_owner?q=<cond>   — assign / release ownership
//	POST /api/v1/{plugin}/bulk_update?q=<cond> — merge attrs / add+remove tags
//
// They are mounted as siblings to the generic plugin CRUD surface (beside the
// snooze retro-apply endpoint) so MountCRUD's generic /{uid} handlers never
// shadow the more-specific /bulk_* paths. They resolve the query server-side,
// apply the mutation with a single driver bulk call — except where the new
// value depends on each row's own prior value (an ownership clear moves THAT
// row's owner into its previous_owner), which is a per-row read-modify-write —
// emit one audit row per affected uid, and return the matched/updated counts.
//
// Tenant scoping is enforced inside the driver: every SetFields/AppendList/
// RemoveList/Search call fail-closes on a naked (no-tenant) context for a
// scoped collection, so a match-all (empty) ?q is safe — the driver still
// AND-s the tenant predicate. The handler never bypasses rt.DB / rt.Host.DB().
//
// Plugin write authorization on the bulk paths goes through the opt-in
// plugins.BulkWriteGuard hook only (see guardBulk): the per-document hooks
// plugins.WriteTransformer / plugins.WriteGuard are never called here, and a
// collection carrying one of them has its bulk requests refused rather than
// half-checked.
//
//nolint:revive
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/protected"
	"github.com/snoozeweb/snooze/internal/resolutionhold"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// recordCollection is the collection the bulk_state endpoint mutates.
const recordCollection = "record"

// bulkAuditUIDCap bounds how many affected uids the bulk handlers load into
// memory to write one audit row each. Above the cap we emit a SINGLE summary
// audit row carrying the matched count instead of N rows (and skip the
// per-uid pre-search), so a multi-million-row match cannot exhaust memory
// purely for the audit log. The mutation itself is always a single driver
// call regardless of how many rows it touches.
const bulkAuditUIDCap = 1000

// bulkStateAllowed is the set of states a bulk_state call may set. It mirrors
// the comment plugin's stateChanging set ({ack,close,open,esc}) verbatim; the
// values are duplicated here rather than imported so the api package does not
// depend on a concrete plugin implementation.
var bulkStateAllowed = map[string]bool{
	"ack": true, "close": true, "open": true, "esc": true,
}

// bulkStateTakes / bulkStateClears split bulk_state's targets by what they do
// to ownership, mirroring the comment plugin: ack and close make the caller
// the owner, open and esc drop the owner.
var (
	bulkStateTakes  = map[string]bool{"ack": true, "close": true}
	bulkStateClears = map[string]bool{"open": true, "esc": true}
)

// mountBulk wires both bulk endpoints. bulk_state carries a fixed rw_record
// permission, so it sits under a RequirePerm group exactly like retro-apply.
// bulk_update's permission is dynamic (rw_<plugin>), so the handler enforces
// it inline against the caller's claims.
func (rt *Router) mountBulk(r chi.Router) {
	if rt.DB == nil {
		return
	}
	// bulk_state mutates the record collection, so the caller must hold
	// rw_record (or the rw_all admin wildcard) — read-only auditors cannot
	// flip state on a whole query.
	r.Group(func(g chi.Router) {
		g.Use(middleware.RequirePerm("rw_" + recordCollection))
		g.Post("/api/v1/record/bulk_state", rt.handleBulkState)
		// bulk_owner rewrites the same collection's ownership keys, so it
		// carries the same fixed permission.
		g.Post("/api/v1/record/bulk_owner", rt.handleBulkOwner)
	})
	// bulk_update gates on rw_<plugin>, which is only known once the
	// collection is resolved, so the permission check lives in the handler.
	//
	// The route is registered STATICALLY, once per plugin, rather than only as
	// the parameterised /api/v1/{plugin}/bulk_update. chi resolves a static
	// path segment before a parameter sibling, so `/api/v1/record/...` descends
	// into the record plugin's CRUD mount and never backtracks to `{plugin}`;
	// the mount has no /bulk_update, so the parameterised form alone answered
	// 405 for EVERY collection (bulk_state works precisely because its path is
	// static). One static registration per plugin puts the route inside the
	// trie node chi actually reaches.
	//
	// `tenant` is excluded for the same reason the CRUD loop in router.go
	// excludes it: the registry is mounted separately behind
	// RequirePlatformPerm (literal permission + platform-tenant origin), and
	// bulk_update's own gate is auth.HasPermission(rw_tenant), which the rw_all
	// wildcard satisfies. Mounting it here would be a way around the platform
	// gate, not a convenience.
	for name := range rt.Plugins {
		if name == auth.TenantCollection {
			continue
		}
		r.Post("/api/v1/"+name+"/bulk_update", rt.bulkUpdateFor(name))
	}
	// Kept for collections with no plugin CRUD mount of their own, where
	// nothing shadows the parameterised form.
	r.Post("/api/v1/{plugin}/bulk_update", rt.handleBulkUpdate)
}

// bulkUpdateFor binds the collection name for the statically-mounted form of
// the route, where there is no {plugin} URL parameter to read.
func (rt *Router) bulkUpdateFor(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt.bulkUpdate(w, r, name)
	}
}

// bulkStateRequest is the bulk_state body. message is recorded once in the
// audit summary, not fanned out as a per-record comment (see handleBulkState).
type bulkStateRequest struct {
	State   string `json:"state"`
	Message string `json:"message"`
	// Source is the optional tool tag (see the comment `source` field),
	// recorded in the audit summary.
	Source string `json:"source"`
}

// bulkStateResponse is the JSON envelope bulk_state returns.
type bulkStateResponse struct {
	Matched int    `json:"matched"`
	Updated int    `json:"updated"`
	State   string `json:"state"`
}

// bulkUpdateRequest is the bulk_update body. At least one of set/tag/untag
// must be present.
type bulkUpdateRequest struct {
	Set   db.Document `json:"set"`
	Tag   []string    `json:"tag"`
	Untag []string    `json:"untag"`
}

// bulkUpdateResponse is the JSON envelope bulk_update returns: one count per op.
type bulkUpdateResponse struct {
	Matched  int `json:"matched"`
	Set      int `json:"set"`
	Tagged   int `json:"tagged"`
	Untagged int `json:"untagged"`
}

// handleBulkState flips state ∈ {ack,close,open,esc} on every record matching
// ?q in a single SetFields call.
//
// Ownership follows the single-record path: an ack or close stamps the caller
// as owner in the same SetFields; an open or esc first clears the owner of
// every owned match, one row at a time (see clearOwners), BEFORE the state
// flip — ?q is written against the pre-transition rows ("state = ack"), which
// the flip would stop matching.
//
// Behavioural difference from the single-record path: the per-record comment
// endpoint writes a `comment` row AND patches state. The bulk path sets state
// directly and does NOT fan out one comment per record (a query could match
// thousands of rows); the request `message` is recorded once in the audit
// summary instead.
func (rt *Router) handleBulkState(w http.ResponseWriter, r *http.Request) {
	var req bulkStateRequest
	if err := ParseJSONBody(r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	if !bulkStateAllowed[req.State] {
		WriteError(w, r, ErrBadRequest.WithMessage("state must be one of ack, close, open, esc"))
		return
	}
	cond, err := decodeQueryCond(r)
	if err != nil {
		WriteError(w, r, ErrBadRequest.WithMessage("bad q: "+err.Error()))
		return
	}
	ctx := r.Context()

	// bulk_state writes {"state": …} to the record collection, so it goes
	// through the same bulk authorization gate as bulk_update. A nil plugin —
	// the registry is not wired in some tests — keeps the historical behaviour:
	// straight to the driver, no hook. `record` implements no write hook at
	// all, so in production this is a pass-through.
	set := db.Document{"state": req.State}
	claims, hasClaims := auth.ClaimsFrom(ctx)
	human := hasClaims && claims.Subject != ""
	if human && bulkStateTakes[req.State] {
		for k, v := range ownership.Take(claims.Subject, claims.Method, time.Now().Unix()) {
			set[k] = v
		}
	}
	// The resolution hold follows the single-record close / open / esc: a
	// human close arms it, a re-open or escalation ends it
	// (internal/resolutionhold).
	switch {
	case req.State == "close" && human:
		for k, v := range resolutionhold.Start(time.Now().Unix(), rt.resolutionHold(ctx)) {
			set[k] = v
		}
	case req.State == "open" || req.State == "esc":
		for k, v := range resolutionhold.Clear() {
			set[k] = v
		}
	}
	if p := rt.plugin(recordCollection); p != nil {
		if !rt.guardBulk(w, r, p, set, nil, nil) {
			return
		}
	}

	if bulkStateClears[req.State] {
		if _, err := rt.clearOwners(ctx, cond); err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
	}

	matched, err := rt.DB.SetFields(ctx, recordCollection, set, cond)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}

	summary := req.State + sourceTag(req.Source)
	if req.Message != "" {
		summary += ": " + req.Message
	}
	rt.auditBulk(ctx, recordCollection, "bulk_state", cond, matched, summary)

	WriteJSON(w, http.StatusOK, bulkStateResponse{
		Matched: matched,
		Updated: matched,
		State:   req.State,
	})
}

// clearOwners drops the owner of every owned record matching cond, each row's
// own owner becoming its previous_owner (ownership.Clear is computed per row,
// so this cannot be one SetFields). Unowned matches are not written at all: a
// clear would only rewrite their empties and keeps their ghost anyway. Returns
// the uids it cleared.
func (rt *Router) clearOwners(ctx context.Context, cond condition.Cond) ([]string, error) {
	rows, _, err := rt.DB.Search(ctx, recordCollection, andCond(cond, ownership.OwnedCond()), db.Page{})
	if err != nil {
		return nil, err
	}
	uids := make([]string, 0, len(rows))
	for _, row := range rows {
		uid, _ := row["uid"].(string)
		if uid == "" {
			continue
		}
		if err := rt.DB.UpdateOne(ctx, recordCollection, uid, ownership.Clear(row), false); err != nil {
			return uids, err
		}
		uids = append(uids, uid)
	}
	return uids, nil
}

// bulkOwnerRequest is the bulk_owner body. assignee (and optionally
// assignee_method) is required for assign; message goes into the audit
// summary, like bulk_state's.
type bulkOwnerRequest struct {
	Action         string `json:"action"`
	Assignee       string `json:"assignee"`
	AssigneeMethod string `json:"assignee_method"`
	Message        string `json:"message"`
	// Source is the optional tool tag, recorded in the audit summary.
	Source string `json:"source"`
}

// bulkOwnerResponse is the JSON envelope bulk_owner returns. matched counts
// every record ?q selects; updated only those whose ownership changed (closed
// records are skipped by assign, unowned ones by release).
type bulkOwnerResponse struct {
	Matched int    `json:"matched"`
	Updated int    `json:"updated"`
	Action  string `json:"action"`
}

// handleBulkOwner applies an ownership action to every record matching ?q —
// the bulk twin of the comment plugin's `assign` / `release` types, with the
// same rules:
//
//   - assign makes `assignee` the owner of every match that is not closed, in
//     one SetFields. The assignee must be an enabled user of the caller's
//     tenant (the user lookup runs under the request context); an unknown,
//     disabled or — without assignee_method — ambiguous login is a 422 and
//     nothing is written.
//   - release clears the owner of every OWNED match, per row. An
//     acknowledged one also returns to `open` on the same timers as a manual
//     open, with `acked_by` removed (ownership.Release is the one source of
//     truth for both paths).
//
// Neither writes a comment nor re-notifies. The audit trail gets one row per
// record whose ownership changed.
func (rt *Router) handleBulkOwner(w http.ResponseWriter, r *http.Request) {
	var req bulkOwnerRequest
	if err := ParseJSONBody(r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	if req.Action != "assign" && req.Action != "release" {
		WriteError(w, r, ErrBadRequest.WithMessage("action must be one of assign, release"))
		return
	}
	if req.Action == "assign" && req.Assignee == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("assign requires an assignee"))
		return
	}
	cond, err := decodeQueryCond(r)
	if err != nil {
		WriteError(w, r, ErrBadRequest.WithMessage("bad q: "+err.Error()))
		return
	}
	ctx := r.Context()
	now := time.Now().Unix()

	// Resolve the assignee before anything else is read or written.
	var set db.Document
	if req.Action == "assign" {
		users, _, err := rt.DB.Search(ctx, "user", ownership.UserCond(req.Assignee), db.Page{})
		if err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
		method, err := ownership.MatchAssignee(users, req.Assignee, req.AssigneeMethod)
		if err != nil {
			WriteError(w, r, ErrValidation.WithMessage(err.Error()))
			return
		}
		set = ownership.Take(req.Assignee, method, now)
	} else {
		// The shape release writes (the values differ per row), for the
		// bulk authorization hook.
		set = ownership.Clear(map[string]any{ownership.FieldOwner: "x"})
		set["state"] = "open"
	}
	if p := rt.plugin(recordCollection); p != nil {
		if !rt.guardBulk(w, r, p, set, nil, nil) {
			return
		}
	}

	_, matched, err := rt.DB.Search(ctx, recordCollection, cond, db.Page{PerPage: 1})
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}

	summary := req.Action
	if req.Action == "assign" {
		summary += " " + req.Assignee
	}
	summary += sourceTag(req.Source)
	if req.Action == "assign" {
		open := andCond(cond, condition.Not(condition.Equals("state", "close")))
		// assign leaves state alone, so the same condition still selects the
		// assigned rows after the write, which is what auditBulk re-queries.
		n, err := rt.DB.SetFields(ctx, recordCollection, set, open)
		if err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
		if req.Message != "" {
			summary += ": " + req.Message
		}
		rt.auditBulk(ctx, recordCollection, "bulk_owner", open, n, summary)
		WriteJSON(w, http.StatusOK, bulkOwnerResponse{Matched: matched, Updated: n, Action: req.Action})
		return
	}

	uids, err := rt.releaseOwners(ctx, cond, now)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if req.Message != "" {
		summary += ": " + req.Message
	}
	// A release changes what ?q ∧ owned selects, so the audit names the
	// released uids directly rather than re-running a query.
	rt.auditBulkUIDs(ctx, recordCollection, "bulk_owner", uids, summary)
	WriteJSON(w, http.StatusOK, bulkOwnerResponse{Matched: matched, Updated: len(uids), Action: req.Action})
}

// releaseOwners releases every owned record matching cond, per row, with the
// same patch as the comment plugin's `release` (ownership.Release): clear,
// plus a return to `open` for an acknowledged record, whose `acked_by` is then
// removed the way a manual open removes it. Returns the released uids.
func (rt *Router) releaseOwners(ctx context.Context, cond condition.Cond, now int64) ([]string, error) {
	rows, _, err := rt.DB.Search(ctx, recordCollection, andCond(cond, ownership.OwnedCond()), db.Page{})
	if err != nil {
		return nil, err
	}
	escalateAfter := rt.escalateAfter(ctx)
	uids := make([]string, 0, len(rows))
	for _, row := range rows {
		uid, _ := row["uid"].(string)
		if uid == "" {
			continue
		}
		patch, reopened := ownership.Release(row, now, escalateAfter)
		if err := rt.DB.UpdateOne(ctx, recordCollection, uid, patch, false); err != nil {
			return uids, err
		}
		if reopened {
			if _, err := rt.DB.UnsetFields(ctx, recordCollection, []string{"acked_by"},
				condition.Equals("uid", uid)); err != nil {
				return uids, err
			}
		}
		uids = append(uids, uid)
	}
	return uids, nil
}

// escalateAfter resolves the live escalate_after window a release re-arms on
// a record it returns to open, with the comment plugin's preference order:
// the tenant-aware runtime settings when the host exposes them, then the
// file-config baseline, then the schema default.
func (rt *Router) escalateAfter(ctx context.Context) time.Duration {
	if rsh, ok := rt.Host.(plugins.RuntimeSettingsHost); ok {
		if rs := rsh.RuntimeSettings(); rs != nil {
			if hk, err := rs.Housekeeper(ctx); err == nil {
				return hk.EscalateAfter.AsDuration()
			}
		}
	}
	if rt.Config != nil {
		return rt.Config.Housekeeper.EscalateAfter.AsDuration()
	}
	return schema.DefaultHousekeeper().EscalateAfter.AsDuration()
}

// sourceTag renders an optional tool tag for an audit summary: " [snooze-skill]",
// or "" when none was given. Clamped like the comment `source` field.
func sourceTag(src string) string {
	src = strings.TrimSpace(strings.ReplaceAll(src, "\x00", ""))
	if src == "" {
		return ""
	}
	if r := []rune(src); len(r) > snoozetypes.MaxSourceLen {
		src = string(r[:snoozetypes.MaxSourceLen])
	}
	return " [" + src + "]"
}

// resolutionHold resolves the live housekeeping.resolution_hold window with the
// same preference order as escalateAfter. Zero is honoured: it disables the hold.
func (rt *Router) resolutionHold(ctx context.Context) time.Duration {
	if rsh, ok := rt.Host.(plugins.RuntimeSettingsHost); ok {
		if rs := rsh.RuntimeSettings(); rs != nil {
			return rs.ResolutionHold(ctx)
		}
	}
	if rt.Config != nil {
		return rt.Config.Housekeeper.ResolutionHold.AsDuration()
	}
	return schema.DefaultResolutionHold
}

// andCond conjoins a (possibly match-all) ?q condition with an extra
// predicate, leaving out the empty AlwaysTrue node rather than nesting it.
func andCond(q, extra condition.Cond) condition.Cond {
	if q.IsZero() {
		return extra
	}
	return condition.And(q, extra)
}

// handleBulkUpdate applies set (attribute merge), tag (idempotent add), and
// untag (remove) to every record matching ?q on the {plugin} collection. The
// plugin must be a registered plugins.DataModel (rejecting audit, notifiers,
// etc.); the caller must hold rw_<plugin>.
func (rt *Router) handleBulkUpdate(w http.ResponseWriter, r *http.Request) {
	rt.bulkUpdate(w, r, chi.URLParam(r, "plugin"))
}

// bulkUpdate is the shared body of both mounted forms of the endpoint.
func (rt *Router) bulkUpdate(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("missing plugin"))
		return
	}
	// Dynamic permission gate: rw_<plugin> (rw_all satisfies it). Missing
	// claims → 401, mismatch → 403 — mirroring middleware.RequirePerm.
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		WriteError(w, r, ErrUnauthorized)
		return
	}
	if !auth.HasPermission(claims, "rw_"+name) {
		WriteError(w, r, ErrForbidden)
		return
	}

	// Only DataModel collections are mutable. A nil plugin (unknown name) or a
	// non-DataModel plugin (audit, notifiers, …) is rejected.
	p := rt.plugin(name)
	dm, ok := p.(plugins.DataModel)
	if p == nil || !ok {
		WriteError(w, r, ErrNotFound.WithMessage("unknown or non-mutable plugin: "+name))
		return
	}
	meta := dm.Metadata()
	meta.PluginName = name

	var req bulkUpdateRequest
	if err := ParseJSONBody(r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	if len(req.Set) == 0 && len(req.Tag) == 0 && len(req.Untag) == 0 {
		WriteError(w, r, ErrBadRequest.WithMessage("at least one of set, tag, untag is required"))
		return
	}
	cond, err := decodeQueryCond(r)
	if err != nil {
		WriteError(w, r, ErrBadRequest.WithMessage("bad q: "+err.Error()))
		return
	}
	ctx := r.Context()

	// A protected field is never writable through a bulk set: the whole point
	// of the concept is that one schema-validating endpoint owns those keys,
	// and a query-wide SetFields would rewrite them on every matching record
	// at once. Refused loudly, exactly as the single-document CRUD path does.
	if names := protected.Names(req.Set); len(names) > 0 {
		WriteError(w, r, ErrForbidden.WithMessage(
			"protected field(s) "+strings.Join(names, ", ")+
				" cannot be written through bulk_update; use their dedicated endpoint"))
		return
	}

	// Authorize the request as a whole BEFORE any driver call, so bulk_update
	// is not a hole in the chain the generic CRUD handlers enforce.
	if !rt.guardBulk(w, r, p, req.Set, req.Tag, req.Untag) {
		return
	}

	var resp bulkUpdateResponse
	if len(req.Set) > 0 {
		n, serr := rt.DB.SetFields(ctx, name, req.Set, cond)
		if serr != nil {
			WriteError(w, r, ErrInternal.WithCause(serr))
			return
		}
		resp.Set = n
		resp.Matched = n
	}
	if len(req.Tag) > 0 {
		vals := toAnySlice(req.Tag)
		// AppendList appends unconditionally, so a record already carrying the
		// tag would end with a duplicate. Remove-then-append makes the add
		// idempotent: a record that already had the tag ends with exactly one.
		if _, rerr := rt.DB.RemoveList(ctx, name, map[string][]any{"tags": vals}, cond); rerr != nil {
			WriteError(w, r, ErrInternal.WithCause(rerr))
			return
		}
		n, aerr := rt.DB.AppendList(ctx, name, map[string][]any{"tags": vals}, cond)
		if aerr != nil {
			WriteError(w, r, ErrInternal.WithCause(aerr))
			return
		}
		resp.Tagged = n
		if n > resp.Matched {
			resp.Matched = n
		}
	}
	if len(req.Untag) > 0 {
		n, rerr := rt.DB.RemoveList(ctx, name, map[string][]any{"tags": toAnySlice(req.Untag)}, cond)
		if rerr != nil {
			WriteError(w, r, ErrInternal.WithCause(rerr))
			return
		}
		resp.Untagged = n
		if n > resp.Matched {
			resp.Matched = n
		}
	}

	rt.auditBulk(ctx, name, "bulk_update", cond, resp.Matched, bulkUpdateSummary(req))

	WriteJSON(w, http.StatusOK, resp)
}

// guardBulk authorizes one bulk mutation against p. It reports whether the
// caller may proceed; when it returns false the response has already been
// written. Three cases, in order:
//
//  1. p implements plugins.BulkWriteGuard — it has opted its collection into
//     bulk writes and owns the semantics. Call it once, before any driver call;
//     an error is a 403.
//  2. p implements plugins.WriteGuard and/or plugins.WriteTransformer but NOT
//     BulkWriteGuard — refuse the whole request with 403. Those hooks are
//     per-document and cannot be honoured by a query-wide mutation (see 3
//     below); running them anyway is worse than refusing, so the collection
//     stays non-bulk-writable until it opts in.
//  3. p implements none of them — proceed with no hook, which is exactly the
//     behaviour these endpoints always had. `record`, the only collection the
//     web UI bulk-mutates, is this case.
//
// Why case 2 refuses instead of adapting the per-document hooks:
//
//   - TransformWrite would run once on the shared `set` document, so the
//     identity fields some transforms stamp (savedsearch's `owner`, comment's
//     `user`/`method`, taken from the CALLER's claims) would be written onto
//     every matched row — a bulk edit that quietly reassigns other people's
//     rows to whoever ran it.
//   - GuardWrite takes a uid and most implementations authorize against that
//     row's prior state. A bulk call never loads the rows, so the best it could
//     pass is "": apikey's guard rejects "" unconditionally (every apikey
//     bulk_update becomes a 403 about how keys are created), and user's
//     last-admin/platform_admin protection, role's reserved-role protection and
//     comment's state-transition rules all silently become no-ops — coverage
//     that looks enforced and is not.
//
// DataModel.Validate is likewise NOT invoked: a bulk body is a partial field
// merge plus two tag lists, never a whole document, and the collection schemas
// describe whole documents.
func (rt *Router) guardBulk(w http.ResponseWriter, r *http.Request,
	p plugins.Plugin, set db.Document, tag, untag []string,
) bool {
	if g, ok := p.(plugins.BulkWriteGuard); ok {
		if err := g.GuardBulkWrite(r.Context(), set, tag, untag); err != nil {
			WriteError(w, r, ErrForbidden.WithMessage(err.Error()))
			return false
		}
		return true
	}
	_, guards := p.(plugins.WriteGuard)
	_, transforms := p.(plugins.WriteTransformer)
	if guards || transforms {
		WriteError(w, r, ErrForbidden.WithMessage(
			"collection does not support bulk writes; its write hooks are per-document"))
		return false
	}
	return true
}

// auditBulk records the audit trail for a bulk mutation. It looks up the
// plugin's Metadata so EmitBulkAudit honours the per-collection audit:false
// opt-out, then collects the affected uids (one row each) when the matched
// count is under bulkAuditUIDCap, or emits a single summary row carrying the
// count above the cap. It is a no-op when no plugin host is wired (some tests)
// or when nothing matched.
//
// log: above bulkAuditUIDCap affected rows the per-uid pre-search is skipped
// and a single summary audit row is written instead, to bound memory.
func (rt *Router) auditBulk(ctx context.Context, collection, action string, cond condition.Cond, matched int, summary string) {
	if rt.Host == nil || matched == 0 {
		return
	}
	p := rt.plugin(collection)
	if p == nil {
		return
	}
	meta := p.Metadata()
	meta.PluginName = collection
	if !meta.Audit {
		return // collection opted out — skip the pre-search entirely.
	}

	if matched > bulkAuditUIDCap {
		// Too many matches to audit per-uid; one summary row with the count.
		plugins.EmitBulkAudit(ctx, rt.Host, meta, collection, action,
			[]string{collection + ":bulk"},
			summary+" (bulk: matched count exceeds audit cap)")
		return
	}

	docs, _, err := rt.DB.Search(ctx, collection, cond, db.Page{PerPage: 0})
	if err != nil {
		if rt.Logger != nil {
			rt.Logger.Warn("bulk: pre-audit search failed",
				"collection", collection, "action", action, "err", err)
		}
		return
	}
	uids := make([]string, 0, len(docs))
	for _, d := range docs {
		if u, ok := d["uid"].(string); ok && u != "" {
			uids = append(uids, u)
		}
	}
	plugins.EmitBulkAudit(ctx, rt.Host, meta, collection, action, uids, summary)
}

// auditBulkUIDs is auditBulk for a mutation that already knows exactly which
// rows it changed (the per-row paths), so no query is re-run: one audit row
// per uid, or a single summary row above bulkAuditUIDCap.
func (rt *Router) auditBulkUIDs(ctx context.Context, collection, action string, uids []string, summary string) {
	if rt.Host == nil || len(uids) == 0 {
		return
	}
	p := rt.plugin(collection)
	if p == nil {
		return
	}
	meta := p.Metadata()
	meta.PluginName = collection
	if len(uids) > bulkAuditUIDCap {
		uids = []string{collection + ":bulk"}
		summary += " (bulk: matched count exceeds audit cap)"
	}
	plugins.EmitBulkAudit(ctx, rt.Host, meta, collection, action, uids, summary)
}

// plugin resolves a registered plugin by name, preferring the Host registry
// (the production source) and falling back to the rt.Plugins map.
func (rt *Router) plugin(name string) plugins.Plugin {
	if rt.Host != nil {
		if p := rt.Host.Plugin(name); p != nil {
			return p
		}
	}
	return rt.Plugins[name]
}

// bulkUpdateSummary builds a short, value-free audit summary describing which
// ops a bulk_update ran (e.g. "set; tag; untag").
func bulkUpdateSummary(req bulkUpdateRequest) string {
	parts := make([]string, 0, 3)
	if len(req.Set) > 0 {
		parts = append(parts, "set")
	}
	if len(req.Tag) > 0 {
		parts = append(parts, "tag")
	}
	if len(req.Untag) > 0 {
		parts = append(parts, "untag")
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}

// toAnySlice converts a []string to a []any for the AppendList/RemoveList
// map[string][]any field shape.
func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// decodeQueryCond decodes the ?q parameter into a condition.Cond using the same
// base64url-then-JSON logic as plugins.decodeListParams. An empty q yields the
// match-all condition.Cond{} — safe because the driver still AND-s tenant_id.
func decodeQueryCond(r *http.Request) (condition.Cond, error) {
	cond := condition.Cond{}
	encoded := r.URL.Query().Get("q")
	if encoded == "" {
		return cond, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		// Tolerate plain (padded) base64 too.
		raw, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return cond, err
		}
	}
	if err := json.Unmarshal(raw, &cond); err != nil {
		return cond, err
	}
	return cond, nil
}
