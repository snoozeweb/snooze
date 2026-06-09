package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// auditCollection is the collection name the audit plugin owns. We avoid
// emitting audit events for writes to this collection (infinite-recursion
// guard) even though normal callers wouldn't trigger that.
const auditCollection = "audit"

// emitAudit best-effort records a single audit event per affected uid. It
// silently no-ops when the plugin opts out via metadata.audit:false, when
// the target collection is the audit collection itself, or when the
// underlying Write fails — auditing must never block the mutation it
// describes.
//
// The schema mirrors internal/pluginimpl/audit/plugin.go: object_type,
// object_id, action, username, method, summary, date_epoch.
func emitAudit(ctx context.Context, host Host, meta Metadata, collection, action string, uids []string, summary string) {
	if !meta.Audit || collection == auditCollection || len(uids) == 0 {
		return
	}
	username := ""
	authMethod := ""
	if claims, ok := auth.ClaimsFrom(ctx); ok {
		username = claims.Subject
		authMethod = claims.Method
	}
	now := float64(time.Now().Unix())
	docs := make([]db.Document, 0, len(uids))
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		docs = append(docs, db.Document{
			"object_type": collection,
			"object_id":   uid,
			"action":      action,
			"username":    username,
			"method":      authMethod,
			"summary":     summary,
			"date_epoch":  now,
		})
	}
	if len(docs) == 0 {
		return
	}
	if _, err := host.DB().Write(ctx, auditCollection, docs, db.WriteOptions{UpdateTime: false}); err != nil {
		host.Logger().Warn("plugins: audit emit failed",
			"collection", collection,
			"action", action,
			"err", err)
	}
}

// MountCRUD installs the standard REST surface for a plugin's collection at
// /api/v1/{plugin}. The collection name is taken from p.Name().
//
// If p implements RouteProvider, MountCRUD delegates entirely to its
// RegisterRoutes hook and the generic handlers are not installed. This is the
// escape hatch for plugins whose URL shape cannot be expressed by the
// canonical CRUD model (webhook receivers, login, /health, …).
//
// If p implements DataModel, its Validate method is called on the body of
// POST/PUT/PATCH requests before any database write.
//
// Every CRUD subrouter (and RouteProvider subrouter too) is wrapped with
// AuthorizeCRUD(meta) so the plugin's authorization_policy is enforced
// against the caller's claims. The auth middleware higher up is in charge
// of deciding whether to skip the Bearer check; this layer is what enforces
// the read/write split per plugin.
func MountCRUD(r chi.Router, host Host, p Plugin) {
	// Stamp PluginName on a local copy so AuthorizeCRUD can derive the
	// implicit `ro_<plugin>` / `rw_<plugin>` permissions without each
	// plugin having to remember to set it.
	meta := p.Metadata()
	meta.PluginName = p.Name()
	authorize := AuthorizeCRUD(meta)

	if rp, ok := p.(RouteProvider); ok {
		r.Route("/api/v1/"+p.Name(), func(sub chi.Router) {
			sub.Use(authorize)
			rp.RegisterRoutes(sub, host)
		})
		return
	}

	// Pure Notifiers (mail / webhook / googlechat / mattermost), Actioners
	// (script / patlite), and WebhookReceivers (alertmanager / grafana /
	// prometheus / kapacitor / influxdb2) don't own a document
	// collection — they're behaviour, not data. Their config lives in the
	// shared `action` collection (Notifiers/Actioners) or is stateless
	// ingestion (WebhookReceivers). Mounting generic CRUD on them
	// produces dead endpoints AND conflicts with sibling mounts: the
	// "webhook" Notifier would otherwise grab /api/v1/webhook before the
	// WebhookReceiver mount registers /api/v1/webhook/<receiver> there.
	// Skip the CRUD surface for them; routes that need a custom shape
	// can still opt in via RouteProvider above. Plugins that are both a
	// DataModel AND one of these behaviour interfaces keep their CRUD
	// (the DataModel check below short-circuits the skip).
	if _, isData := p.(DataModel); !isData {
		if _, isNotifier := p.(Notifier); isNotifier {
			return
		}
		if _, isAction := p.(Actioner); isAction {
			return
		}
		if _, isWebhook := p.(WebhookReceiver); isWebhook {
			return
		}
	}

	collection := p.Name()
	r.Route("/api/v1/"+collection, func(sub chi.Router) {
		sub.Use(authorize)
		sub.Get("/", listHandler(host, p, collection))
		sub.Post("/", createHandler(host, p, collection))
		sub.Delete("/", bulkDeleteHandler(host, p, collection))
		sub.Post("/search", searchHandler(host, collection))
		sub.Get("/{uid}", getOneHandler(host, collection))
		sub.Put("/{uid}", replaceHandler(host, p, collection))
		sub.Patch("/{uid}", patchHandler(host, p, collection))
		sub.Delete("/{uid}", deleteOneHandler(host, p, collection))
	})
}

