package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// setAnalysisCall is the tools/call request body used across these tests.
const setAnalysisCall = `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{
  "name":"set_alert_analysis",
  "arguments":{
    "uid":"rec-1",
    "root_cause":{"summary":"/var filled by unrotated nginx logs","scope":"srv-1:/var",
                  "evidence":["df -h /var: 100%"],"confidence":"high"},
    "remediation_plan":{"steps":[{"action":"rotate logs","command":"logrotate -f","risk":"low"}],
                        "automatable":true}
  }}}`

func TestToolsCall_setAlertAnalysis(t *testing.T) {
	api := &fakeAPI{putResp: map[string]any{"uid": "rec-1", "agentic": map[string]any{}}}
	s := newTestServer(api)

	res := decodeToolResult(t, call(t, s, setAnalysisCall))
	require.False(t, res.IsError, res.Content)
	require.Contains(t, res.Content[0].Text, "Stored the analysis on alert rec-1")
	require.Contains(t, res.Content[0].Text, "confidence high")

	require.Len(t, api.puts, 1)
	require.Equal(t, "/api/v1/record/rec-1/agentic", api.puts[0].path)

	// The uid rides in the path, not the body, and the write is tagged so the
	// audit trail shows it came from an assistant.
	raw, err := json.Marshal(api.puts[0].body)
	require.NoError(t, err)
	var sent map[string]any
	require.NoError(t, json.Unmarshal(raw, &sent))
	require.NotContains(t, sent, "uid")
	require.Equal(t, "mcp", sent["source"])
	require.Contains(t, sent, "root_cause")
}

// An invalid analysis is caught before the call: the model gets the offending
// paths back, and no write is attempted.
func TestToolsCall_setAlertAnalysisValidatesBeforeCalling(t *testing.T) {
	api := &fakeAPI{}
	s := newTestServer(api)

	req := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{
	  "name":"set_alert_analysis",
	  "arguments":{"uid":"rec-1","root_cause":{"summary":"x","confidence":"certain"},
	               "remediation_plan":{"steps":[]}}}}`
	res := decodeToolResult(t, call(t, s, req))
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "root_cause.confidence")
	require.Contains(t, res.Content[0].Text, "remediation_plan.steps")
	require.Empty(t, api.puts, "an invalid analysis must never reach the server")
}

func TestToolsCall_setAlertAnalysisRequiresUID(t *testing.T) {
	s := newTestServer(&fakeAPI{})
	res := decodeToolResult(t, call(t, s,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"set_alert_analysis","arguments":{}}}`))
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "missing or empty `uid`")
}

// A server-side refusal (no rw_protected, or a schema this build predates)
// surfaces its details rather than a bare status code.
func TestToolsCall_setAlertAnalysisSurfacesServerDetails(t *testing.T) {
	api := &fakeAPI{putErr: &snoozeclient.APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_error",
		Details: map[string]any{"root_cause.summary": "is required"},
	}}
	s := newTestServer(api)
	res := decodeToolResult(t, call(t, s, setAnalysisCall))
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "root_cause.summary")
}

func TestToolsCall_getAlertAnalysis(t *testing.T) {
	api := &fakeAPI{getResp: map[string]any{
		"uid":     "rec-1",
		"agentic": map[string]any{"root_cause": map[string]any{"summary": "disk full"}},
	}}
	s := newTestServer(api)
	res := decodeToolResult(t, call(t, s,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"get_alert_analysis","arguments":{"uid":"rec-1"}}}`))
	require.False(t, res.IsError)
	require.Equal(t, []string{"/api/v1/record/rec-1/agentic"}, api.gets)
	require.Contains(t, res.Content[0].Text, "disk full")
}

// "Not analysed yet" is an answer, not a failure — the model should not
// retry or report an error to the operator.
func TestToolsCall_getAlertAnalysisMissingIsNotAnError(t *testing.T) {
	api := &fakeAPI{getErr: &snoozeclient.APIError{Status: http.StatusNotFound, Code: "not_found"}}
	s := newTestServer(api)
	res := decodeToolResult(t, call(t, s,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"get_alert_analysis","arguments":{"uid":"rec-1"}}}`))
	require.False(t, res.IsError)
	require.Contains(t, res.Content[0].Text, "no agentic analysis yet")
}

func TestToolsCall_getAlertAnalysisTransportErrorIsAnError(t *testing.T) {
	api := &fakeAPI{getErr: &snoozeclient.APIError{Status: http.StatusInternalServerError, Code: "internal"}}
	s := newTestServer(api)
	res := decodeToolResult(t, call(t, s,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"get_alert_analysis","arguments":{"uid":"rec-1"}}}`))
	require.True(t, res.IsError)
}
