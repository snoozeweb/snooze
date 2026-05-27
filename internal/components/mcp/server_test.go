package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// fakeAPI is a race-safe stand-in for snoozeAPI. It records every call and
// lets a test program canned responses/errors. The mutex guards concurrent
// access so `go test -race` stays clean even though the Server is sequential
// in practice.
type fakeAPI struct {
	mu sync.Mutex

	// posts captures (path, body) for every Post call.
	posts []postCall
	// postResult, when set, is decoded into dest on the next Post matching
	// the search endpoint.
	postResp any
	postErr  error

	comments  []snoozeclient.Comment
	commErr   error
	snoozes   []snoozeclient.Snooze
	snoozeErr error
}

type postCall struct {
	path string
	body any
}

func (f *fakeAPI) Post(_ context.Context, path string, body, dest any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, postCall{path: path, body: body})
	if f.postErr != nil {
		return f.postErr
	}
	if dest != nil && f.postResp != nil {
		// Round-trip the canned response through JSON to honour dest's type.
		raw, _ := json.Marshal(f.postResp)
		return json.Unmarshal(raw, dest)
	}
	return nil
}

func (f *fakeAPI) PostComment(_ context.Context, c snoozeclient.Comment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, c)
	return f.commErr
}

func (f *fakeAPI) CreateSnooze(_ context.Context, s snoozeclient.Snooze) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snoozes = append(f.snoozes, s)
	return f.snoozeErr
}

func (f *fakeAPI) lastComment() snoozeclient.Comment {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.comments[len(f.comments)-1]
}

// newTestServer builds a Server over the fake. version is fixed so the
// initialize assertion is stable.
func newTestServer(api snoozeAPI) *Server {
	return NewServer(api, "1.2.3", nil)
}

// call is a helper that round-trips one request through Handle and decodes
// the response envelope.
func call(t *testing.T, s *Server, req string) rpcResponse {
	t.Helper()
	out := s.Handle(context.Background(), []byte(req))
	require.NotNil(t, out, "expected a response for %q", req)
	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out, &resp))
	return resp
}

func TestInitialize(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	out := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}`))
	var resp struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      int              `json:"id"`
		Result  initializeResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(out, &resp))
	require.Equal(t, "2.0", resp.JSONRPC)
	require.Equal(t, 1, resp.ID)
	// We echo the client's requested version per the spec's negotiation rule.
	require.Equal(t, "2024-11-05", resp.Result.ProtocolVersion)
	require.Equal(t, serverName, resp.Result.ServerInfo.Name)
	require.Equal(t, "1.2.3", resp.Result.ServerInfo.Version)
	require.Contains(t, resp.Result.Capabilities, "tools")
}

func TestInitialize_defaultsProtocolVersionWhenAbsent(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	out := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	var resp struct {
		Result initializeResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(out, &resp))
	require.Equal(t, supportedProtocolVersion, resp.Result.ProtocolVersion)
}

func TestNotificationsInitialized_noResponse(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	out := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	require.Nil(t, out, "notifications must produce no response")
}

func TestToolsList_containsAllSixTools(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	out := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	var resp struct {
		Result toolsListResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(out, &resp))

	names := map[string]tool{}
	for _, tl := range resp.Result.Tools {
		names[tl.Name] = tl
	}
	for _, want := range []string{"list_alerts", "get_alert", "ack_alert", "close_alert", "comment_alert", "snooze_alert"} {
		tl, ok := names[want]
		require.True(t, ok, "missing tool %q", want)
		require.NotEmpty(t, tl.Description, "%q has no description", want)
		require.Equal(t, "object", tl.InputSchema["type"], "%q inputSchema must be a JSON object", want)
		require.Contains(t, tl.InputSchema, "properties", "%q inputSchema must have properties", want)
	}
	require.Len(t, resp.Result.Tools, 6)
}

func TestToolsCall_listAlerts_invokesAPIandReturnsContent(t *testing.T) {
	api := &fakeAPI{postResp: map[string]any{
		"data": []map[string]any{
			{"uid": "rec-1", "host": "web-1", "severity": "warning", "message": "disk full", "state": ""},
			{"uid": "rec-2", "host": "db-1", "severity": "critical", "message": "down"},
		},
	}}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_alerts","arguments":{"query":"disk","limit":5}}}`)
	require.Nil(t, resp.Error)

	// Decode the result into a toolCallResult to inspect the content.
	res := decodeToolResult(t, resp)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)
	require.Equal(t, "text", res.Content[0].Type)
	require.Contains(t, res.Content[0].Text, "rec-1")
	require.Contains(t, res.Content[0].Text, "rec-2")

	// The fake recorded the search POST to the right endpoint with a SEARCH
	// condition derived from `query`.
	require.Len(t, api.posts, 1)
	require.Equal(t, recordSearchEndpoint, api.posts[0].path)
	body := api.posts[0].body.(map[string]any)
	require.Equal(t, []any{"SEARCH", "disk"}, body["condition"])
}

func TestToolsCall_getAlert_returnsRecord(t *testing.T) {
	api := &fakeAPI{postResp: map[string]any{
		"data": []map[string]any{{"uid": "rec-9", "host": "h", "message": "m"}},
	}}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_alert","arguments":{"uid":"rec-9"}}}`)
	res := decodeToolResult(t, resp)
	require.False(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "rec-9")
	body := api.posts[0].body.(map[string]any)
	require.Equal(t, []any{"=", "uid", "rec-9"}, body["condition"])
}

