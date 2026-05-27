package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// commentMethod is the `method` stamped on every comment/snooze the MCP
// server posts, mirroring forward.go's Method:"google" and the teams
// handler's "teams". Lets operators see in the audit trail that an action
// came from an AI assistant via MCP.
const commentMethod = "mcp"

// recordSearchEndpoint is the canonical structured-search route. Body shape:
// {"condition": <Cond>}; response {"data": [...], "meta": {...}}. Mirrors
// internal/components/googlechat/forward.go and the snooze-jira poller.
const recordSearchEndpoint = "/api/v1/record/search"

// defaultListLimit caps list_alerts when the caller omits limit. Kept modest
// because the result is fed to an LLM context window.
const defaultListLimit = 20

// tool is one entry in the MCP tool catalog.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// toolsListResult is the tools/list response payload.
type toolsListResult struct {
	Tools []tool `json:"tools"`
}

// toolContent is one item in an MCP tool-call result's content array. Only
// text content is produced by this server.
type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolCallResult is the tools/call result payload.
type toolCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError"`
}

// objSchema is a tiny helper for building a JSON-schema object node.
func objSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// catalog is the static tool catalog returned by tools/list. Built once.
func catalog() []tool {
	uidProp := map[string]any{
		"type":        "string",
		"description": "The Snooze record UID (from list_alerts / get_alert).",
	}
	msgProp := map[string]any{
		"type":        "string",
		"description": "Optional free-text note recorded with the action.",
	}
	return []tool{
		{
			Name:        "list_alerts",
			Description: "List or search Snooze alert records. Returns the most recent matching alerts (uid, host, severity, message, state, timestamp).",
			InputSchema: objSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Optional free-text search across all record fields (SEARCH operator). Mutually combined with `condition` when both are given.",
				},
				"condition": map[string]any{
					"type":        "array",
					"description": "Optional Snooze condition in list form, e.g. [\"=\", \"host\", \"web-1\"] or [\"AND\", [...], [...]]. Advanced; prefer `query` for plain text.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Max number of alerts to return. Default %d.", defaultListLimit),
					"minimum":     1,
				},
			}),
		},
		{
			Name:        "get_alert",
			Description: "Fetch a single Snooze alert record by its UID.",
			InputSchema: objSchema(map[string]any{"uid": uidProp}, "uid"),
		},
		{
			Name:        "ack_alert",
			Description: "Acknowledge a Snooze alert by UID. Records an `ack` comment that transitions the record to the acknowledged state.",
			InputSchema: objSchema(map[string]any{"uid": uidProp, "message": msgProp}, "uid"),
		},
		{
			Name:        "close_alert",
			Description: "Close (resolve) a Snooze alert by UID. Records a `close` comment that transitions the record to the closed state.",
			InputSchema: objSchema(map[string]any{"uid": uidProp, "message": msgProp}, "uid"),
		},
		{
			Name:        "comment_alert",
			Description: "Add a free-text comment to a Snooze alert by UID without changing its state.",
			InputSchema: objSchema(map[string]any{
				"uid": uidProp,
				"message": map[string]any{
					"type":        "string",
					"description": "The comment text. Required.",
				},
			}, "uid", "message"),
		},
		{
			Name:        "snooze_alert",
			Description: "Snooze a Snooze alert by UID so re-deliveries are suppressed for a window. Also acknowledges the alert.",
			InputSchema: objSchema(map[string]any{
				"uid": uidProp,
				"duration": map[string]any{
					"type":        "string",
					"description": "How long to snooze as a Go duration (\"6h\", \"30m\"). Omit for a forever snooze.",
				},
			}, "uid"),
		},
	}
}

// handleToolsList returns the catalog.
func (s *Server) handleToolsList() toolsListResult {
	return toolsListResult{Tools: catalog()}
}

