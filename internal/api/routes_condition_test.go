package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// TestConditionParseRoute_Success asserts that a well-formed query returns
// the Cond AST in the response envelope so the frontend can encode it as
// the `?q=` parameter for list endpoints.
func TestConditionParseRoute_Success(t *testing.T) {
	rt := &Router{}
	r := chi.NewRouter()
	rt.mountCondition(r)

	body := []byte(`{"query":"host = myhost01 and severity = warning"}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/condition/parse", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct {
		Condition map[string]any `json:"condition"`
		Error     any            `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Nil(t, got.Error)
	require.Equal(t, "AND", got.Condition["op"])
	children, ok := got.Condition["children"].([]any)
	require.True(t, ok, "AND must have children")
	require.Len(t, children, 2)
}

// TestConditionParseRoute_Error asserts that a malformed query returns a
// 200 response with the error position so the editor can underline the
// offending token. Returning 200 (not 4xx) keeps the request cheap on the
// hot path where the user is mid-keystroke.
func TestConditionParseRoute_Error(t *testing.T) {
	rt := &Router{}
	r := chi.NewRouter()
	rt.mountCondition(r)

	body := []byte(`{"query":"host = "}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/condition/parse", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct {
		Condition any            `json:"condition"`
		Error     map[string]any `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Nil(t, got.Condition)
	require.NotNil(t, got.Error)
	require.Contains(t, got.Error, "pos")
	require.Contains(t, got.Error, "message")
}

// TestConditionParseRoute_EmptyQueryIsAlwaysTrue asserts an empty/whitespace
// query yields the AlwaysTrue condition (op:"" no field/value/children),
// which downstream encodes as an empty `q=` (no filter).
func TestConditionParseRoute_EmptyQueryIsAlwaysTrue(t *testing.T) {
	rt := &Router{}
	r := chi.NewRouter()
	rt.mountCondition(r)

	for _, q := range []string{"", "   "} {
		body, _ := json.Marshal(map[string]any{"query": q})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/condition/parse", bytes.NewReader(body)))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got struct {
			Condition map[string]any `json:"condition"`
			Error     any            `json:"error"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Nil(t, got.Error)
		require.Equal(t, "", got.Condition["op"])
	}
}

// TestConditionParseRoute_BadJSON asserts a malformed envelope (not the
// query string itself) returns 400.
func TestConditionParseRoute_BadJSON(t *testing.T) {
	rt := &Router{}
	r := chi.NewRouter()
	rt.mountCondition(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/condition/parse", bytes.NewReader([]byte(`{`))))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestConditionFieldsRoute_DefaultRecord asserts the catalog for the
// record (alerts) collection includes the canonical fields and exposes
// enum values for state and severity.
func TestConditionFieldsRoute_DefaultRecord(t *testing.T) {
	rt := &Router{}
	r := chi.NewRouter()
	rt.mountCondition(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/condition/fields?collection=record", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotEmpty(t, got.Data)

	byName := map[string]map[string]any{}
	for _, f := range got.Data {
		name, _ := f["name"].(string)
		byName[name] = f
	}
	for _, want := range []string{"host", "message", "severity", "state", "environment", "source", "rules"} {
		require.Contains(t, byName, want, "field %q missing from catalog", want)
	}
	sev := byName["severity"]
	values, _ := sev["values"].([]any)
	require.Contains(t, values, "critical")
	require.Contains(t, values, "warning")

	st := byName["state"]
	stateValues, _ := st["values"].([]any)
	require.Contains(t, stateValues, "open")
	require.Contains(t, stateValues, "ack")
}

// TestConditionFieldsRoute_UnknownCollection asserts an unknown collection
// returns an empty data array (not 404) so the UI can still show the
// generic field-less autocomplete (operators + literals) without an extra
// branch.
func TestConditionFieldsRoute_UnknownCollection(t *testing.T) {
	rt := &Router{}
	r := chi.NewRouter()
	rt.mountCondition(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/condition/fields?collection=does-not-exist", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.Data)
	require.Empty(t, got.Data)
}
