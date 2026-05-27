package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// daemonWith builds a Daemon whose Server is backed by the fake and whose
// streams are the supplied in/out — bypassing New (and the real snooze
// client) so the stdio loop can be exercised in isolation.
func daemonWith(api snoozeAPI, in *bytes.Buffer, out *bytes.Buffer) *Daemon {
	return &Daemon{
		cfg:    Config{Server: "https://x", Token: "t", Method: "local"},
		logger: testLogger(),
		server: NewServer(api, "test", testLogger()),
		in:     in,
		out:    out,
	}
}

func TestRun_framing_oneMessagePerLine(t *testing.T) {
	api := &fakeAPI{postResp: map[string]any{"data": []map[string]any{{"uid": "rec-1"}}}}
	in := bytes.NewBufferString(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`, // no response expected
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_alerts","arguments":{}}}`,
		"", // trailing blank line — must be skipped
	}, "\n") + "\n")
	var out bytes.Buffer

	d := daemonWith(api, in, &out)
	require.NoError(t, d.Run(context.Background()))

	// Exactly three response lines (initialize, tools/list, tools/call); the
	// notification produced nothing and the blank line was skipped.
	lines := nonEmptyLines(out.String())
	require.Len(t, lines, 3, "got: %q", out.String())

	// Each line must be a standalone, complete JSON object.
	ids := []json.RawMessage{}
	for _, ln := range lines {
		var resp rpcResponse
		require.NoError(t, json.Unmarshal([]byte(ln), &resp), "line not valid JSON: %q", ln)
		require.Equal(t, "2.0", resp.JSONRPC)
		ids = append(ids, resp.ID)
	}
	require.Equal(t, json.RawMessage("1"), ids[0])
	require.Equal(t, json.RawMessage("2"), ids[1])
	require.Equal(t, json.RawMessage("3"), ids[2])
}

func TestRun_stopsOnEOF(t *testing.T) {
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	var out bytes.Buffer
	d := daemonWith(&fakeAPI{}, in, &out)
	// No trailing data → Run returns nil on EOF.
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return on EOF")
	}
	require.Len(t, nonEmptyLines(out.String()), 1)
}

func TestRun_emptyInput_noOutput(t *testing.T) {
	var out bytes.Buffer
	d := daemonWith(&fakeAPI{}, bytes.NewBufferString(""), &out)
	require.NoError(t, d.Run(context.Background()))
	require.Empty(t, strings.TrimSpace(out.String()))
}

func nonEmptyLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			lines = append(lines, sc.Text())
		}
	}
	return lines
}
