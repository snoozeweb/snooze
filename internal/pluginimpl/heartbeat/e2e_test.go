package heartbeat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHeartbeatE2E exercises the full dead-man's-switch flow against a running
// snooze-server. It creates a heartbeat with a 1-second interval, never pings
// it, and polls the records endpoint until the background scanner has injected
// a "heartbeat ... missed" alert.
//
// The test is skipped unless SNOOZE_E2E_HEARTBEAT_URL is set. That variable is
// the snooze-server API base URL (no trailing slash), e.g.:
//
//	export SNOOZE_E2E_HEARTBEAT_URL="http://localhost:5200"
//	# optional Bearer token for the (authenticated) CRUD create/list calls:
//	export SNOOZE_E2E_HEARTBEAT_TOKEN="<jwt>"
//	go test -run TestHeartbeatE2E ./internal/pluginimpl/heartbeat/...
//
// Notes:
//   - The server's scan interval defaults to 30s, so the bounded wait below is
//     generous (up to ~45s). A 1s heartbeat interval guarantees the switch is
//     overdue well before the first scan tick.
//   - The created heartbeat is named with a unique suffix and best-effort
//     deleted at the end so repeated runs stay clean.
func TestHeartbeatE2E(t *testing.T) {
	base := strings.TrimRight(os.Getenv("SNOOZE_E2E_HEARTBEAT_URL"), "/")
	if base == "" {
		t.Skip("set SNOOZE_E2E_HEARTBEAT_URL (snooze-server base URL) to run the heartbeat end-to-end test")
	}
	token := os.Getenv("SNOOZE_E2E_HEARTBEAT_TOKEN")

	name := fmt.Sprintf("e2e-deadman-%d", time.Now().UnixNano())
	client := &http.Client{Timeout: 10 * time.Second}

	doReq := func(method, path string, body any) (*http.Response, []byte) {
		var rdr io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			require.NoError(t, err)
			rdr = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, base+path, rdr)
		require.NoError(t, err)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, raw
	}

	// 1. Create a 1-second heartbeat and never ping it.
	resp, raw := doReq(http.MethodPost, "/api/v1/heartbeat", map[string]any{
		"name":     name,
		"interval": 1,
		"grace":    0,
		"severity": "critical",
		"enabled":  true,
	})
	require.Truef(t, resp.StatusCode >= 200 && resp.StatusCode < 300,
		"create heartbeat: status %d body %s", resp.StatusCode, raw)

	// Best-effort cleanup.
	t.Cleanup(func() {
		_, _ = doReq(http.MethodDelete, "/api/v1/heartbeat?q="+encodeNameQuery(name), nil)
	})

	// 2. Poll the records endpoint until the miss alert appears (bounded wait).
	deadline := time.Now().Add(45 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		resp, raw := doReq(http.MethodGet, "/api/v1/record", nil)
		if resp.StatusCode == http.StatusOK {
			var listed struct {
				Data []map[string]any `json:"data"`
			}
			if err := json.Unmarshal(raw, &listed); err == nil {
				for _, d := range listed.Data {
					if d["source"] == "heartbeat" && strings.Contains(asString(d["message"]), name) {
						found = true
						break
					}
				}
			}
		}
		if found {
			break
		}
		time.Sleep(3 * time.Second)
	}

	require.True(t, found, "expected a heartbeat miss alert for %q to appear in /api/v1/record within the bounded wait", name)
}