// listResponse mirrors snoozetypes.ListResponse for arbitrary document types
// returned by the generic CRUD layer (driver Documents are map[string]any).
type listResponse struct {
	Data []db.Document `json:"data"`
	Meta listMeta      `json:"meta"`
}

type listMeta struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// writeJSON sets the canonical content-type header, writes the status, then
// encodes body as JSON. It never returns; encoding errors are logged via the
// http.Error mechanism on the response writer.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Body is already partially written; we can't recover. Best-effort log.
		http.Error(w, fmt.Sprintf("encode: %v", err), http.StatusInternalServerError)
	}
}

// writeError sends a minimal error envelope. The full envelope including
// request_id / trace_id is the API package's responsibility; here we keep it
// small to avoid importing the api package and creating a dep cycle.
//
// 5xx responses are also slog.Error'd so the underlying message lands in the
// journal — the audit middleware only records the status code, so without
// this hook a "500 db_error" disappears into a black box.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	if status >= 500 {
		slog.Error("crud error response",
			"status", status,
			"code", code,
			"message", msg,
		)
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	})
}

// listHandler GET /api/v1/{plugin}
func listHandler(host Host, _ Plugin, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, cond, err := decodeListParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		docs, total, err := host.DB().Search(r.Context(), collection, cond, page)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if docs == nil {
			docs = []db.Document{}
		}
		writeJSON(w, http.StatusOK, listResponse{
			Data: docs,
			Meta: listMeta{
				Count:  len(docs),
				Limit:  page.PerPage,
				Offset: offsetFromPage(page),
				Total:  total,
			},
		})
	}
}

