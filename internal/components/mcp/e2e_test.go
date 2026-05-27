package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// testLogger returns a quiet logger for tests (discards output). Shared
// across the package's _test.go files.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestMCPE2E drives the Server against a REAL snooze-server. It is skipped
// unless SNOOZE_E2E_MCP_SERVER is set, so `go test ./...` stays green in CI
// without credentials.
//
// Required env:
//
//	SNOOZE_E2E_MCP_SERVER    Snooze base URL (e.g. https://snooze.example.com)
//
// One of:
//
//	SNOOZE_E2E_MCP_TOKEN     A pre-minted bearer token, OR
//	SNOOZE_E2E_MCP_USERNAME + SNOOZE_E2E_MCP_PASSWORD (with optional
//	SNOOZE_E2E_MCP_METHOD, default "local")
//
// Optional:
//
//	SNOOZE_E2E_MCP_INSECURE  "true" to skip TLS verification (self-signed dev).
//
// The test performs initialize → tools/list → list_alerts and asserts no
// protocol error at any step. It does NOT mutate any record.
func TestMCPE2E(t *testing.T) {
	server := os.Getenv("SNOOZE_E2E_MCP_SERVER")
	if server == "" {
		t.Skip("set SNOOZE_E2E_MCP_SERVER to run the MCP end-to-end test")
	}

	sc, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:  server,
		Username: os.Getenv("SNOOZE_E2E_MCP_USERNAME"),
		Password: os.Getenv("SNOOZE_E2E_MCP_PASSWORD"),
		Method:   envOr("SNOOZE_E2E_MCP_METHOD", "local"),
		Token:    os.Getenv("SNOOZE_E2E_MCP_TOKEN"),
		Insecure: os.Getenv("SNOOZE_E2E_MCP_INSECURE") == "true",
		Logger:   testLogger(),
	})
	require.NoError(t, err)

	ctx := context.Background()
	if os.Getenv("SNOOZE_E2E_MCP_TOKEN") == "" {
		require.NoError(t, sc.Login(ctx), "login to real snooze-server")
	}

	s := NewServer(sc, "e2e", testLogger())

	// initialize
	resp := e2eCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`)
	require.Nil(t, resp.Error, "initialize: %+v", resp.Error)

	// tools/list
	resp = e2eCall(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	require.Nil(t, resp.Error, "tools/list: %+v", resp.Error)

	// list_alerts (read-only)
	resp = e2eCall(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_alerts","arguments":{"limit":5}}}`)
	require.Nil(t, resp.Error, "tools/call list_alerts: %+v", resp.Error)
	res := decodeToolResult(t, resp)
	require.False(t, res.IsError, "list_alerts returned tool error: %v", res.Content)
	t.Logf("list_alerts returned: %s", res.Content[0].Text)
}

func e2eCall(t *testing.T, s *Server, req string) rpcResponse {
	t.Helper()
	out := s.Handle(context.Background(), []byte(req))
	require.NotNil(t, out)
	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out, &resp))
	return resp
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