// TestHeartbeatE2EPing exercises the ping endpoint against a running
// snooze-server. It creates a long-lived heartbeat (3600s interval so it never
// trips the scanner during the test), captures the server-generated token from
// the create response, and then:
//
//  1. PINGs with the correct token (no Authorization header) → expects HTTP 200.
//  2. PINGs again with a deliberately wrong token → expects HTTP 401.
//
// Using a separate, long-interval heartbeat keeps the miss-alert phase in
// TestHeartbeatE2E fully deterministic — this heartbeat will never go overdue
// during a normal test run.
//
// The test is skipped unless SNOOZE_E2E_HEARTBEAT_URL is set.
func TestHeartbeatE2EPing(t *testing.T) {
	base := strings.TrimRight(os.Getenv("SNOOZE_E2E_HEARTBEAT_URL"), "/")
	if base == "" {
		t.Skip("set SNOOZE_E2E_HEARTBEAT_URL (snooze-server base URL) to run the heartbeat end-to-end test")
	}
	authToken := os.Getenv("SNOOZE_E2E_HEARTBEAT_TOKEN")

	pingName := fmt.Sprintf("e2e-ping-%d", time.Now().UnixNano())
	client := &http.Client{Timeout: 10 * time.Second}

	// doAuthed issues an authenticated (CRUD) request using the operator token.
	doAuthed := func(method, path string, body any) (*http.Response, []byte) {
		var rdr io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			require.NoError(t, err)
			rdr = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, base+path, rdr)
		require.NoError(t, err)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if authToken != "" {
			req.Header.Set("Authorization", "Bearer "+authToken)
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, raw
	}

	// doPublicPing issues an unauthenticated POST to the ping endpoint (no
	// Authorization header, just ?name=&token= in the query string).
	doPublicPing := func(name, tok string) *http.Response {
		u := fmt.Sprintf("%s/api/v1/webhook/heartbeat?name=%s&token=%s", base, name, tok)
		req, err := http.NewRequest(http.MethodPost, u, nil)
		require.NoError(t, err)
		// Deliberately no Authorization header — the ping endpoint is public.
		resp, err := client.Do(req)
		require.NoError(t, err)
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()
		return resp
	}

	// 1. Create a long-interval heartbeat so it never trips the scanner.
	resp, raw := doAuthed(http.MethodPost, "/api/v1/heartbeat", map[string]any{
		"name":     pingName,
		"interval": 3600,
		"grace":    0,
		"severity": "info",
		"enabled":  true,
	})
	require.Truef(t, resp.StatusCode >= 200 && resp.StatusCode < 300,
		"create ping heartbeat: status %d body %s", resp.StatusCode, raw)

	// Best-effort cleanup at the end.
	t.Cleanup(func() {
		_, _ = doAuthed(http.MethodDelete, "/api/v1/heartbeat?q="+encodeNameQuery(pingName), nil)
	})

	// 2. Extract the per-heartbeat token from the create response.
	// Expected response shape: {"data": [{"token": "...", ...}]}
	// Fall back to a GET if the token is absent from the create body.
	hbToken := extractTokenFromResponse(raw)
	if hbToken == "" {
		// Fall back: GET the heartbeat and read its token.
		_, getRaw := doAuthed(http.MethodGet, "/api/v1/heartbeat?q="+encodeNameQuery(pingName), nil)
		hbToken = extractTokenFromResponse(getRaw)
	}
	require.NotEmpty(t, hbToken,
		"server must return a token for the heartbeat (in create response or GET); raw: %s", raw)

	// 3. Correct token ping (no Authorization header) → 200.
	pingResp := doPublicPing(pingName, hbToken)
	require.Equal(t, http.StatusOK, pingResp.StatusCode,
		"ping with correct token must return 200")

	// 4. Wrong token ping → 401.
	wrongResp := doPublicPing(pingName, "definitely-wrong-token")
	require.Equal(t, http.StatusUnauthorized, wrongResp.StatusCode,
		"ping with wrong token must return 401")
}

// extractTokenFromResponse pulls the `token` field from a response body that
// may take the shape {"data": [{"token": "..."}]} (list) or {"token": "..."}
// (single-document). Returns "" when the token is absent or unparseable.
func extractTokenFromResponse(raw []byte) string {
	// Try list shape first: {"data": [{"token": "..."}]}
	var listResp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &listResp); err == nil && len(listResp.Data) > 0 {
		if tok, _ := listResp.Data[0]["token"].(string); tok != "" {
			return tok
		}
	}
	// Try flat shape: {"token": "..."}
	var flat map[string]any
	if err := json.Unmarshal(raw, &flat); err == nil {
		if tok, _ := flat["token"].(string); tok != "" {
			return tok
		}
	}
	return ""
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// encodeNameQuery base64url-encodes a `name = <name>` condition for the bulk
// delete endpoint. Kept tiny and dependency-free; falls back to an empty query
// (no-op delete) if marshalling ever fails.
func encodeNameQuery(name string) string {
	cond := map[string]any{"op": "=", "field": "name", "value": name}
	b, err := json.Marshal(cond)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
