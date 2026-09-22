package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const cliValidAgentic = `{"root_cause":{"summary":"disk full","confidence":"high"},` +
	`"remediation_plan":{"steps":[{"action":"rotate logs","risk":"low"}]}}`

func TestRecordAgenticSet(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/api/v1/record/u-1/agentic", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uid":"u-1","agentic":{"analysis":{"by":"bot"}}}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "record", "agentic", "set", "u-1", cliValidAgentic)
	require.NoError(t, err)
	require.Contains(t, out, "Stored agentic analysis on u-1")
	require.Contains(t, out, "confidence high")
	// The CLI tags the write so the audit trail shows where it came from.
	require.Equal(t, "snooze-cli", gotBody["source"])
	// Provenance is never sent by the client.
	require.NotContains(t, gotBody, "analysis")
}

func TestRecordAgenticSetKeepsPayloadSource(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uid":"u-1","agentic":{}}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	body := `{"root_cause":{"summary":"x","confidence":"low"},` +
		`"remediation_plan":{"steps":[{"action":"a","risk":"low"}]},"source":"alert-rca"}`
	_, _, err := executeCmd(t, rt, "record", "agentic", "set", "u-1", body)
	require.NoError(t, err)
	require.Equal(t, "alert-rca", gotBody["source"])
}

// A payload that breaks the schema never reaches the server: the CLI runs the
// same validation and prints the offending paths.
func TestRecordAgenticSetValidatesLocally(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server must not be called for a locally invalid payload")
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, stderr, err := executeCmd(t, rt, "record", "agentic", "set", "u-1",
		`{"root_cause":{"summary":"x","confidence":"certain"},"remediation_plan":{"steps":[]}}`)
	require.Error(t, err)
	require.Contains(t, stderr, "root_cause.confidence: must be one of high|medium|low")
	require.Contains(t, stderr, "remediation_plan.steps: must hold at least one step")
}

// A server-side refusal (403 without rw_protected, 422 from a schema the CLI
// is older than) is surfaced with its details rather than a bare status.
func TestRecordAgenticSetSurfacesServerDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"nope",` +
			`"details":{"root_cause.summary":"is required"}}}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	_, stderr, err := executeCmd(t, rt, "record", "agentic", "set", "u-1", cliValidAgentic)
	require.Error(t, err)
	require.Contains(t, stderr, "root_cause.summary: is required")
}

func TestRecordAgenticGetAndClear(t *testing.T) {
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/record/u-1/agentic", r.URL.Path)
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uid":"u-1","agentic":{"root_cause":{"summary":"disk full"}}}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	rt.flags.Server = srv.URL

	out, _, err := executeCmd(t, rt, "record", "agentic", "get", "u-1")
	require.NoError(t, err)
	require.Contains(t, out, "root_cause")

	rt2, _, _ := newTestRuntime(t, srv)
	rt2.flags.Token = "tok"
	rt2.flags.Server = srv.URL
	out, _, err = executeCmd(t, rt2, "record", "agentic", "clear", "u-1")
	require.NoError(t, err)
	require.True(t, deleted)
	require.Contains(t, out, "Cleared agentic analysis on u-1")
}

func TestRecordAgenticSetRejectsBothArgAndFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Server = srv.URL
	_, _, err := executeCmd(t, rt, "record", "agentic", "set", "u-1", cliValidAgentic, "--file", "x.json")
	require.ErrorContains(t, err, "not both")
}
