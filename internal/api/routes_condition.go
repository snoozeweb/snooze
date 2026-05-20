package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/condition"
)

// mountCondition wires the search-DSL endpoints used by the React SearchBar:
//
//	POST /api/v1/condition/parse        — query string → Cond AST (or pos-anchored error)
//	GET  /api/v1/condition/fields       — field catalog for autocomplete
//
// Both endpoints are read-only and stateless, so they share no host
// dependency and can be mounted under any Router instance — including the
// minimal stubs used in unit tests.
func (rt *Router) mountCondition(r chi.Router) {
	r.Route("/api/v1/condition", func(sub chi.Router) {
		sub.Post("/parse", rt.handleConditionParse)
		sub.Get("/fields", rt.handleConditionFields)
	})
}

// parseError mirrors *condition.ParseError on the wire. A nil error field
// means the parse succeeded; a non-nil one means the condition field is
// absent and the editor should anchor a marker at `pos`.
type parseError struct {
	Pos     int    `json:"pos"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message"`
}

// parseResponse is the wire shape of POST /condition/parse. Exactly one of
// Condition / Error is populated on a 200 response.
type parseResponse struct {
	Condition *condition.Cond `json:"condition,omitempty"`
	Error     *parseError     `json:"error,omitempty"`
}

// handleConditionParse turns a search-bar string into the canonical Cond
// AST. The response is always 200 (even on parse failure) so the React
// component can render an inline error marker without distinguishing
// network failures from user typos.
//
// The handler returns 400 only when the *envelope* (not the query) is
// malformed — e.g. JSON syntax error, missing query field of the wrong type.
func (rt *Router) handleConditionParse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	if err := ParseJSONBody(r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(body.Query) == "" {
		// Empty query → AlwaysTrue. Encoded explicitly so the response
		// shape is symmetric with the success case.
		zero := condition.Cond{}
		WriteJSON(w, http.StatusOK, parseResponse{Condition: &zero})
		return
	}
	c, err := condition.Parse(body.Query)
	if err != nil {
		var pe *condition.ParseError
		if errors.As(err, &pe) {
			WriteJSON(w, http.StatusOK, parseResponse{Error: &parseError{
				Pos:     pe.Pos,
				Token:   pe.Token,
				Message: pe.Message,
			}})
			return
		}
		WriteJSON(w, http.StatusOK, parseResponse{Error: &parseError{Message: err.Error()}})
		return
	}
	WriteJSON(w, http.StatusOK, parseResponse{Condition: &c})
}

// fieldInfo describes one searchable field. Type is informational (it does
// not constrain the parser); Values, when present, enables value-completion
// in the SearchBar.
type fieldInfo struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Values      []string `json:"values,omitempty"`
}

// recordFields is the catalog for the `record` collection (alerts page).
// Order is preserved as authored — the SearchBar uses it as the
// default-suggestion order before any user typing applies a filter.
//
// Sources of truth (see pkg/snoozetypes/record.go):
//
//   - State enum: open / ack / close / shelved (plus "" for new alerts).
//   - Severity enum: critical / err / warning / info / unknown (default
//     "info"). The Python alerter accepted "error" too, kept here as an
//     alias to ease the migration from 1.x dashboards.
//   - rules / aggregate / snoozed: present on every processed record.
//
// Adding a field here is free; the catalog is purely a UX hint, the parser
// accepts any field name.
var recordFields = []fieldInfo{
	{Name: "host", Type: "string", Description: "Originating host"},
	{Name: "process", Type: "string", Description: "Process name"},
	{Name: "message", Type: "string", Description: "Alert message body"},
	{Name: "severity", Type: "string", Description: "Severity level",
		Values: []string{"critical", "err", "error", "warning", "info", "unknown"}},
	{Name: "state", Type: "string", Description: "Lifecycle state",
		Values: []string{"open", "ack", "close", "shelved"}},
	{Name: "source", Type: "string", Description: "Input source (syslog, snmptrap, …)"},
	{Name: "environment", Type: "string", Description: "Operator-defined environment tag"},
	{Name: "rules", Type: "array", Description: "Rules matched by the pipeline"},
	{Name: "aggregate", Type: "string", Description: "Aggregation key"},
	{Name: "snoozed", Type: "string", Description: "Name of the matching snooze entry (if any)"},
	{Name: "uid", Type: "string", Description: "Unique record id"},
	{Name: "date_epoch", Type: "number", Description: "Ingestion time (unix seconds)"},
	{Name: "duplicates", Type: "number", Description: "Repeat counter"},
	{Name: "ttl", Type: "number", Description: "Time-to-live (seconds). -1 = shelved"},
}

// fieldCatalog is the lookup table backing GET /condition/fields. Plugins
// whose names match are returned with their canonical fields; unknown
// collections return an empty array so the UI can fall back to the
// operator-only completion menu.
var fieldCatalog = map[string][]fieldInfo{
	// The collection used by the alerts page is `record` (see
	// internal/pluginimpl/record). Same collection backs the snooze
	// /api/v1/record list endpoint.
	"record": recordFields,
}

// handleConditionFields returns the field catalog for the requested
// collection. The `collection` query parameter defaults to `record`.
//
// We never 404 on an unknown collection — the editor stays useful with an
// empty catalog (operators and literals still suggest themselves), and
// returning a stable 200 with an empty array avoids an awkward error
// branch in the React hook.
func (rt *Router) handleConditionFields(w http.ResponseWriter, r *http.Request) {
	collection := r.URL.Query().Get("collection")
	if collection == "" {
		collection = "record"
	}
	fields, ok := fieldCatalog[collection]
	if !ok {
		fields = []fieldInfo{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"data": fields})
}