// toolCallParams is the tools/call params envelope.
type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// handleToolsCall decodes the call, dispatches by tool name, and returns the
// MCP result. A protocol-level problem (bad params, unknown tool) is returned
// as an *rpcError; a Snooze-side failure is returned as an ordinary result
// with isError:true (per the MCP convention that tool execution errors live
// in the result, not the JSON-RPC envelope).
func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (toolCallResult, *rpcError) {
	var p toolCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return toolCallResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid tools/call params: " + err.Error()}
	}
	if p.Name == "" {
		return toolCallResult{}, &rpcError{Code: codeInvalidParams, Message: "tools/call: missing tool name"}
	}

	args := map[string]any{}
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return toolCallResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid arguments: " + err.Error()}
		}
	}

	switch p.Name {
	case "list_alerts":
		return s.toolListAlerts(ctx, args), nil
	case "get_alert":
		return s.toolGetAlert(ctx, args), nil
	case "ack_alert":
		return s.toolComment(ctx, args, "ack", "acknowledged"), nil
	case "close_alert":
		return s.toolComment(ctx, args, "close", "closed"), nil
	case "comment_alert":
		return s.toolComment(ctx, args, "", "commented"), nil
	case "snooze_alert":
		return s.toolSnooze(ctx, args), nil
	default:
		// An unknown tool is a protocol error per the MCP spec.
		return toolCallResult{}, &rpcError{Code: codeMethodNotFound, Message: "unknown tool: " + p.Name}
	}
}

// textResult wraps text in a successful tool result.
func textResult(text string) toolCallResult {
	return toolCallResult{Content: []toolContent{{Type: "text", Text: text}}, IsError: false}
}

// errResult wraps an error message in a failed tool result (isError:true).
func errResult(format string, a ...any) toolCallResult {
	return toolCallResult{Content: []toolContent{{Type: "text", Text: fmt.Sprintf(format, a...)}}, IsError: true}
}

// jsonResult marshals v as indented JSON and wraps it as a text result so the
// LLM gets structured data it can parse.
func jsonResult(v any) toolCallResult {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errResult("could not encode result: %v", err)
	}
	return textResult(string(raw))
}

// recordSearchEnvelope is the {"data":[...]} response of POST
// /api/v1/record/search. Records are kept as raw maps so the LLM sees every
// field the typed schema doesn't model.
type recordSearchEnvelope struct {
	Data []map[string]any `json:"data"`
}

// toolListAlerts runs a structured search. `query` becomes a SEARCH condition;
// `condition` (list form) is passed through verbatim. When both are present
// they're AND-ed. With neither, an empty condition matches everything.
func (s *Server) toolListAlerts(ctx context.Context, args map[string]any) toolCallResult {
	limit := defaultListLimit
	if l, ok := asInt(args["limit"]); ok && l > 0 {
		limit = l
	}

	cond := buildListCondition(args)
	body := map[string]any{
		"condition": cond,
		// The CRUD search handler honours these list params for pagination.
		"perpage": limit,
		"orderby": "date_epoch",
		"asc":     false,
	}
	var env recordSearchEnvelope
	if err := s.api.Post(ctx, recordSearchEndpoint, body, &env); err != nil {
		return errResult("list_alerts: search failed: %v", err)
	}
	data := env.Data
	if len(data) > limit {
		data = data[:limit]
	}
	summaries := make([]map[string]any, 0, len(data))
	for _, rec := range data {
		summaries = append(summaries, summarise(rec))
	}
	return jsonResult(map[string]any{"count": len(summaries), "alerts": summaries})
}

// buildListCondition turns the query/condition args into a condition payload
// the server's parser accepts. We emit list-form conditions (["op", ...])
// because that's the wire shape forward.go uses and the parser accepts it
// alongside the AST form.
func buildListCondition(args map[string]any) any {
	var conds []any
	if q, ok := args["query"].(string); ok {
		if q = strings.TrimSpace(q); q != "" {
			conds = append(conds, []any{"SEARCH", q})
		}
	}
	if c, ok := args["condition"].([]any); ok && len(c) > 0 {
		conds = append(conds, c)
	}
	switch len(conds) {
	case 0:
		return nil // empty condition → match everything
	case 1:
		return conds[0]
	default:
		return append([]any{"AND"}, conds...)
	}
}

// toolGetAlert fetches a single record by uid via a search on uid (the search
// endpoint is the uniform path that returns the full document; the CRUD
// GET-by-uid path returns a bare doc but search keeps one code path).
func (s *Server) toolGetAlert(ctx context.Context, args map[string]any) toolCallResult {
	uid, ok := requireUID(args)
	if !ok {
		return errResult("get_alert: missing or empty `uid`")
	}
	body := map[string]any{"condition": []any{"=", "uid", uid}}
	var env recordSearchEnvelope
	if err := s.api.Post(ctx, recordSearchEndpoint, body, &env); err != nil {
		return errResult("get_alert: search failed: %v", err)
	}
	if len(env.Data) == 0 {
		return errResult("get_alert: no alert found with uid %q", uid)
	}
	return jsonResult(env.Data[0])
}

