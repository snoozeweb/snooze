package plugins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// crudPlugin is a minimal Plugin for CRUD wiring (no Processor/DataModel).
type crudPlugin struct{ name string }

func (p *crudPlugin) Name() string                              { return p.name }
func (p *crudPlugin) Metadata() Metadata                        { return Metadata{Name: p.name} }
func (p *crudPlugin) PostInit(context.Context, Host) error      { return nil }
func (p *crudPlugin) Reload(context.Context) error              { return nil }

// validatingPlugin extends crudPlugin with DataModel + Validate.
type validatingPlugin struct {
	crudPlugin
	validate func(map[string]any) error
}

func (p *validatingPlugin) Schema() any                       { return map[string]string{"type": "object"} }
func (p *validatingPlugin) Validate(obj map[string]any) error { return p.validate(obj) }

// routedPlugin satisfies RouteProvider.
type routedPlugin struct {
	crudPlugin
	called bool
}

func (p *routedPlugin) RegisterRoutes(r chi.Router, host Host) {
	p.called = true
	r.Get("/custom", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
}

// mount builds a chi router with MountCRUD and returns it ready for httptest.
func mount(t *testing.T, p Plugin, driver db.Driver) (chi.Router, *nullHost) {
	t.Helper()
	r := chi.NewRouter()
	h := newNullHost(driver)
	MountCRUD(r, h, p)
	return r, h
}

func doRequest(t *testing.T, r chi.Router, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			buf = strings.NewReader(v)
		case []byte:
			buf = bytes.NewReader(v)
		default:
			data, err := json.Marshal(body)
			require.NoError(t, err)
			buf = bytes.NewReader(data)
		}
	}
	req := httptest.NewRequest(method, target, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), into))
}

