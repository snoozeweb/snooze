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

// ruleFields — search hints for the Rules tree (/api/v1/rule list).
// The tree_order / parents fields are exposed because operators routinely
// want to find roots (parents = []) or specific subtree positions.
var ruleFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Rule name"},
	{Name: "enabled", Type: "bool", Description: "Whether the rule fires"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "parents", Type: "array", Description: "Parent uids (empty = root)"},
	{Name: "tree_order", Type: "number", Description: "Sibling position"},
	{Name: "uid", Type: "string", Description: "Unique rule id"},
}

// aggregateRuleFields — searchable shape for the Aggregates tab.
var aggregateRuleFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Aggregate rule name"},
	{Name: "enabled", Type: "bool", Description: "Whether the rule fires"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "fields", Type: "array", Description: "Aggregation key fields"},
	{Name: "watch", Type: "array", Description: "Fields tracked for changes"},
	{Name: "throttle", Type: "number", Description: "Throttle window (0 = unlimited)"},
	{Name: "uid", Type: "string", Description: "Unique rule id"},
}

// snoozeFields — searchable shape for the Snoozes page.
var snoozeFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Snooze name"},
	{Name: "enabled", Type: "bool", Description: "Whether the snooze is in effect"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "discard", Type: "bool", Description: "Discard matching alerts instead of tagging"},
	{Name: "uid", Type: "string", Description: "Unique snooze id"},
}

// userFields — searchable shape for the Users admin page.
var userFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Username"},
	{Name: "method", Type: "string", Description: "Auth backend",
		Values: []string{"local", "ldap", "anonymous"}},
	{Name: "type", Type: "string", Description: "Legacy alias for method"},
	{Name: "roles", Type: "array", Description: "Assigned roles"},
	{Name: "groups", Type: "array", Description: "Groups from the auth backend"},
	{Name: "last_login", Type: "number", Description: "Last successful login (epoch seconds)"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "uid", Type: "string", Description: "Unique user id"},
}

// roleFields — searchable shape for the Roles admin page.
var roleFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Role name"},
	{Name: "permissions", Type: "array", Description: "Permission strings"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "uid", Type: "string", Description: "Unique role id"},
}

// notificationFields — searchable shape for notifications.
var notificationFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Notification name"},
	{Name: "enabled", Type: "bool", Description: "Whether it fires"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "actions", Type: "array", Description: "Action names triggered"},
	{Name: "uid", Type: "string", Description: "Unique notification id"},
}

// actionFields — searchable shape for actions (notifier configs). The
// notifier plugin name lives at `action.selected` on the stored document
// (mirroring the Python ActionObject layout — see pluginimpl/notification/
// plugin.go::actionEnvelope), so that's the path the DSL must use.
var actionFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Action name"},
	{Name: "action.selected", Type: "string", Description: "Notifier plugin (mail, webhook, …)"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "uid", Type: "string", Description: "Unique action id"},
}

// environmentFields — searchable shape for environment tags.
var environmentFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Environment name"},
	{Name: "color", Type: "string", Description: "Display color"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "uid", Type: "string", Description: "Unique environment id"},
}

// kvFields — searchable shape for the key-value store.
var kvFields = []fieldInfo{
	{Name: "namespace", Type: "string", Description: "Logical bucket"},
	{Name: "key", Type: "string", Description: "Key within the namespace"},
	{Name: "value", Type: "string", Description: "Stored value"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "uid", Type: "string", Description: "Unique entry id"},
}

// widgetFields — searchable shape for dashboard widgets.
var widgetFields = []fieldInfo{
	{Name: "name", Type: "string", Description: "Widget name"},
	{Name: "type", Type: "string", Description: "Widget type"},
	{Name: "comment", Type: "string", Description: "Free-form description"},
	{Name: "uid", Type: "string", Description: "Unique widget id"},
}

// auditFields — searchable shape for the audit log.
var auditFields = []fieldInfo{
	{Name: "object_type", Type: "string", Description: "Resource collection"},
	{Name: "object_id", Type: "string", Description: "Resource uid"},
	{Name: "action", Type: "string", Description: "Audit action",
		Values: []string{"create", "update", "delete"}},
	{Name: "user", Type: "string", Description: "Username that performed the action"},
	{Name: "date_epoch", Type: "number", Description: "When (unix seconds)"},
	{Name: "uid", Type: "string", Description: "Unique entry id"},
}

// fieldCatalog is the lookup table backing GET /condition/fields. Plugins
// whose names match are returned with their canonical fields; unknown
// collections return an empty array so the UI can fall back to the
// operator-only completion menu.
//
// Adding an entry here gives the corresponding admin page server-side
// search via ?q= — the same DSL the alerts page already uses. The CRUD
// layer in internal/plugins/crud.go decodes ?q= for every list endpoint;
// the catalog only feeds autocomplete, it never gates which fields are
// searchable.
var fieldCatalog = map[string][]fieldInfo{
	"record":        recordFields,
	"rule":          ruleFields,
	"aggregaterule": aggregateRuleFields,
	"snooze":        snoozeFields,
	"user":          userFields,
	"role":          roleFields,
	"notification":  notificationFields,
	"action":        actionFields,
	"environment":   environmentFields,
	"kv":            kvFields,
	"widget":        widgetFields,
	"audit":         auditFields,
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