// toolComment posts a typed comment (ack / close / plain). actionType is the
// `type` field; pastVerb is the word used in the success message. This is the
// exact PostComment path the snooze-teams handler uses.
func (s *Server) toolComment(ctx context.Context, args map[string]any, actionType, pastVerb string) toolCallResult {
	uid, ok := requireUID(args)
	if !ok {
		return errResult("%s: missing or empty `uid`", pastVerb)
	}
	message, _ := args["message"].(string)
	if actionType == "" && strings.TrimSpace(message) == "" {
		// A plain comment with no text is meaningless.
		return errResult("comment_alert: `message` is required")
	}
	err := s.api.PostComment(ctx, snoozeclient.Comment{
		RecordUID: uid,
		Name:      commentName(args),
		Method:    commentMethod,
		Type:      actionType,
		Message:   message,
	})
	if err != nil {
		return errResult("could not %s alert %s: %v", pastVerb, uid, err)
	}
	suffix := ""
	if strings.TrimSpace(message) != "" {
		suffix = fmt.Sprintf(" with message %q", message)
	}
	return textResult(fmt.Sprintf("Alert %s %s%s.", uid, pastVerb, suffix))
}

// toolSnooze creates a snooze entry scoped to the record's uid and then acks
// it — mirroring the snooze-teams handleSnooze flow. An omitted/blank
// duration means "forever" (nil time_constraints).
func (s *Server) toolSnooze(ctx context.Context, args map[string]any) toolCallResult {
	uid, ok := requireUID(args)
	if !ok {
		return errResult("snooze_alert: missing or empty `uid`")
	}
	durStr, _ := args["duration"].(string)
	durStr = strings.TrimSpace(durStr)

	var tc map[string]any
	human := "forever"
	if durStr != "" {
		d, err := time.ParseDuration(durStr)
		if err != nil || d <= 0 {
			return errResult("snooze_alert: invalid duration %q (use Go syntax like \"6h\" or \"30m\", or omit for forever)", durStr)
		}
		now := time.Now()
		until := now.Add(d)
		// time_constraints shape mirrors what the notification plugin
		// consumes: a single datetime window [now, until].
		tc = map[string]any{
			"datetime": []map[string]any{{
				"from":  now.UTC().Format(time.RFC3339),
				"until": until.UTC().Format(time.RFC3339),
			}},
		}
		human = durStr
	}

	snooze := snoozeclient.Snooze{
		Name:            fmt.Sprintf("[mcp] snooze %s (%s)", human, uid),
		Comment:         commentName(args),
		Condition:       []any{"=", "uid", uid},
		TimeConstraints: tc,
	}
	if err := s.api.CreateSnooze(ctx, snooze); err != nil {
		return errResult("snooze_alert: could not create snooze for %s: %v", uid, err)
	}
	// Best-effort ack so the record visibly changes state (errors non-fatal —
	// the snooze entry is the load-bearing side effect).
	_ = s.api.PostComment(ctx, snoozeclient.Comment{
		RecordUID: uid,
		Name:      commentName(args),
		Method:    commentMethod,
		Type:      "ack",
		Message:   "Snoozed for " + human,
	})
	if tc == nil {
		return textResult(fmt.Sprintf("Alert %s snoozed forever.", uid))
	}
	return textResult(fmt.Sprintf("Alert %s snoozed for %s.", uid, human))
}

// summarise trims a raw record map to the fields most useful to an LLM,
// keeping the response compact. Unknown/empty fields are dropped.
func summarise(rec map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"uid", "host", "source", "process", "severity", "message", "state", "environment", "timestamp", "hash"} {
		if v, ok := rec[k]; ok && v != nil && v != "" {
			out[k] = v
		}
	}
	return out
}

// requireUID extracts a non-empty uid string from the args.
func requireUID(args map[string]any) (string, bool) {
	uid, _ := args["uid"].(string)
	uid = strings.TrimSpace(uid)
	return uid, uid != ""
}

// commentName returns the actor name stamped on the comment. MCP clients may
// pass an optional `actor`; otherwise we attribute to the AI assistant.
func commentName(args map[string]any) string {
	if a, ok := args["actor"].(string); ok && strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return "AI assistant via MCP"
}

// asInt coerces a JSON number (which decodes to float64) or an int into an
// int. Returns ok=false for anything else.
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}