func TestToolsCall_getAlert_notFound_isError(t *testing.T) {
	api := &fakeAPI{postResp: map[string]any{"data": []map[string]any{}}}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_alert","arguments":{"uid":"missing"}}}`)
	res := decodeToolResult(t, resp)
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "no alert found")
}

func TestToolsCall_ackAlert_postsExpectedCommentPayload(t *testing.T) {
	api := &fakeAPI{}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"ack_alert","arguments":{"uid":"rec-7","message":"on it"}}}`)
	res := decodeToolResult(t, resp)
	require.False(t, res.IsError)

	require.Len(t, api.comments, 1)
	c := api.lastComment()
	require.Equal(t, "rec-7", c.RecordUID)
	require.Equal(t, "ack", c.Type)
	require.Equal(t, "mcp", c.Method)
	require.Equal(t, "on it", c.Message)
	require.NotEmpty(t, c.Name)
}

func TestToolsCall_closeAlert_postsCloseType(t *testing.T) {
	api := &fakeAPI{}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"close_alert","arguments":{"uid":"rec-8"}}}`)
	res := decodeToolResult(t, resp)
	require.False(t, res.IsError)
	require.Equal(t, "close", api.lastComment().Type)
}

func TestToolsCall_commentAlert_requiresMessage(t *testing.T) {
	api := &fakeAPI{}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"comment_alert","arguments":{"uid":"rec-1"}}}`)
	res := decodeToolResult(t, resp)
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "message")
	require.Empty(t, api.comments)
}

func TestToolsCall_commentAlert_postsPlainComment(t *testing.T) {
	api := &fakeAPI{}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"comment_alert","arguments":{"uid":"rec-1","message":"hello"}}}`)
	res := decodeToolResult(t, resp)
	require.False(t, res.IsError)
	c := api.lastComment()
	require.Equal(t, "", c.Type)
	require.Equal(t, "hello", c.Message)
}

func TestToolsCall_snoozeAlert_finite(t *testing.T) {
	api := &fakeAPI{}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"snooze_alert","arguments":{"uid":"rec-3","duration":"6h"}}}`)
	res := decodeToolResult(t, resp)
	require.False(t, res.IsError)
	require.Len(t, api.snoozes, 1)
	require.Equal(t, []any{"=", "uid", "rec-3"}, api.snoozes[0].Condition)
	require.NotNil(t, api.snoozes[0].TimeConstraints)
	// A best-effort ack also fires.
	require.Equal(t, "ack", api.lastComment().Type)
}

func TestToolsCall_snoozeAlert_forever(t *testing.T) {
	api := &fakeAPI{}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"snooze_alert","arguments":{"uid":"rec-3"}}}`)
	res := decodeToolResult(t, resp)
	require.False(t, res.IsError)
	require.Nil(t, api.snoozes[0].TimeConstraints, "forever snooze must have nil time_constraints")
	require.Contains(t, res.Content[0].Text, "forever")
}

func TestToolsCall_snoozeAlert_invalidDuration(t *testing.T) {
	api := &fakeAPI{}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"snooze_alert","arguments":{"uid":"rec-3","duration":"banana"}}}`)
	res := decodeToolResult(t, resp)
	require.True(t, res.IsError)
	require.Empty(t, api.snoozes)
}

func TestToolsCall_apiError_isToolError(t *testing.T) {
	api := &fakeAPI{commErr: errors.New("boom")}
	s := newTestServer(api)
	resp := call(t, s, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"ack_alert","arguments":{"uid":"rec-1"}}}`)
	require.Nil(t, resp.Error, "a Snooze-side failure must be a tool result error, not a JSON-RPC error")
	res := decodeToolResult(t, resp)
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "boom")
}

func TestToolsCall_unknownTool_isMethodNotFound(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	resp := call(t, s, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	require.NotNil(t, resp.Error)
	require.Equal(t, codeMethodNotFound, resp.Error.Code)
}

func TestToolsCall_missingUID_isToolError(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	resp := call(t, s, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"ack_alert","arguments":{}}}`)
	res := decodeToolResult(t, resp)
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "uid")
}

func TestUnknownMethod_isMethodNotFound(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	resp := call(t, s, `{"jsonrpc":"2.0","id":12,"method":"resources/list"}`)
	require.NotNil(t, resp.Error)
	require.Equal(t, codeMethodNotFound, resp.Error.Code)
	require.Equal(t, json.RawMessage("12"), resp.ID)
}

func TestUnknownNotification_noResponse(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	out := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled"}`))
	require.Nil(t, out)
}

func TestMalformedJSON_isParseError(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	out := s.Handle(context.Background(), []byte(`{not json`))
	require.NotNil(t, out)
	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, codeParseError, resp.Error.Code)
	require.Equal(t, json.RawMessage("null"), resp.ID)
}

func TestToolsCall_badParams_isInvalidParams(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	// params is a string, not the expected object → decode fails.
	resp := call(t, s, `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":"oops"}`)
	require.NotNil(t, resp.Error)
	require.Equal(t, codeInvalidParams, resp.Error.Code)
}

// decodeToolResult extracts a toolCallResult from a response envelope.
func decodeToolResult(t *testing.T, resp rpcResponse) toolCallResult {
	t.Helper()
	require.Nil(t, resp.Error, "expected a result, got JSON-RPC error: %+v", resp.Error)
	raw, err := json.Marshal(resp.Result)
	require.NoError(t, err)
	var res toolCallResult
	require.NoError(t, json.Unmarshal(raw, &res))
	return res
}