func TestCRUD_PostListGetPatchDelete(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &crudPlugin{name: "thing"}
	r, _ := mount(t, plug, memo)

	// Create one
	rec := doRequest(t, r, "POST", "/api/v1/thing", db.Document{"uid": "u1", "host": "a"})
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	var wres db.WriteResult
	decode(t, rec, &wres)
	require.Equal(t, []string{"u1"}, wres.Added)

	// Create multiple via array body
	rec = doRequest(t, r, "POST", "/api/v1/thing", []db.Document{
		{"uid": "u2", "host": "b"},
		{"uid": "u3", "host": "c"},
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	// List
	rec = doRequest(t, r, "GET", "/api/v1/thing", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list listResponse
	decode(t, rec, &list)
	require.Len(t, list.Data, 3)
	require.Equal(t, 3, list.Meta.Count)
	require.Equal(t, 3, list.Meta.Total)
	require.Equal(t, 0, list.Meta.Offset)

	// List with pagination
	rec = doRequest(t, r, "GET", "/api/v1/thing?limit=2&offset=0", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	decode(t, rec, &list)
	require.Len(t, list.Data, 2)
	require.Equal(t, 2, list.Meta.Limit)

	// Get one
	rec = doRequest(t, r, "GET", "/api/v1/thing/u2", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var doc db.Document
	decode(t, rec, &doc)
	require.Equal(t, "b", doc["host"])

	// Get missing
	rec = doRequest(t, r, "GET", "/api/v1/thing/missing", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Patch
	rec = doRequest(t, r, "PATCH", "/api/v1/thing/u1", db.Document{"host": "a2"})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "a2", memo.docs("thing")[0]["host"])

	// Replace
	rec = doRequest(t, r, "PUT", "/api/v1/thing/u1", db.Document{"uid": "u1", "host": "a3"})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "a3", memo.docs("thing")[0]["host"])

	// Replace missing
	rec = doRequest(t, r, "PUT", "/api/v1/thing/missing", db.Document{"host": "x"})
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Delete one
	rec = doRequest(t, r, "DELETE", "/api/v1/thing/u3", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, memo.docs("thing"), 2)

	// Delete missing
	rec = doRequest(t, r, "DELETE", "/api/v1/thing/u3", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCRUD_BulkDeleteWithQuery(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &crudPlugin{name: "alert"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/alert", []db.Document{
		{"uid": "u1", "host": "a"},
		{"uid": "u2", "host": "b"},
		{"uid": "u3", "host": "a"},
	})

	// Build a q= for host=a
	cond := condition.Equals("host", "a")
	raw, err := json.Marshal(cond)
	require.NoError(t, err)
	q := base64.RawURLEncoding.EncodeToString(raw)

	rec := doRequest(t, r, "DELETE", "/api/v1/alert?q="+q, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	decode(t, rec, &resp)
	// JSON numbers decode to float64.
	require.Equal(t, float64(2), resp["deleted"])
	require.Len(t, memo.docs("alert"), 1)
	require.Equal(t, "b", memo.docs("alert")[0]["host"])
}

func TestCRUD_SearchEndpoint(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &crudPlugin{name: "rec"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/rec", []db.Document{
		{"uid": "u1", "host": "a"},
		{"uid": "u2", "host": "b"},
	})

	body := map[string]any{"condition": condition.Equals("host", "b")}
	rec := doRequest(t, r, "POST", "/api/v1/rec/search", body)
	require.Equal(t, http.StatusOK, rec.Code)

	var list listResponse
	decode(t, rec, &list)
	require.Len(t, list.Data, 1)
	require.Equal(t, "b", list.Data[0]["host"])
}

func TestCRUD_DataModelValidation(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &validatingPlugin{
		crudPlugin: crudPlugin{name: "u"},
		validate: func(obj map[string]any) error {
			if _, ok := obj["host"]; !ok {
				return errors.New("host is required")
			}
			return nil
		},
	}
	r, _ := mount(t, plug, memo)

	// Missing host → 422.
	rec := doRequest(t, r, "POST", "/api/v1/u", db.Document{"uid": "u1"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// Valid → 201.
	rec = doRequest(t, r, "POST", "/api/v1/u", db.Document{"uid": "u1", "host": "h"})
	require.Equal(t, http.StatusCreated, rec.Code)

	// PUT validates too.
	rec = doRequest(t, r, "PUT", "/api/v1/u/u1", db.Document{"uid": "u1"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// PATCH validates too.
	rec = doRequest(t, r, "PATCH", "/api/v1/u/u1", db.Document{"foo": "bar"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestCRUD_RouteProviderTakesOver(t *testing.T) {
	t.Parallel()
	plug := &routedPlugin{crudPlugin: crudPlugin{name: "alertmanager"}}
	r, _ := mount(t, plug, newMemDB())

	rec := doRequest(t, r, "GET", "/api/v1/alertmanager/custom", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, plug.called)

	// Generic GET / should NOT exist (the RouteProvider replaces it).
	rec = doRequest(t, r, "GET", "/api/v1/alertmanager", nil)
	require.NotEqual(t, http.StatusOK, rec.Code)
}

func TestCRUD_BadRequestPaths(t *testing.T) {
	t.Parallel()
	plug := &crudPlugin{name: "x"}
	r, _ := mount(t, plug, newMemDB())

	// POST with empty body
	rec := doRequest(t, r, "POST", "/api/v1/x", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// POST with invalid JSON
	rec = doRequest(t, r, "POST", "/api/v1/x", "{not json")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Bad ?limit
	rec = doRequest(t, r, "GET", "/api/v1/x?limit=abc", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Bad ?offset
	rec = doRequest(t, r, "GET", "/api/v1/x?offset=-1", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Bad ?asc
	rec = doRequest(t, r, "GET", "/api/v1/x?asc=lol", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Bad ?q (non-base64)
	rec = doRequest(t, r, "GET", "/api/v1/x?q=!!!", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Bad ?q (base64 of invalid JSON)
	rec = doRequest(t, r, "GET", "/api/v1/x?q="+base64.RawURLEncoding.EncodeToString([]byte("[")), nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCRUD_ListPaginationWithQ(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &crudPlugin{name: "alerts"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/alerts", []db.Document{
		{"uid": "u1", "host": "a"},
		{"uid": "u2", "host": "b"},
		{"uid": "u3", "host": "a"},
	})

	cond := condition.Equals("host", "a")
	raw, _ := json.Marshal(cond)
	q := base64.RawURLEncoding.EncodeToString(raw)
	rec := doRequest(t, r, "GET", "/api/v1/alerts?q="+q, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list listResponse
	decode(t, rec, &list)
	require.Len(t, list.Data, 2)
	require.Equal(t, 2, list.Meta.Total)
}

// ---- Audit emission ------------------------------------------------------

// auditPlugin is a Plugin whose Metadata.Audit is true, so the CRUD layer
// emits audit-log entries for its mutations. Used to verify the
// emitAudit() integration in createHandler/replaceHandler/patchHandler/
// deleteOneHandler/bulkDeleteHandler.
type auditPlugin struct{ name string }

func (p *auditPlugin) Name() string                         { return p.name }
func (p *auditPlugin) Metadata() Metadata                   { return Metadata{Name: p.name, Audit: true} }
func (p *auditPlugin) PostInit(context.Context, Host) error { return nil }
func (p *auditPlugin) Reload(context.Context) error         { return nil }

// auditDocs returns the documents in the "audit" collection of memDB, sorted
// stably by their object_id so assertions don't depend on insertion order.
func auditDocs(memo *memDB) []db.Document {
	memo.mu.Lock()
	defer memo.mu.Unlock()
	out := append([]db.Document(nil), memo.data[auditCollection]...)
	return out
}

func TestAudit_EmittedOnCreate(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &auditPlugin{name: "rule"}
	r, _ := mount(t, plug, memo)

	rec := doRequest(t, r, "POST", "/api/v1/rule", db.Document{"uid": "r1", "name": "x"})
	require.Equal(t, http.StatusCreated, rec.Code)

	rows := auditDocs(memo)
	require.Len(t, rows, 1)
	require.Equal(t, "rule", rows[0]["object_type"])
	require.Equal(t, "r1", rows[0]["object_id"])
	require.Equal(t, "create", rows[0]["action"])
	require.IsType(t, float64(0), rows[0]["date_epoch"])
}

func TestAudit_EmittedOnPatchWithFieldSummary(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &auditPlugin{name: "rule"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/rule", db.Document{"uid": "r1", "name": "x"})
	rec := doRequest(t, r, "PATCH", "/api/v1/rule/r1",
		db.Document{"enabled": true, "tree_order": 3})
	require.Equal(t, http.StatusOK, rec.Code)

	rows := auditDocs(memo)
	require.Len(t, rows, 2) // create + patch
	patch := rows[1]
	require.Equal(t, "patch", patch["action"])
	require.Equal(t, "r1", patch["object_id"])
	// Summary lists the patched fields, sorted for stable diffs.
	require.Equal(t, "enabled, tree_order", patch["summary"])
}

func TestAudit_EmittedOnReplace(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &auditPlugin{name: "rule"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/rule", db.Document{"uid": "r1", "name": "x"})
	rec := doRequest(t, r, "PUT", "/api/v1/rule/r1", db.Document{"name": "y"})
	require.Equal(t, http.StatusOK, rec.Code)

	rows := auditDocs(memo)
	require.Len(t, rows, 2)
	require.Equal(t, "replace", rows[1]["action"])
}

func TestAudit_EmittedOnDeleteOne(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &auditPlugin{name: "rule"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/rule", db.Document{"uid": "r1", "name": "x"})
	rec := doRequest(t, r, "DELETE", "/api/v1/rule/r1", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	rows := auditDocs(memo)
	require.Len(t, rows, 2)
	require.Equal(t, "delete", rows[1]["action"])
	require.Equal(t, "r1", rows[1]["object_id"])
}

func TestAudit_EmittedOnBulkDelete(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	plug := &auditPlugin{name: "rule"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/rule", []db.Document{
		{"uid": "r1", "name": "a", "tag": "x"},
		{"uid": "r2", "name": "b", "tag": "x"},
		{"uid": "r3", "name": "c", "tag": "y"},
	})
	cond := condition.Equals("tag", "x")
	raw, _ := json.Marshal(cond)
	q := base64.RawURLEncoding.EncodeToString(raw)
	rec := doRequest(t, r, "DELETE", "/api/v1/rule?q="+q, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// 3 creates (one POST with array) + 2 deletes for r1, r2.
	rows := auditDocs(memo)
	require.Len(t, rows, 5)
	deletes := rows[3:]
	require.Equal(t, "delete", deletes[0]["action"])
	require.Equal(t, "bulk delete", deletes[0]["summary"])
	require.ElementsMatch(t, []any{"r1", "r2"}, []any{deletes[0]["object_id"], deletes[1]["object_id"]})
}

func TestAudit_SkippedWhenPluginDisabled(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	// crudPlugin's Metadata has Audit:false — the default for noisy
	// collections (comment, kv).
	plug := &crudPlugin{name: "comment"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/comment", db.Document{"uid": "c1"})
	doRequest(t, r, "PATCH", "/api/v1/comment/c1", db.Document{"message": "hi"})
	doRequest(t, r, "DELETE", "/api/v1/comment/c1", nil)

	require.Empty(t, auditDocs(memo))
}

func TestAudit_DoesNotRecurseOnAuditCollection(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	// Even if a plugin declares Audit:true for the "audit" collection
	// itself, emitAudit must short-circuit; otherwise every audit insert
	// would loop indefinitely.
	plug := &auditPlugin{name: "audit"}
	r, _ := mount(t, plug, memo)

	doRequest(t, r, "POST", "/api/v1/audit",
		db.Document{"uid": "a1", "object_type": "rule"})

	// One document exists in the audit collection — the one we POSTed —
	// but no extra "audit-of-audit" record was emitted.
	require.Len(t, auditDocs(memo), 1)
}

// TestAudit_CapturesUsernameFromContext is a direct exercise of emitAudit
// rather than going through the HTTP surface, because the http handler
// here doesn't run the auth middleware. The HTTP path is exercised
// end-to-end by the E2E tour.
func TestAudit_CapturesUsernameFromContext(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	h := newNullHost(memo)

	ctx := auth.WithClaims(context.Background(), snoozetypes.Claims{
		Subject: "alice",
		Method:  "local",
	})
	emitAudit(ctx, h, Metadata{Audit: true}, "rule", "patch", []string{"r1"}, "enabled")

	rows := auditDocs(memo)
	require.Len(t, rows, 1)
	require.Equal(t, "alice", rows[0]["username"])
	require.Equal(t, "local", rows[0]["method"])
	require.Equal(t, "enabled", rows[0]["summary"])
}

func TestMetadata_AuditDefaultsToTrueWhenAbsent(t *testing.T) {
	t.Parallel()
	// Most plugins don't declare `audit:` — the default semantic is "audit
	// unless explicitly disabled" (matches the Python convention). Without
	// the custom UnmarshalYAML the Go zero-value would silently flip it.
	m, err := ParseMetadata([]byte("name: Foo\n"))
	require.NoError(t, err)
	require.True(t, m.Audit)
}

func TestMetadata_AuditRespectsExplicitFalse(t *testing.T) {
	t.Parallel()
	m, err := ParseMetadata([]byte("name: Foo\naudit: false\n"))
	require.NoError(t, err)
	require.False(t, m.Audit)
}