// searchHandler POST /api/v1/{plugin}/search — body {condition: <Cond>}
func searchHandler(host Host, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Condition condition.Cond `json:"condition"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		page, _, err := decodeListParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		docs, total, err := host.DB().Search(r.Context(), collection, body.Condition, page)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if docs == nil {
			docs = []db.Document{}
		}
		writeJSON(w, http.StatusOK, listResponse{
			Data: docs,
			Meta: listMeta{
				Count:  len(docs),
				Limit:  page.PerPage,
				Offset: offsetFromPage(page),
				Total:  total,
			},
		})
	}
}

// getOneHandler GET /api/v1/{plugin}/{uid}
func getOneHandler(host Host, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		if uid == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "missing uid")
			return
		}
		doc, err := host.DB().GetOne(r.Context(), collection, db.Document{"uid": uid})
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, doc)
	}
}

// createHandler POST /api/v1/{plugin}. Accepts a single object or an array.
func createHandler(host Host, p Plugin, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		docs, err := decodeWriteBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if dm, ok := p.(DataModel); ok {
			for _, d := range docs {
				if err := dm.Validate(d); err != nil {
					writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
					return
				}
			}
		}
		if wt, ok := p.(WriteTransformer); ok {
			for _, d := range docs {
				if err := wt.TransformWrite(r.Context(), d); err != nil {
					writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
					return
				}
			}
		}
		if g, ok := p.(WriteGuard); ok {
			for _, d := range docs {
				if err := g.GuardWrite(r.Context(), "", d, false); err != nil {
					writeError(w, http.StatusForbidden, "forbidden", err.Error())
					return
				}
			}
		}
		writeOpts := db.WriteOptions{UpdateTime: true}
		if pk, ok := p.(PrimaryKeyer); ok {
			primary := pk.PrimaryKey()
			if len(primary) > 0 {
				writeOpts.Primary = primary
				// Rejecting duplicates is the right default for a create
				// endpoint: a user POSTing a name that's already taken
				// should see an error, not a silent merge.
				writeOpts.DuplicatePolicy = "reject"
			}
		}
		res, err := host.DB().Write(r.Context(), collection, docs, writeOpts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		// Driver-level duplicate rejection lands as a Rejected entry rather
		// than err. Surface the first rejection as a 409 so callers see it.
		if len(res.Rejected) > 0 {
			writeError(w, http.StatusConflict, "duplicate", res.Rejected[0].Reason)
			return
		}
		if hook, ok := p.(CreateHook); ok {
			if err := hook.AfterCreate(r.Context(), docs); err != nil {
				host.Logger().Error("plugin AfterCreate failed",
					"plugin", collection, "err", err)
			}
		}
		emitAudit(r.Context(), host, p.Metadata(), collection, "create", res.Added, "")
		writeJSON(w, http.StatusCreated, res)
	}
}

// replaceHandler PUT /api/v1/{plugin}/{uid}
func replaceHandler(host Host, p Plugin, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		if uid == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "missing uid")
			return
		}
		var body db.Document
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		// The uid comes from the URL, not the body. Stamp it on the document so
		// DataModel.Validate / WriteTransformer see the identity of the row being
		// updated — e.g. a duplicate-guard that excludes "self" by uid must still
		// match when the request renames the row — and so the replacement keeps
		// its uid even if the client omitted it.
		body["uid"] = uid
		if dm, ok := p.(DataModel); ok {
			if err := dm.Validate(body); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
				return
			}
		}
		if wt, ok := p.(WriteTransformer); ok {
			if err := wt.TransformWrite(r.Context(), body); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
				return
			}
		}
		if g, ok := p.(WriteGuard); ok {
			if err := g.GuardWrite(r.Context(), uid, body, true); err != nil {
				writeError(w, http.StatusForbidden, "forbidden", err.Error())
				return
			}
		}
		matched, err := host.DB().ReplaceOne(r.Context(), collection, db.Document{"uid": uid}, body, true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if matched == 0 {
			writeError(w, http.StatusNotFound, "not_found", "uid "+uid)
			return
		}
		if hook, ok := p.(UpdateHook); ok {
			if err := hook.AfterUpdate(r.Context(), uid, body); err != nil {
				host.Logger().Error("plugin AfterUpdate failed",
					"plugin", collection, "err", err)
			}
		}
		emitAudit(r.Context(), host, p.Metadata(), collection, "replace", []string{uid}, "")
		writeJSON(w, http.StatusOK, map[string]any{"matched": matched})
	}
}

// patchHandler PATCH /api/v1/{plugin}/{uid}
func patchHandler(host Host, p Plugin, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		if uid == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "missing uid")
			return
		}
		var patch db.Document
		if err := decodeBody(r, &patch); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		// Stamp the URL uid so Validate / WriteTransformer can identify the row
		// being patched (e.g. a duplicate-guard that excludes self by uid). uid
		// is excluded from the audit summary and is a no-op on the merge.
		patch["uid"] = uid
		if dm, ok := p.(DataModel); ok {
			if err := dm.Validate(patch); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
				return
			}
		}
		if wt, ok := p.(WriteTransformer); ok {
			if err := wt.TransformWrite(r.Context(), patch); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
				return
			}
		}
		if g, ok := p.(WriteGuard); ok {
			if err := g.GuardWrite(r.Context(), uid, patch, false); err != nil {
				writeError(w, http.StatusForbidden, "forbidden", err.Error())
				return
			}
		}
		if err := host.DB().UpdateOne(r.Context(), collection, uid, patch, true); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if hook, ok := p.(UpdateHook); ok {
			if err := hook.AfterUpdate(r.Context(), uid, patch); err != nil {
				host.Logger().Error("plugin AfterUpdate failed",
					"plugin", collection, "err", err)
			}
		}
		emitAudit(r.Context(), host, p.Metadata(), collection, "patch", []string{uid}, patchSummary(patch))
		writeJSON(w, http.StatusOK, map[string]any{"uid": uid})
	}
}

// patchSummary returns a short, human-readable list of the fields a patch
// touched (e.g. "enabled, tree_order"). Used in audit summary text; we don't
// include the values because they may be large (nested condition trees,
// long descriptions) or sensitive.
func patchSummary(patch db.Document) string {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		// uid is implied; don't list it.
		if k == "uid" {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	// Stable order so audit rows are diff-friendly.
	sort.Strings(keys)
	out := keys[0]
	for _, k := range keys[1:] {
		out += ", " + k
	}
	return out
}

// deleteOneHandler DELETE /api/v1/{plugin}/{uid}
func deleteOneHandler(host Host, p Plugin, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		if uid == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "missing uid")
			return
		}
		if g, ok := p.(DeleteGuard); ok {
			if err := g.GuardDelete(r.Context(), []string{uid}); err != nil {
				writeError(w, http.StatusForbidden, "forbidden", err.Error())
				return
			}
		}
		deleted, err := host.DB().Delete(r.Context(), collection,
			condition.Equals("uid", uid), false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if deleted == 0 {
			writeError(w, http.StatusNotFound, "not_found", "uid "+uid)
			return
		}
		if hook, ok := p.(DeleteHook); ok {
			if err := hook.AfterDelete(r.Context(), []string{uid}); err != nil {
				host.Logger().Error("plugin AfterDelete failed",
					"plugin", collection, "err", err)
			}
		}
		emitAudit(r.Context(), host, p.Metadata(), collection, "delete", []string{uid}, "")
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
	}
}

// bulkDeleteHandler DELETE /api/v1/{plugin}?q=...
func bulkDeleteHandler(host Host, p Plugin, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, cond, err := decodeListParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		meta := p.Metadata()
		_, hasGuard := p.(DeleteGuard)
		auditing := meta.Audit && collection != auditCollection

		// Search once for both the guard and the audit log. Plugins that
		// implement neither pay nothing; plugins that implement both avoid
		// the duplicate round-trip the original code incurred.
		var uids []string
		if hasGuard || auditing {
			docs, _, serr := host.DB().Search(r.Context(), collection, cond, db.Page{PerPage: 0})
			if serr != nil {
				if hasGuard {
					// A guard must see an accurate list; fail closed rather than
					// risk deleting protected rows (e.g. the last platform admin).
					writeError(w, http.StatusInternalServerError, "db_error", serr.Error())
					return
				}
				host.Logger().Warn("plugins: pre-delete search for audit failed",
					"collection", collection, "err", serr)
			} else {
				uids = make([]string, 0, len(docs))
				for _, d := range docs {
					if u, ok := d["uid"].(string); ok && u != "" {
						uids = append(uids, u)
					}
				}
			}
		}
		if g, ok := p.(DeleteGuard); ok {
			if err := g.GuardDelete(r.Context(), uids); err != nil {
				writeError(w, http.StatusForbidden, "forbidden", err.Error())
				return
			}
		}
		deleted, err := host.DB().Delete(r.Context(), collection, cond, false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if hook, ok := p.(DeleteHook); ok {
			if err := hook.AfterDelete(r.Context(), uids); err != nil {
				host.Logger().Error("plugin AfterDelete failed",
					"plugin", collection, "err", err)
			}
		}
		emitAudit(r.Context(), host, meta, collection, "delete", uids, "bulk delete")
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
	}
}

// ---- helpers ---------------------------------------------------------------

// decodeBody enforces a non-empty JSON body. The body is closed by the caller
// chain (chi/net-http standard behaviour) — we use io.ReadAll explicitly only
// to give a useful error on syntactically invalid JSON.
func decodeBody(r *http.Request, into any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(raw) == 0 {
		return errors.New("empty body")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	return nil
}

// decodeWriteBody accepts either a single object or an array of objects.
func decodeWriteBody(r *http.Request) ([]db.Document, error) {
	if r.Body == nil {
		return nil, errors.New("empty body")
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("empty body")
	}
	// First try array.
	var arr []db.Document
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	// Fall back to single object.
	var one db.Document
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, fmt.Errorf("decode body: %w", err)
	}
	return []db.Document{one}, nil
}

// decodeListParams parses the ?q, ?limit, ?offset, ?orderby, ?asc query
// parameters of a list endpoint and returns the resulting Page and Cond. An
// empty ?q yields the AlwaysTrue condition.
func decodeListParams(r *http.Request) (db.Page, condition.Cond, error) {
	q := r.URL.Query()
	page := db.Page{Asc: true}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return page, condition.Cond{}, fmt.Errorf("bad limit %q", v)
		}
		page.PerPage = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return page, condition.Cond{}, fmt.Errorf("bad offset %q", v)
		}
		// Translate offset+limit into 1-indexed pageNb. If limit is zero the
		// driver ignores PageNb anyway, so this is harmless.
		if page.PerPage > 0 {
			page.PageNb = (n / page.PerPage) + 1
		}
	}
	page.OrderBy = q.Get("orderby")
	if v := q.Get("asc"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return page, condition.Cond{}, fmt.Errorf("bad asc %q", v)
		}
		page.Asc = b
	}

	cond := condition.Cond{}
	if encoded := q.Get("q"); encoded != "" {
		raw, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			// Tolerate plain (padded) base64 too.
			raw, err = base64.URLEncoding.DecodeString(encoded)
			if err != nil {
				return page, condition.Cond{}, fmt.Errorf("decode q: %w", err)
			}
		}
		if err := json.Unmarshal(raw, &cond); err != nil {
			return page, condition.Cond{}, fmt.Errorf("parse q: %w", err)
		}
	}
	return page, cond, nil
}

// offsetFromPage reverses Page.PageNb back into an offset for the response
// envelope, so the client sees what it sent.
func offsetFromPage(p db.Page) int {
	if p.PerPage == 0 || p.PageNb == 0 {
		return 0
	}
	return (p.PageNb - 1) * p.PerPage
}
