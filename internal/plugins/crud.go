package plugins

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
)

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
func MountCRUD(r chi.Router, host Host, p Plugin) {
	if rp, ok := p.(RouteProvider); ok {
		r.Route("/api/v1/"+p.Name(), func(sub chi.Router) {
			rp.RegisterRoutes(sub, host)
		})
		return
	}

	collection := p.Name()
	r.Route("/api/v1/"+collection, func(sub chi.Router) {
		sub.Get("/", listHandler(host, p, collection))
		sub.Post("/", createHandler(host, p, collection))
		sub.Delete("/", bulkDeleteHandler(host, collection))
		sub.Post("/search", searchHandler(host, collection))
		sub.Get("/{uid}", getOneHandler(host, collection))
		sub.Put("/{uid}", replaceHandler(host, p, collection))
		sub.Patch("/{uid}", patchHandler(host, p, collection))
		sub.Delete("/{uid}", deleteOneHandler(host, collection))
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
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	})
}

// listHandler GET /api/v1/{plugin}
func listHandler(host Host, p Plugin, collection string) http.HandlerFunc {
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
		if dm, ok := p.(DataModel); ok {
			if err := dm.Validate(body); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
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
		if dm, ok := p.(DataModel); ok {
			if err := dm.Validate(patch); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
				return
			}
		}
		if err := host.DB().UpdateOne(r.Context(), collection, uid, patch, true); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"uid": uid})
	}
}

// deleteOneHandler DELETE /api/v1/{plugin}/{uid}
func deleteOneHandler(host Host, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		if uid == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "missing uid")
			return
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
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
	}
}

// bulkDeleteHandler DELETE /api/v1/{plugin}?q=...
func bulkDeleteHandler(host Host, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, cond, err := decodeListParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		deleted, err := host.DB().Delete(r.Context(), collection, cond, false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
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
