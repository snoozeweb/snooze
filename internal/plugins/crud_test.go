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

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
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
